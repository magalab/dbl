// Package sqlite adapts a local SQLite database to the workbench's SQL stream.
package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/magalab/dbl/internal/connection"
	"github.com/magalab/dbl/internal/sqliteuri"
	"github.com/magalab/dbl/internal/sqlstream"
	modernsqlite "modernc.org/sqlite"
)

func init() {
	modernsqlite.RegisterConnectionHook(func(driverConn modernsqlite.ExecQuerierContext, dsn string) error {
		parsed, err := url.Parse(dsn)
		if err != nil {
			return fmt.Errorf("parse SQLite connection URI: %w", err)
		}
		if parsed.Query().Get("mode") != "ro" {
			return nil
		}
		return installReadOnlyAuthorizer(driverConn)
	})
}

type AccessMode uint8

const (
	ReadWriteExisting AccessMode = iota
	ReadOnly
	CreateNew
)

type Connector struct {
	Path   string
	Access AccessMode
}

func (connector Connector) Connect(ctx context.Context) (connection.Connection, error) {
	return Open(ctx, connector.Path, connector.Access)
}

// Store owns one local database handle. Call Close when its workspace closes.
type Store struct {
	db   *sql.DB
	path string
}

type Object = connection.Object
type ObjectPage = connection.ObjectPage
type Column = connection.Column

// Open opens a SQLite file with an explicit access policy and verifies that it
// can be reached before returning. A missing file is only created for
// CreateNew, so a typo in an existing-file path cannot silently create an
// empty database. New files are created with owner-only permissions.
func Open(ctx context.Context, path string, access AccessMode) (*Store, error) {
	if strings.TrimSpace(path) == "" {
		return nil, errors.New("SQLite file path is empty")
	}
	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve SQLite file path: %w", err)
	}
	info, statErr := os.Stat(absPath)
	switch access {
	case ReadWriteExisting, ReadOnly:
		if statErr != nil {
			return nil, fmt.Errorf("open SQLite file: %w", statErr)
		}
		if info.IsDir() {
			return nil, errors.New("SQLite path is a directory")
		}
	case CreateNew:
		if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
			return nil, fmt.Errorf("inspect SQLite file: %w", statErr)
		}
		if statErr == nil {
			if info.IsDir() {
				return nil, errors.New("SQLite path is a directory")
			}
			return nil, fmt.Errorf("SQLite file already exists: %s", absPath)
		}
		file, err := os.OpenFile(absPath, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o600)
		if err != nil {
			return nil, fmt.Errorf("create SQLite file: %w", err)
		}
		if err := file.Close(); err != nil {
			_ = os.Remove(absPath)
			return nil, fmt.Errorf("close new SQLite file: %w", err)
		}
	default:
		return nil, fmt.Errorf("unknown SQLite access mode %d", access)
	}

	dsn, err := dataSourceName(absPath, access)
	if err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("configure SQLite connection: %w", err)
	}
	db.SetMaxOpenConns(4)
	db.SetMaxIdleConns(1)

	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := db.PingContext(pingCtx); err != nil {
		_ = db.Close()
		if access == CreateNew {
			_ = os.Remove(absPath)
		}
		return nil, fmt.Errorf("connect to SQLite file: %w", err)
	}
	return &Store{db: db, path: absPath}, nil
}

func dataSourceName(absPath string, access AccessMode) (string, error) {
	mode := ""
	switch access {
	case ReadWriteExisting:
		mode = "rw"
	case ReadOnly:
		mode = "ro"
	case CreateNew:
		mode = "rw"
	default:
		return "", fmt.Errorf("unknown SQLite access mode %d", access)
	}
	return sqliteuri.DataSourceName(absPath, mode, runtime.GOOS == "windows")
}
func (store *Store) Close() error {
	if store == nil || store.db == nil {
		return nil
	}
	return store.db.Close()
}

func (store *Store) Driver() connection.Driver { return connection.DriverSQLite }

func (store *Store) HealthCheck(ctx context.Context) error {
	if store == nil || store.db == nil {
		return errors.New("SQLite connection is closed")
	}
	if err := store.db.PingContext(ctx); err != nil {
		return fmt.Errorf("check SQLite connection: %w", err)
	}
	return nil
}

