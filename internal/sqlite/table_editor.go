package sqlite

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
		return errors.New("SQLite connection is closed")
	}
	if object.Type != "table" {
		return errors.New("only base tables can be edited")
	}
	if object.Name == "" || (object.Schema != "" && object.Schema != "main") {
		return errors.New("SQLite table name is invalid")
	}
	if len(changes) == 0 || len(changes) > connection.MaxTableChangeBatch {
		return fmt.Errorf("table edit batch must contain 1 to %d changes", connection.MaxTableChangeBatch)
	}
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin SQLite table edit: %w", err)
	}
	defer tx.Rollback()
	table := quoteSQLiteIdentifier(object.Name)
	for _, change := range changes {
		if err := applySQLiteChange(ctx, tx, table, primaryKey, change); err != nil {
			return fmt.Errorf("apply SQLite table edit: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit SQLite table edit: %w", err)
	}
	return nil
}

func (store *Store) ImportTableRows(ctx context.Context, object connection.Object, changes []connection.TableChange) error {
	if store == nil || store.db == nil {
		return errors.New("SQLite connection is closed")
	}
	if object.Type != "table" || object.Name == "" || object.Schema != "" && object.Schema != "main" {
		return errors.New("a valid SQLite base table is required")
	}
	if len(changes) == 0 || len(changes) > connection.MaxTableImportRows {
		return fmt.Errorf("table import must contain 1 to %d rows", connection.MaxTableImportRows)
	}
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin SQLite table import: %w", err)
	}
	defer tx.Rollback()
	table := quoteSQLiteIdentifier(object.Name)
	for _, change := range changes {
		if change.Kind != connection.InsertRow {
			return errors.New("table import accepts insert rows only")
		}
		if err := applySQLiteChange(ctx, tx, table, nil, change); err != nil {
			return fmt.Errorf("import SQLite table rows: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit SQLite table import: %w", err)
	}
	return nil
}

func (store *Store) ApplySchemaChanges(ctx context.Context, statements []string) error {
	if store == nil || store.db == nil {
		return errors.New("SQLite connection is closed")
	}
	if len(statements) == 0 {
		return errors.New("schema change batch is empty")
	}
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin SQLite schema edit: %w", err)
	}
	defer tx.Rollback()
	for _, statement := range statements {
		if strings.TrimSpace(statement) == "" {
			return errors.New("schema change statement is empty")
		}
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("apply SQLite schema edit: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit SQLite schema edit: %w", err)
	}
	return nil
}

func applySQLiteChange(ctx context.Context, tx *sql.Tx, table string, primaryKey []string, change connection.TableChange) error {
	switch change.Kind {
	case connection.InsertRow:
		columns := sortedSQLiteKeys(change.Values)
		if len(columns) == 0 {
			_, err := tx.ExecContext(ctx, "INSERT INTO "+table+" DEFAULT VALUES")
			return err
		}
		identifiers, placeholders, values := make([]string, len(columns)), make([]string, len(columns)), make([]any, len(columns))
		for i, column := range columns {
			identifiers[i], placeholders[i], values[i] = quoteSQLiteIdentifier(column), "?", change.Values[column]
		}
		_, err := tx.ExecContext(ctx, "INSERT INTO "+table+" ("+strings.Join(identifiers, ", ")+") VALUES ("+strings.Join(placeholders, ", ")+")", values...)
		return err
	case connection.UpdateRow:
		if len(primaryKey) == 0 {
			return errors.New("table has no primary key; row editing is unavailable")
		}
		columns := sortedSQLiteKeys(change.Values)
		if len(columns) == 0 {
			return nil
		}
		where, keyValues, err := sqlitePrimaryKeyWhere(primaryKey, change.Original)
		if err != nil {
			return err
		}
		var found int
		if err := tx.QueryRowContext(ctx, "SELECT 1 FROM "+table+" WHERE "+where, keyValues...).Scan(&found); errors.Is(err, sql.ErrNoRows) {
			return errors.New("row changed or was deleted by another session; refresh before editing")
		} else if err != nil {
			return err
		}
		assignments, values := make([]string, len(columns)), make([]any, 0, len(columns)+len(keyValues))
		for i, column := range columns {
			assignments[i] = quoteSQLiteIdentifier(column) + " = ?"
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
		where, values, err := sqlitePrimaryKeyWhere(primaryKey, change.Original)
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
		return errors.New("unsupported SQLite table edit")
	}
}

func sqlitePrimaryKeyWhere(primaryKey []string, original map[string]any) (string, []any, error) {
	if len(primaryKey) == 0 {
		return "", nil, errors.New("table has no primary key; row editing is unavailable")
	}
	parts, values := make([]string, len(primaryKey)), make([]any, len(primaryKey))
	for i, column := range primaryKey {
		value, ok := original[column]
		if !ok || value == nil {
			return "", nil, fmt.Errorf("original primary key value for %q is missing", column)
		}
		parts[i], values[i] = quoteSQLiteIdentifier(column)+" = ?", value
	}
	return strings.Join(parts, " AND "), values, nil
}

func quoteSQLiteIdentifier(value string) string {
	return `"` + strings.ReplaceAll(value, `"`, `""`) + `"`
}

func sortedSQLiteKeys(values map[string]any) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

var _ connection.TableEditor = (*Store)(nil)
var _ connection.TableImporter = (*Store)(nil)
