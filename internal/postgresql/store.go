// Package postgresql adapts PostgreSQL to the workbench SQL contract.
package postgresql

import (
	"context"
	"crypto/x509"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/magalab/dbl/internal/connection"
	"github.com/magalab/dbl/internal/credentials"
	"github.com/magalab/dbl/internal/preferences"
	"github.com/magalab/dbl/internal/sqlstream"
)

const (
	defaultPort        = 5432
	connectTimeout     = 10 * time.Second
	maxOpenConnections = 5
)

// Connector opens a PostgreSQL connection from non-secret profile metadata
// and an optional OS-vault reference.
type Connector struct {
	Profile    preferences.ConnectionProfile
	Vault      *credentials.Vault
	SearchPath string
}

func (connector Connector) Connect(ctx context.Context) (connection.Connection, error) {
	if connector.Profile.Driver != preferences.DriverPostgreSQL {
		return nil, errors.New("PostgreSQL connector received a different database type")
	}
	if strings.TrimSpace(connector.Profile.Host) == "" {
		return nil, errors.New("PostgreSQL host is required")
	}

	var password []byte
	if connector.Profile.CredentialRef != "" {
		secret, err := connector.Vault.Load(credentials.Reference(connector.Profile.CredentialRef))
		if err != nil {
			return nil, fmt.Errorf("load PostgreSQL credential: %w", err)
		}
		password = secret
	}
	defer clear(password)

	config, err := buildConfig(connector.Profile, string(password))
	if err != nil {
		return nil, err
	}
	if connector.SearchPath != "" {
		config.RuntimeParams["search_path"] = quoteSearchPathIdentifier(connector.SearchPath)
	}
	db := stdlib.OpenDB(*config)
	db.SetMaxOpenConns(maxOpenConnections)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(30 * time.Minute)

	pingCtx, cancel := context.WithTimeout(ctx, connectTimeout)
	defer cancel()
	if err := db.PingContext(pingCtx); err != nil {
		_ = db.Close()
		return nil, redactError("connect to PostgreSQL", err, config.Password)
	}
	return &Store{db: db, config: config.Copy(), readOnly: connector.Profile.ReadOnly}, nil
}

func quoteSearchPathIdentifier(schema string) string {
	return `"` + strings.ReplaceAll(schema, `"`, `""`) + `"`
}

func buildConfig(profile preferences.ConnectionProfile, password string) (*pgx.ConnConfig, error) {
	if profile.Port < 1 || profile.Port > 65535 {
		if profile.Port == 0 {
			profile.Port = defaultPort
		} else {
			return nil, errors.New("PostgreSQL port must be between 1 and 65535")
		}
	}
	tlsMode := profile.TLSMode
	if tlsMode == "" {
		tlsMode = "verify-full"
	}
	switch tlsMode {
	case "verify-full", "verify-ca", "require", "prefer", "disable":
	default:
		return nil, errors.New("unsupported PostgreSQL TLS mode")
	}
	if profile.TLSCAPath != "" && tlsMode != "verify-full" && tlsMode != "verify-ca" {
		return nil, errors.New("PostgreSQL custom CA requires verify-full or verify-ca TLS mode")
	}
	parseOptions := "host=localhost sslmode=" + tlsMode
	config, err := pgx.ParseConfig(parseOptions)
	if err != nil {
		return nil, fmt.Errorf("configure PostgreSQL TLS: %w", err)
	}
	config.Host = strings.TrimSpace(profile.Host)
	config.Port = uint16(profile.Port)
	config.Database = profile.Database
	config.User = profile.Username
	config.Password = password
	if config.TLSConfig != nil {
		config.TLSConfig.ServerName = config.Host
		if tlsMode == "verify-full" || tlsMode == "verify-ca" {
			roots, poolErr := x509.SystemCertPool()
			if poolErr != nil || roots == nil {
				roots = x509.NewCertPool()
			}
			if profile.TLSCAPath != "" {
				pem, readErr := os.ReadFile(profile.TLSCAPath)
				if readErr != nil {
					return nil, fmt.Errorf("read PostgreSQL CA certificate: %w", readErr)
				}
				if !roots.AppendCertsFromPEM(pem) {
					return nil, errors.New("PostgreSQL CA file contains no valid certificates")
				}
			}
			config.TLSConfig.RootCAs = roots
		}
	}
	config.RuntimeParams["application_name"] = "DBL"
	if profile.ReadOnly {
		config.RuntimeParams["default_transaction_read_only"] = "on"
	}
	return config, nil
}