func (store *Store) ClassifyError(err error) connection.ErrorKind {
	if kind := connection.ClassifyCommonError(err); kind != connection.ErrorUnknown {
		return kind
	}
	if err == nil {
		return connection.ErrorUnknown
	}
	message := strings.ToLower(err.Error())
	switch {
	case strings.Contains(message, "database is locked"), strings.Contains(message, "database is busy"):
		return connection.ErrorBusy
	case strings.Contains(message, "syntax error"), strings.Contains(message, "no such table"), strings.Contains(message, "no such column"):
		return connection.ErrorQuery
	default:
		return connection.ErrorUnknown
	}
}

func (store *Store) Query(ctx context.Context, statement string, batchSize int) <-chan sqlstream.Event {
	return sqlstream.Query(ctx, store.db, statement, batchSize)
}

// Namespaces lists attached SQLite databases. The main file remains the
// default query namespace; selecting another attachment is not supported by
// the workbench because attachments are connection-local and user-authored
// SQL must not be rewritten implicitly.
func (store *Store) Namespaces(ctx context.Context) ([]string, error) {
	rows, err := store.db.QueryContext(ctx, "PRAGMA database_list")
	if err != nil {
		return nil, fmt.Errorf("list SQLite databases: %w", err)
	}
	defer rows.Close()
	var names []string
	for rows.Next() {
		var sequence int
		var name, file string
		if err := rows.Scan(&sequence, &name, &file); err != nil {
			return nil, fmt.Errorf("read SQLite database list: %w", err)
		}
		names = append(names, name)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read SQLite database list: %w", err)
	}
	return names, nil
}

func (store *Store) CurrentNamespace(context.Context) (string, error) { return "main", nil }

func (store *Store) StartQuery(ctx context.Context, statement string) (*sqlstream.Cursor, error) {
	return sqlstream.Open(ctx, store.db, statement)
}

// Export reruns a statement on a separate read-only connection and streams
// the complete result to output without retaining it in memory.
func (store *Store) Export(ctx context.Context, statement string, format sqlstream.ExportFormat, output io.Writer) (int64, error) {
	readOnly, err := Open(ctx, store.path, ReadOnly)
	if err != nil {
		return 0, fmt.Errorf("open read-only export connection: %w", err)
	}
	defer readOnly.Close()

	cursor, err := readOnly.StartQuery(ctx, statement)
	if err != nil {
		return 0, fmt.Errorf("run export query read-only: %w", err)
	}
	defer cursor.Close()
	columns := cursor.Columns()

	switch format {
	case sqlstream.CSV:
		return sqlstream.WriteCSV(output, columns, cursor)
	case sqlstream.JSON:
		return sqlstream.WriteJSON(output, columns, cursor)
	default:
		return 0, fmt.Errorf("unsupported export format %d", format)
	}
}

// installReadOnlyAuthorizer constrains every physical connection opened with
// mode=ro. Opening only the main file read-only does not constrain attached
// databases, and PRAGMA query_only alone can be disabled by the SQL itself.
func installReadOnlyAuthorizer(driverConn modernsqlite.ExecQuerierContext) error {
	authorizer, ok := driverConn.(modernsqlite.AuthorizerRegisterer)
	if !ok {
		return errors.New("SQLite driver does not support query authorization")
	}
	return authorizer.RegisterAuthorizer(readOnlyAuthorizer())
}

func readOnlyAuthorizer() modernsqlite.AuthorizerFn {
	return func(action modernsqlite.AuthorizerActionCode, arg1, arg2, _, _ string) modernsqlite.AuthorizerReturnCode {
		switch action {
		case modernsqlite.AuthSelect, modernsqlite.AuthRead, modernsqlite.AuthRecursive:
			return modernsqlite.AuthorizerOK
		case modernsqlite.AuthFunction:
			// Keep read-only SQL from gaining filesystem access if an extension
			// registers SQLite's shell file I/O helpers in a future build.
			switch strings.ToLower(arg2) {
			case "load_extension", "readfile", "writefile":
				return modernsqlite.AuthorizerDeny
			default:
				return modernsqlite.AuthorizerOK
			}
		case modernsqlite.AuthPragma:
			if allowedReadOnlyPragma(arg1, arg2) {
				return modernsqlite.AuthorizerOK
			}
		}
		return modernsqlite.AuthorizerDeny
	}
}

