package postgresql

import (
	"context"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/magalab/dbl/internal/connection"
)

func (store *Store) ApplyTableChanges(ctx context.Context, object connection.Object, primaryKey []string, changes []connection.TableChange) error {
	if store == nil || store.db == nil {
		return errors.New("PostgreSQL connection is closed")
	}
	if store.readOnly {
		return errors.New("PostgreSQL connection is read-only")
	}
	if object.Type != "table" {
		return errors.New("only base tables can be edited")
	}
	if object.Schema == "" || object.Name == "" {
		return errors.New("PostgreSQL schema and table are required")
	}
	if len(changes) == 0 || len(changes) > connection.MaxTableChangeBatch {
		return fmt.Errorf("table edit batch must contain 1 to %d changes", connection.MaxTableChangeBatch)
	}
	columns, err := store.Columns(ctx, object.Schema, object.Name)
	if err != nil {
		return err
	}
	types := make(map[string]string, len(columns))
	for _, column := range columns {
		types[column.Name] = column.Type
	}
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return redactError("begin PostgreSQL table edit", err, store.config.Password)
	}
	defer tx.Rollback()

	table := quoteTable(object.Schema, object.Name)
	for _, change := range changes {
		if err := applyPostgreSQLChange(ctx, tx, table, primaryKey, types, change); err != nil {
			return redactError("apply PostgreSQL table edit", err, store.config.Password)
		}
	}
	return redactError("commit PostgreSQL table edit", tx.Commit(), store.config.Password)
}

func (store *Store) ApplySchemaChanges(ctx context.Context, statements []string) error {
	if store == nil || store.db == nil {
		return errors.New("PostgreSQL connection is closed")
	}
	if store.readOnly {
		return errors.New("PostgreSQL connection is read-only")
	}
	if len(statements) == 0 {
		return errors.New("schema change batch is empty")
	}
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return redactError("begin PostgreSQL schema edit", err, store.config.Password)
	}
	defer tx.Rollback()
	for _, statement := range statements {
		if strings.TrimSpace(statement) == "" {
			return errors.New("schema change statement is empty")
		}
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			return redactError("apply PostgreSQL schema edit", err, store.config.Password)
		}
	}
	if err := tx.Commit(); err != nil {
		return redactError("commit PostgreSQL schema edit", err, store.config.Password)
	}
	return nil
}

func applyPostgreSQLChange(ctx context.Context, tx *sql.Tx, table string, primaryKey []string, types map[string]string, change connection.TableChange) error {
	switch change.Kind {
	case connection.InsertRow:
		columns := sortedKeys(change.Values)
		if len(columns) == 0 {
			_, err := tx.ExecContext(ctx, "INSERT INTO "+table+" DEFAULT VALUES")
			return err
		}
		identifiers, placeholders, values := make([]string, len(columns)), make([]string, len(columns)), make([]any, len(columns))
		for i, column := range columns {
			typeName := types[column]
			if typeName == "" {
				return fmt.Errorf("column %q has no database type metadata", column)
			}
			identifiers[i], placeholders[i], values[i] = quoteIdentifier(column), fmt.Sprintf("CAST(CAST($%d AS text) AS %s)", i+1, typeName), pgTextParameter(change.Values[column], typeName)
		}
		_, err := tx.ExecContext(ctx, "INSERT INTO "+table+" ("+strings.Join(identifiers, ", ")+") VALUES ("+strings.Join(placeholders, ", ")+")", values...)
		return err
	case connection.UpdateRow:
		if len(primaryKey) == 0 {
			return errors.New("table has no primary key; row editing is unavailable")
		}
		columns := sortedKeys(change.Values)
		if len(columns) == 0 {
			return nil
		}
		where, keyValues, err := pgPrimaryKeyWhere(primaryKey, change.Original, types, len(columns))
		if err != nil {
			return err
		}
		if err := lockPostgreSQLRow(ctx, tx, table, where, keyValues); err != nil {
			return err
		}
		assignments, values := make([]string, len(columns)), make([]any, 0, len(columns)+len(keyValues))
		for i, column := range columns {
			typeName := types[column]
			if typeName == "" {
				return fmt.Errorf("column %q has no database type metadata", column)
			}
			assignments[i] = quoteIdentifier(column) + fmt.Sprintf(" = CAST($%d AS %s)", i+1, typeName)
			values = append(values, pgTextParameter(change.Values[column], typeName))
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
		where, values, err := pgPrimaryKeyWhere(primaryKey, change.Original, types, 0)
		if err != nil {
			return err
		}
		result, err := tx.ExecContext(ctx, "DELETE FROM "+table+" WHERE "+where, values...)
		if err != nil {
			return err
		}
		return requireOnePostgreSQLRow(result)
	default:
		return errors.New("unsupported PostgreSQL table edit")
	}
}

func pgPrimaryKeyWhere(primaryKey []string, original map[string]any, types map[string]string, offset int) (string, []any, error) {
	if len(primaryKey) == 0 {
		return "", nil, errors.New("table has no primary key; row editing is unavailable")
	}
	parts, values := make([]string, len(primaryKey)), make([]any, len(primaryKey))
	for i, column := range primaryKey {
		value, ok := original[column]
		if !ok || value == nil {
			return "", nil, fmt.Errorf("original primary key value for %q is missing", column)
		}
		typeName := types[column]
		if typeName == "" {
			return "", nil, fmt.Errorf("primary key column %q has no database type metadata", column)
		}
		parts[i], values[i] = quoteIdentifier(column)+fmt.Sprintf(" = CAST(CAST($%d AS text) AS %s)", offset+i+1, typeName), pgTextParameter(value, typeName)
	}
	return strings.Join(parts, " AND "), values, nil
}

func pgTextParameter(value any, typeName string) any {
	if value == nil {
		return nil
	}
	var text string
	switch value := value.(type) {
	case string:
		text = value
	case []byte:
		if strings.Contains(strings.ToLower(typeName), "bytea") {
			text = `\x` + hex.EncodeToString(value)
		} else {
			text = string(value)
		}
	case time.Time:
		typeName = strings.ToLower(typeName)
		switch {
		case strings.HasPrefix(typeName, "date"):
			text = value.Format("2006-01-02")
		case strings.HasPrefix(typeName, "time"):
			text = value.Format("15:04:05.999999999Z07:00")
		default:
			text = value.Format("2006-01-02 15:04:05.999999999Z07:00")
		}
	default:
		text = fmt.Sprint(value)
	}
	return pgtype.Text{String: text, Valid: true}
}

func lockPostgreSQLRow(ctx context.Context, tx *sql.Tx, table, where string, values []any) error {
	var found int
	err := tx.QueryRowContext(ctx, "SELECT 1 FROM "+table+" WHERE "+where+" FOR UPDATE", values...).Scan(&found)
	if errors.Is(err, sql.ErrNoRows) {
		return errors.New("row changed or was deleted by another session; refresh before editing")
	}
	return err
}

func requireOnePostgreSQLRow(result sql.Result) error {
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return fmt.Errorf("expected to delete one row, affected %d", count)
	}
	return nil
}

func quoteTable(schema, table string) string {
	return quoteIdentifier(schema) + "." + quoteIdentifier(table)
}

func quoteIdentifier(value string) string {
	return `"` + strings.ReplaceAll(value, `"`, `""`) + `"`
}

func sortedKeys(values map[string]any) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

var _ connection.TableEditor = (*Store)(nil)
