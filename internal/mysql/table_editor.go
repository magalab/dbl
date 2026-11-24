package mysql

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/magalab/dbl/internal/connection"
)

func (store *Store) ApplyTableChanges(ctx context.Context, object connection.Object, primaryKey []string, changes []connection.TableChange) error {
	if store == nil || store.db == nil {
		return errors.New("MySQL connection is closed")
	}
	if store.readOnly {
		return errors.New("MySQL connection is read-only")
	}
	if object.Type != "table" {
		return errors.New("only base tables can be edited")
	}
	if object.Schema == "" || object.Name == "" {
		return errors.New("MySQL database and table are required")
	}
	if len(changes) == 0 || len(changes) > connection.MaxTableChangeBatch {
		return fmt.Errorf("table edit batch must contain 1 to %d changes", connection.MaxTableChangeBatch)
	}
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return redactError("begin MySQL table edit", err, store.password())
	}
	defer tx.Rollback()
	table := quoteMySQLTable(object.Schema, object.Name)
	for _, change := range changes {
		if err := applyMySQLChange(ctx, tx, table, primaryKey, change); err != nil {
			return redactError("apply MySQL table edit", err, store.password())
		}
	}
	return redactError("commit MySQL table edit", tx.Commit(), store.password())
}

func (store *Store) ImportTableRows(ctx context.Context, object connection.Object, changes []connection.TableChange) error {
	if store == nil || store.db == nil {
		return errors.New("MySQL connection is closed")
	}
	if store.readOnly {
		return errors.New("MySQL connection is read-only")
	}
	if object.Type != "table" || object.Schema == "" || object.Name == "" {
		return errors.New("a MySQL database and base table are required")
	}
	if len(changes) == 0 || len(changes) > connection.MaxTableImportRows {
		return fmt.Errorf("table import must contain 1 to %d rows", connection.MaxTableImportRows)
	}
	var transactions string
	const engineQuery = `
		SELECT COALESCE(e.transactions, 'NO')
		FROM information_schema.tables AS t
		LEFT JOIN information_schema.engines AS e ON e.engine = t.engine
		WHERE t.table_schema = ? AND t.table_name = ? AND t.table_type = 'BASE TABLE'`
	if err := store.db.QueryRowContext(ctx, engineQuery, object.Schema, object.Name).Scan(&transactions); err != nil {
		return redactError("check MySQL import table engine", err, store.password())
	}
	if !strings.EqualFold(transactions, "YES") {
		return errors.New("MySQL table engine does not support transactions; import was not started")
	}
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return redactError("begin MySQL table import", err, store.password())
	}
	defer tx.Rollback()
	table := quoteMySQLTable(object.Schema, object.Name)
	for _, change := range changes {
		if change.Kind != connection.InsertRow {
			return errors.New("table import accepts insert rows only")
		}
		if err := applyMySQLChange(ctx, tx, table, nil, change); err != nil {
			return redactError("import MySQL table rows", err, store.password())
		}
	}
	return redactError("commit MySQL table import", tx.Commit(), store.password())
}

func (store *Store) ApplySchemaChanges(ctx context.Context, statements []string) error {
	if store == nil || store.db == nil {
		return errors.New("MySQL connection is closed")
	}
	if store.readOnly {
		return errors.New("MySQL connection is read-only")
	}
	if len(statements) == 0 {
		return errors.New("schema change batch is empty")
	}
	// MySQL DDL statements implicitly commit, so running them in a SQL
	// transaction would suggest atomicity that the server cannot provide.
	for _, statement := range statements {
		if strings.TrimSpace(statement) == "" {
			return errors.New("schema change statement is empty")
		}
		if _, err := store.db.ExecContext(ctx, statement); err != nil {
			return redactError("apply MySQL schema edit", err, store.password())
		}
	}
	return nil
}