func allowedReadOnlyPragma(name, argument string) bool {
	switch strings.ToLower(name) {
	case "table_xinfo", "table_info", "index_list", "index_info", "index_xinfo", "foreign_key_list", "foreign_key_check", "integrity_check", "quick_check":
		return true
	case "database_list", "compile_options", "data_version", "schema_version", "user_version", "application_id", "page_count", "page_size", "freelist_count", "journal_mode", "locking_mode", "query_only", "encoding", "foreign_keys", "busy_timeout":
		return argument == ""
	default:
		return false
	}
}

// Objects reads one keyset-paginated page of schema objects without reading
// the contents of those objects.
func (store *Store) Objects(ctx context.Context, afterSchema, afterType, afterName string, limit int) (ObjectPage, error) {
	if afterSchema != "" {
		return ObjectPage{}, errors.New("SQLite schema pagination cursor must be empty")
	}
	if limit <= 0 {
		limit = 200
	}
	if limit > 1000 {
		limit = 1000
	}
	query := `
		SELECT name, type, COALESCE(sql, '')
		FROM sqlite_schema
		WHERE type IN ('table', 'view', 'index', 'trigger')
		  AND lower(substr(name, 1, 7)) <> 'sqlite_'`
	args := make([]any, 0, 4)
	if afterType != "" || afterName != "" {
		query += ` AND (type > ? OR (type = ? AND name > ?))`
		args = append(args, afterType, afterType, afterName)
	}
	query += ` ORDER BY type, name LIMIT ?`
	args = append(args, limit+1)
	rows, err := store.db.QueryContext(ctx, query, args...)
	if err != nil {
		return ObjectPage{}, fmt.Errorf("list SQLite objects: %w", err)
	}
	defer rows.Close()

	objects := make([]Object, 0, limit+1)
	for rows.Next() {
		var object Object
		if err := rows.Scan(&object.Name, &object.Type, &object.SQL); err != nil {
			return ObjectPage{}, fmt.Errorf("read SQLite object metadata: %w", err)
		}
		objects = append(objects, object)
	}
	if err := rows.Err(); err != nil {
		return ObjectPage{}, fmt.Errorf("read SQLite object metadata: %w", err)
	}
	page := ObjectPage{Objects: objects}
	if len(page.Objects) > limit {
		page.HasMore = true
		page.Objects = page.Objects[:limit]
	}
	if len(page.Objects) > 0 {
		last := page.Objects[len(page.Objects)-1]
		page.NextSchema = last.Schema
		page.NextType = last.Type
		page.NextName = last.Name
	}
	return page, nil
}

// Columns returns table metadata, including hidden and generated columns. The
// identifier is quoted as a SQLite identifier, never interpolated as SQL.
func (store *Store) Columns(ctx context.Context, schema, table string) ([]Column, error) {
	if schema != "" {
		return nil, errors.New("SQLite objects do not have schemas")
	}
	quoted := `"` + strings.ReplaceAll(table, `"`, `""`) + `"`
	rows, err := store.db.QueryContext(ctx, "PRAGMA table_xinfo("+quoted+")")
	if err != nil {
		return nil, fmt.Errorf("list columns for %q: %w", table, err)
	}
	defer rows.Close()

	columns := make([]Column, 0)
	for rows.Next() {
		var (
			cid        int
			name       string
			dataType   string
			notNull    int
			defaultV   any
			primaryKey int
			hidden     int
		)
		if err := rows.Scan(&cid, &name, &dataType, &notNull, &defaultV, &primaryKey, &hidden); err != nil {
			return nil, fmt.Errorf("read columns for %q: %w", table, err)
		}
		columns = append(columns, Column{
			Position:   cid,
			Name:       name,
			Type:       dataType,
			NotNull:    notNull != 0,
			PrimaryKey: primaryKey != 0,
			Hidden:     hidden,
			Default:    defaultV,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read columns for %q: %w", table, err)
	}
	return columns, nil
}

var _ connection.SQL = (*Store)(nil)
var _ connection.Connector = Connector{}