// Store owns one PostgreSQL pool and its connection configuration.
type Store struct {
	db       *sql.DB
	config   *pgx.ConnConfig
	readOnly bool
}

func (store *Store) Driver() connection.Driver { return connection.DriverPostgreSQL }

func (store *Store) SQLVersion(ctx context.Context) (string, error) {
	var version string
	err := store.db.QueryRowContext(ctx, "SHOW server_version").Scan(&version)
	return version, err
}

func (store *Store) HealthCheck(ctx context.Context) error {
	if store == nil || store.db == nil {
		return errors.New("PostgreSQL connection is closed")
	}
	return store.db.PingContext(ctx)
}

func (store *Store) ClassifyError(err error) connection.ErrorKind {
	if kind := connection.ClassifyCommonError(err); kind != connection.ErrorUnknown {
		return kind
	}
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return connection.ErrorUnknown
	}
	switch {
	case pgErr.Code == "57014": // query_canceled
		return connection.ErrorCanceled
	case strings.HasPrefix(pgErr.Code, "28"), pgErr.Code == "42501":
		return connection.ErrorPermission
	case strings.HasPrefix(pgErr.Code, "42"), pgErr.Code == "3F000":
		return connection.ErrorQuery
	case pgErr.Code == "40001" || pgErr.Code == "40P01":
		return connection.ErrorBusy
	default:
		return connection.ErrorUnknown
	}
}

func (store *Store) Close() error {
	if store == nil || store.db == nil {
		return nil
	}
	return store.db.Close()
}

func (store *Store) Namespaces(ctx context.Context) ([]string, error) {
	const query = `SELECT nspname FROM pg_catalog.pg_namespace WHERE nspname <> 'information_schema' AND nspname NOT LIKE 'pg\_%' ESCAPE '\' AND pg_catalog.has_schema_privilege(oid, 'USAGE') ORDER BY nspname`
	rows, err := store.db.QueryContext(ctx, query)
	if err != nil {
		return nil, redactError("list PostgreSQL schemas", err, store.config.Password)
	}
	defer rows.Close()
	var schemas []string
	for rows.Next() {
		var schema string
		if err := rows.Scan(&schema); err != nil {
			return nil, redactError("read PostgreSQL schemas", err, store.config.Password)
		}
		schemas = append(schemas, schema)
	}
	if err := rows.Err(); err != nil {
		return nil, redactError("read PostgreSQL schemas", err, store.config.Password)
	}
	return schemas, nil
}

func (store *Store) CurrentNamespace(ctx context.Context) (string, error) {
	var schema sql.NullString
	if err := store.db.QueryRowContext(ctx, "SELECT pg_catalog.current_schema()").Scan(&schema); err != nil {
		return "", redactError("read current PostgreSQL schema", err, store.config.Password)
	}
	return schema.String, nil
}

func (store *Store) CurrentDatabase(ctx context.Context) (string, error) {
	var database string
	err := store.db.QueryRowContext(ctx, "SELECT current_database()").Scan(&database)
	return database, err
}

func (store *Store) StartQuery(ctx context.Context, statement string) (*sqlstream.Cursor, error) {
	if store == nil || store.db == nil {
		return nil, errors.New("PostgreSQL connection is closed")
	}
	if store.readOnly {
		tx, err := store.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
		if err != nil {
			return nil, redactError("start read-only PostgreSQL query", err, store.config.Password)
		}
		cursor, err := sqlstream.Open(ctx, tx, statement)
		if err != nil {
			_ = tx.Rollback()
			return nil, redactError("run read-only PostgreSQL query", err, store.config.Password)
		}
		cursor.OnFinish(func(completed bool) error {
			if completed {
				return redactError("finish read-only PostgreSQL query", tx.Commit(), store.config.Password)
			}
			if err := tx.Rollback(); err != nil && !errors.Is(err, sql.ErrTxDone) {
				return redactError("stop read-only PostgreSQL query", err, store.config.Password)
			}
			return nil
		})
		return cursor, nil
	}
	return sqlstream.Open(ctx, store.db, statement)
}