func applyMySQLChange(ctx context.Context, tx *sql.Tx, table string, primaryKey []string, change connection.TableChange) error {
	switch change.Kind {
	case connection.InsertRow:
		columns := sortedMySQLKeys(change.Values)
		if len(columns) == 0 {
			_, err := tx.ExecContext(ctx, "INSERT INTO "+table+" () VALUES ()")
			return err
		}
		identifiers, placeholders, values := make([]string, len(columns)), make([]string, len(columns)), make([]any, len(columns))
		for i, column := range columns {
			identifiers[i], placeholders[i], values[i] = quoteMySQLIdentifier(column), "?", change.Values[column]
		}
		_, err := tx.ExecContext(ctx, "INSERT INTO "+table+" ("+strings.Join(identifiers, ", ")+") VALUES ("+strings.Join(placeholders, ", ")+")", values...)
		return err
	case connection.UpdateRow:
		if len(primaryKey) == 0 {
			return errors.New("table has no primary key; row editing is unavailable")
		}
		columns := sortedMySQLKeys(change.Values)
		if len(columns) == 0 {
			return nil
		}
		where, keyValues, err := mysqlPrimaryKeyWhere(primaryKey, change.Original)
		if err != nil {
			return err
		}
		if err := lockMySQLRow(ctx, tx, table, where, keyValues); err != nil {
			return err
		}
		assignments, values := make([]string, len(columns)), make([]any, 0, len(columns)+len(keyValues))
		for i, column := range columns {
			assignments[i] = quoteMySQLIdentifier(column) + " = ?"
			values = append(values, change.Values[column])
		}
		values = append(values, keyValues...)
		result, err := tx.ExecContext(ctx, "UPDATE "+table+" SET "+strings.Join(assignments, ", ")+" WHERE "+where, values...)
		if err != nil {
			return err
		}
		count, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if count > 1 {
			return fmt.Errorf("primary key matched %d rows", count)
		}
		return nil
	case connection.DeleteRow:
		where, values, err := mysqlPrimaryKeyWhere(primaryKey, change.Original)
		if err != nil {
			return err
		}
		result, err := tx.ExecContext(ctx, "DELETE FROM "+table+" WHERE "+where, values...)
		if err != nil {
			return err
		}
		count, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if count != 1 {
			return fmt.Errorf("expected to delete one row, affected %d", count)
		}
		return nil
	default:
		return errors.New("unsupported MySQL table edit")
	}
}

func mysqlPrimaryKeyWhere(primaryKey []string, original map[string]any) (string, []any, error) {
	if len(primaryKey) == 0 {
		return "", nil, errors.New("table has no primary key; row editing is unavailable")
	}
	parts, values := make([]string, len(primaryKey)), make([]any, len(primaryKey))
	for i, column := range primaryKey {
		value, ok := original[column]
		if !ok || value == nil {
			return "", nil, fmt.Errorf("original primary key value for %q is missing", column)
		}
		parts[i], values[i] = quoteMySQLIdentifier(column)+" = ?", value
	}
	return strings.Join(parts, " AND "), values, nil
}

func lockMySQLRow(ctx context.Context, tx *sql.Tx, table, where string, values []any) error {
	var found int
	err := tx.QueryRowContext(ctx, "SELECT 1 FROM "+table+" WHERE "+where+" FOR UPDATE", values...).Scan(&found)
	if errors.Is(err, sql.ErrNoRows) {
		return errors.New("row changed or was deleted by another session; refresh before editing")
	}
	return err
}

func quoteMySQLTable(schema, table string) string {
	return quoteMySQLIdentifier(schema) + "." + quoteMySQLIdentifier(table)
}

func quoteMySQLIdentifier(value string) string {
	return "`" + strings.ReplaceAll(value, "`", "``") + "`"
}

func sortedMySQLKeys(values map[string]any) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

var _ connection.TableEditor = (*Store)(nil)
var _ connection.TableImporter = (*Store)(nil)