func (store *Store) ExecStatement(ctx context.Context, statement string) (int64, error) {
	if store == nil || store.db == nil {
		return 0, errors.New("PostgreSQL connection is closed")
	}
	if store.readOnly {
		tx, err := store.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
		if err != nil {
			return 0, redactError("start read-only PostgreSQL statement", err, store.config.Password)
		}
		defer tx.Rollback()
		result, err := tx.ExecContext(ctx, statement)
		if err != nil {
			return 0, redactError("execute read-only PostgreSQL statement", err, store.config.Password)
		}
		count, err := result.RowsAffected()
		if err != nil {
			return 0, redactError("read PostgreSQL affected row count", err, store.config.Password)
		}
		if err := tx.Commit(); err != nil {
			return 0, redactError("finish read-only PostgreSQL statement", err, store.config.Password)
		}
		return count, nil
	}
	result, err := store.db.ExecContext(ctx, statement)
	if err != nil {
		return 0, redactError("execute PostgreSQL statement", err, store.config.Password)
	}
	count, err := result.RowsAffected()
	return count, redactError("read PostgreSQL affected row count", err, store.config.Password)
}

func (store *Store) Objects(ctx context.Context, afterSchema, afterType, afterName string, limit int) (connection.ObjectPage, error) {
	if limit <= 0 {
		limit = 200
	}
	if limit > 1000 {
		limit = 1000
	}
	const query = `
		WITH objects AS (
			SELECT n.nspname AS schema_name, n.nspname AS object_name,
			       'schema'::text AS object_type, ''::text AS definition, ''::text AS table_name
			FROM pg_catalog.pg_namespace AS n
			WHERE n.nspname <> 'information_schema' AND n.nspname NOT LIKE 'pg\_%' ESCAPE '\'
			UNION ALL
			SELECT n.nspname AS schema_name, c.relname AS object_name,
			       CASE c.relkind
			           WHEN 'v' THEN 'view'::text
			           WHEN 'm' THEN 'materialized_view'::text
			           WHEN 'S' THEN 'sequence'::text
			           ELSE 'table'::text
			       END AS object_type,
			       CASE WHEN c.relkind IN ('v', 'm') THEN pg_catalog.pg_get_viewdef(c.oid, true) ELSE ''::text END AS definition,
			       ''::text AS table_name
			FROM pg_catalog.pg_class AS c
			JOIN pg_catalog.pg_namespace AS n ON n.oid = c.relnamespace
			WHERE c.relkind IN ('r', 'p', 'f', 'v', 'm', 'S')
			  AND n.nspname <> 'information_schema' AND n.nspname NOT LIKE 'pg\_%' ESCAPE '\'
			UNION ALL
			SELECT n.nspname AS schema_name,
			       p.proname || '(' || pg_catalog.pg_get_function_identity_arguments(p.oid) || ')' AS object_name,
			       CASE WHEN pg_catalog.to_jsonb(p)->>'prokind' = 'p' THEN 'procedure'::text ELSE 'function'::text END AS object_type,
			       pg_catalog.pg_get_functiondef(p.oid) AS definition, ''::text AS table_name
			FROM pg_catalog.pg_proc AS p
			JOIN pg_catalog.pg_namespace AS n ON n.oid = p.pronamespace
			WHERE NOT COALESCE((pg_catalog.to_jsonb(p)->>'proisagg')::boolean, false)
			  AND COALESCE(pg_catalog.to_jsonb(p)->>'prokind', 'f') IN ('f', 'p', 'w')
			  AND n.nspname <> 'information_schema' AND n.nspname NOT LIKE 'pg\_%' ESCAPE '\'
			UNION ALL
			SELECT n.nspname AS schema_name, t.typname AS object_name,
			       'type'::text AS object_type, ''::text AS definition, ''::text AS table_name
			FROM pg_catalog.pg_type AS t
			JOIN pg_catalog.pg_namespace AS n ON n.oid = t.typnamespace
			WHERE t.typtype IN ('d', 'e', 'r', 'm') AND t.typisdefined
			  AND t.typname NOT LIKE '\_%' ESCAPE '\'
			  AND n.nspname <> 'information_schema' AND n.nspname NOT LIKE 'pg\_%' ESCAPE '\'
			UNION ALL
			SELECT n.nspname AS schema_name, e.extname AS object_name,
			       'extension'::text AS object_type,
			       'CREATE EXTENSION IF NOT EXISTS ' || pg_catalog.quote_ident(e.extname) || ';' AS definition,
			       ''::text AS table_name
			FROM pg_catalog.pg_extension AS e
			JOIN pg_catalog.pg_namespace AS n ON n.oid = e.extnamespace
			WHERE n.nspname <> 'information_schema' AND n.nspname NOT LIKE 'pg\_%' ESCAPE '\'
			UNION ALL
			SELECT n.nspname AS schema_name, t.tgname AS object_name,
			       'trigger'::text AS object_type, pg_catalog.pg_get_triggerdef(t.oid, true) AS definition,
			       c.relname AS table_name
			FROM pg_catalog.pg_trigger AS t
			JOIN pg_catalog.pg_class AS c ON c.oid = t.tgrelid
			JOIN pg_catalog.pg_namespace AS n ON n.oid = c.relnamespace
			WHERE NOT t.tgisinternal AND n.nspname <> 'information_schema' AND n.nspname NOT LIKE 'pg\_%' ESCAPE '\'
			UNION ALL
			SELECT ''::text AS schema_name, et.evtname AS object_name,
			       'event_trigger'::text AS object_type,
			       'CREATE EVENT TRIGGER ' || pg_catalog.quote_ident(et.evtname) || ' ON ' || et.evtevent ||
			       CASE WHEN et.evttags IS NULL OR pg_catalog.array_length(et.evttags, 1) IS NULL THEN '' ELSE
			           ' WHEN TAG IN (' || pg_catalog.array_to_string(ARRAY(
			               SELECT pg_catalog.quote_literal(tag) FROM pg_catalog.unnest(et.evttags) AS event_tags(tag)
			           ), ', ') || ')'
			       END || ' EXECUTE PROCEDURE ' || pg_catalog.quote_ident(pn.nspname) || '.' || pg_catalog.quote_ident(p.proname) || '();' AS definition,
			       ''::text AS table_name
			FROM pg_catalog.pg_event_trigger AS et
			JOIN pg_catalog.pg_proc AS p ON p.oid = et.evtfoid
			JOIN pg_catalog.pg_namespace AS pn ON pn.oid = p.pronamespace
		)
		SELECT schema_name, object_name, object_type, definition, table_name
		FROM objects
		WHERE schema_name > $1
		   OR (schema_name = $1 AND object_type > $2)
		   OR (schema_name = $1 AND object_type = $2 AND object_name > $3)
		ORDER BY schema_name, object_type, object_name
		LIMIT $4`
	rows, err := store.db.QueryContext(ctx, query, afterSchema, afterType, afterName, limit+1)
	if err != nil {
		return connection.ObjectPage{}, redactError("list PostgreSQL schemas and tables", err, store.config.Password)
	}
	defer rows.Close()
	objects := make([]connection.Object, 0, limit+1)
	for rows.Next() {
		var object connection.Object
		if err := rows.Scan(&object.Schema, &object.Name, &object.Type, &object.SQL, &object.TableName); err != nil {
			return connection.ObjectPage{}, redactError("read PostgreSQL object metadata", err, store.config.Password)
		}
		objects = append(objects, object)
	}
	if err := rows.Err(); err != nil {
		return connection.ObjectPage{}, redactError("read PostgreSQL object metadata", err, store.config.Password)
	}
	page := connection.ObjectPage{Objects: objects}
	if len(objects) > limit {
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

func (store *Store) Columns(ctx context.Context, schema, table string) ([]connection.Column, error) {
	if schema == "" || table == "" {
		return nil, errors.New("PostgreSQL schema and table are required")
	}
	const query = `
		SELECT a.attnum,
		       a.attname,
		       pg_catalog.format_type(a.atttypid, a.atttypmod),
		       a.attnotnull,
		       EXISTS (
		           SELECT 1 FROM pg_catalog.pg_index AS i
		           WHERE i.indrelid = c.oid AND i.indisprimary AND a.attnum = ANY(i.indkey)
		       ),
		       (a.attgenerated <> '' OR a.attidentity <> ''),
		       pg_catalog.pg_get_expr(d.adbin, d.adrelid)
		FROM pg_catalog.pg_attribute AS a
		JOIN pg_catalog.pg_class AS c ON c.oid = a.attrelid
		JOIN pg_catalog.pg_namespace AS n ON n.oid = c.relnamespace
		LEFT JOIN pg_catalog.pg_attrdef AS d ON d.adrelid = c.oid AND d.adnum = a.attnum
		WHERE n.nspname = $1 AND c.relname = $2 AND a.attnum > 0 AND NOT a.attisdropped
		ORDER BY a.attnum`
	rows, err := store.db.QueryContext(ctx, query, schema, table)
	if err != nil {
		return nil, redactError("list PostgreSQL columns", err, store.config.Password)
	}
	defer rows.Close()
	columns := make([]connection.Column, 0)
	for rows.Next() {
		var (
			column    connection.Column
			generated bool
			def       sql.NullString
		)
		if err := rows.Scan(&column.Position, &column.Name, &column.Type, &column.NotNull, &column.PrimaryKey, &generated, &def); err != nil {
			return nil, redactError("read PostgreSQL column metadata", err, store.config.Password)
		}
		if generated {
			column.Hidden = 1
		}
		if def.Valid {
			column.Default = def.String
		}
		columns = append(columns, column)
	}
	if err := rows.Err(); err != nil {
		return nil, redactError("read PostgreSQL column metadata", err, store.config.Password)
	}
	return columns, nil
}

func (store *Store) Export(ctx context.Context, statement string, format sqlstream.ExportFormat, output io.Writer) (int64, error) {
	if store == nil || store.config == nil {
		return 0, errors.New("PostgreSQL connection is closed")
	}
	config := store.config.Copy()
	config.RuntimeParams = cloneParams(config.RuntimeParams)
	config.RuntimeParams["default_transaction_read_only"] = "on"
	db := stdlib.OpenDB(*config)
	db.SetMaxOpenConns(1)
	defer db.Close()
	if err := db.PingContext(ctx); err != nil {
		return 0, redactError("open read-only PostgreSQL export connection", err, config.Password)
	}
	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return 0, redactError("start read-only PostgreSQL export transaction", err, config.Password)
	}
	defer tx.Rollback()
	cursor, err := sqlstream.Open(ctx, tx, statement)
	if err != nil {
		return 0, redactError("run PostgreSQL export query", err, config.Password)
	}
	defer cursor.Close()
	switch format {
	case sqlstream.CSV:
		return sqlstream.WriteCSV(output, cursor.Columns(), cursor)
	case sqlstream.JSON:
		return sqlstream.WriteJSON(output, cursor.Columns(), cursor)
	default:
		return 0, fmt.Errorf("unsupported export format %d", format)
	}
}

func cloneParams(params map[string]string) map[string]string {
	copy := make(map[string]string, len(params)+1)
	for key, value := range params {
		copy[key] = value
	}
	return copy
}

func redactError(operation string, err error, password string) error {
	if err == nil {
		return nil
	}
	message := err.Error()
	if password != "" {
		message = strings.ReplaceAll(message, password, "[redacted]")
	}
	return fmt.Errorf("%s: %s", operation, message)
}

var _ connection.SQL = (*Store)(nil)
var _ connection.Connector = Connector{}
