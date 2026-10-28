// Package mysql adapts MySQL to the workbench SQL contract.
package mysql

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"time"

	mysqldriver "github.com/go-sql-driver/mysql"
	"github.com/magalab/dbl/internal/connection"
	"github.com/magalab/dbl/internal/credentials"
	"github.com/magalab/dbl/internal/preferences"
	"github.com/magalab/dbl/internal/sqlstream"
)

const (
	defaultPort        = 3306
	connectTimeout     = 10 * time.Second
	maxOpenConnections = 5
)

// Connector opens a MySQL connection from non-secret profile metadata and an
// optional OS-vault reference.
type Connector struct {
	Profile preferences.ConnectionProfile
	Vault   *credentials.Vault
}

func (connector Connector) Connect(ctx context.Context) (connection.Connection, error) {
	if connector.Profile.Driver != preferences.DriverMySQL {
		return nil, errors.New("MySQL connector received a different database type")
	}
	if strings.TrimSpace(connector.Profile.Host) == "" {
		return nil, errors.New("MySQL host is required")
	}

	var password []byte
	if connector.Profile.CredentialRef != "" {
		secret, err := connector.Vault.Load(credentials.Reference(connector.Profile.CredentialRef))
		if err != nil {
			return nil, fmt.Errorf("load MySQL credential: %w", err)
		}
		password = secret
	}
	defer clear(password)

	config, err := buildConfig(connector.Profile, string(password))
	if err != nil {
		return nil, err
	}
	driverConnector, err := mysqldriver.NewConnector(config)
	if err != nil {
		return nil, redactError("configure MySQL connection", err, config.Passwd)
	}
	db := sql.OpenDB(driverConnector)
	db.SetMaxOpenConns(maxOpenConnections)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(4 * time.Minute)

	pingCtx, cancel := context.WithTimeout(ctx, connectTimeout)
	defer cancel()
	if err := db.PingContext(pingCtx); err != nil {
		_ = db.Close()
		return nil, redactError("connect to MySQL", err, config.Passwd)
	}
	return &Store{db: db, config: config.Clone(), readOnly: connector.Profile.ReadOnly}, nil
}

func buildConfig(profile preferences.ConnectionProfile, password string) (*mysqldriver.Config, error) {
	if profile.Port < 1 || profile.Port > 65535 {
		if profile.Port == 0 {
			profile.Port = defaultPort
		} else {
			return nil, errors.New("MySQL port must be between 1 and 65535")
		}
	}
	tlsMode := profile.TLSMode
	if tlsMode == "" {
		tlsMode = "verify-full"
	}
	tlsConfig, allowFallback, err := buildTLSConfigWithCA(tlsMode, strings.TrimSpace(profile.Host), profile.TLSCAPath)
	if err != nil {
		return nil, err
	}
	config := mysqldriver.NewConfig()
	config.Net = "tcp"
	config.Addr = net.JoinHostPort(strings.TrimSpace(profile.Host), fmt.Sprint(profile.Port))
	config.User = profile.Username
	config.Passwd = password
	config.DBName = profile.Database
	config.Timeout = connectTimeout
	config.TLS = tlsConfig
	config.AllowFallbackToPlaintext = allowFallback
	config.ParseTime = true
	if err := config.Apply(mysqldriver.Charset("utf8mb4", "")); err != nil {
		return nil, fmt.Errorf("configure MySQL character set: %w", err)
	}
	return config, nil
}

func buildTLSConfig(mode, host string) (*tls.Config, bool, error) {
	return buildTLSConfigWithCA(mode, host, "")
}

func buildTLSConfigWithCA(mode, host, caPath string) (*tls.Config, bool, error) {
	if caPath != "" && mode != "verify-full" && mode != "verify-ca" {
		return nil, false, errors.New("MySQL custom CA requires verify-full or verify-ca TLS mode")
	}
	switch mode {
	case "disable":
		return nil, false, nil
	case "verify-full", "verify-ca":
		roots, err := x509.SystemCertPool()
		if err != nil {
			return nil, false, fmt.Errorf("load system certificate roots for MySQL TLS: %w", err)
		}
		if roots == nil {
			return nil, false, errors.New("system certificate roots are unavailable for MySQL TLS")
		}
		if caPath != "" {
			pem, err := os.ReadFile(caPath)
			if err != nil {
				return nil, false, fmt.Errorf("read MySQL CA certificate: %w", err)
			}
			if !roots.AppendCertsFromPEM(pem) {
				return nil, false, errors.New("MySQL CA file contains no valid certificates")
			}
		}
		config := &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: roots, ServerName: host}
		if mode == "verify-full" {
			return config, false, nil
		}
		config.InsecureSkipVerify = true // Certificate and chain are checked below; hostname verification is intentionally omitted.
		config.VerifyConnection = func(state tls.ConnectionState) error {
			if len(state.PeerCertificates) == 0 {
				return errors.New("MySQL server did not provide a TLS certificate")
			}
			intermediates := x509.NewCertPool()
			for _, certificate := range state.PeerCertificates[1:] {
				intermediates.AddCert(certificate)
			}
			_, err := state.PeerCertificates[0].Verify(x509.VerifyOptions{
				Roots: roots, Intermediates: intermediates,
				KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
			})
			return err
		}
		return config, false, nil
	case "require", "prefer":
		return &tls.Config{MinVersion: tls.VersionTLS12, ServerName: host, InsecureSkipVerify: true}, mode == "prefer", nil
	default:
		return nil, false, errors.New("unsupported MySQL TLS mode")
	}
}

// Store owns one MySQL pool and its connection configuration.
type Store struct {
	db       *sql.DB
	config   *mysqldriver.Config
	readOnly bool
}

func (store *Store) Driver() connection.Driver { return connection.DriverMySQL }

func (store *Store) HealthCheck(ctx context.Context) error {
	if store == nil || store.db == nil {
		return errors.New("MySQL connection is closed")
	}
	return redactError("check MySQL connection", store.db.PingContext(ctx), store.password())
}

func (store *Store) ClassifyError(err error) connection.ErrorKind {
	if kind := connection.ClassifyCommonError(err); kind != connection.ErrorUnknown {
		return kind
	}
	var mysqlErr *mysqldriver.MySQLError
	if !errors.As(err, &mysqlErr) {
		return connection.ErrorUnknown
	}
	switch mysqlErr.Number {
	case 1317, 1927:
		return connection.ErrorCanceled
	case 1049, 1051, 1054, 1146:
		return connection.ErrorNotFound
	case 1044, 1045, 1141, 1142, 1143, 1227, 1370:
		return connection.ErrorPermission
	case 1205, 1213:
		return connection.ErrorBusy
	case 3024, 1969:
		return connection.ErrorTimeout
	}
	if string(mysqlErr.SQLState[:2]) == "42" {
		return connection.ErrorQuery
	}
	return connection.ErrorUnknown
}

func (store *Store) Close() error {
	if store == nil || store.db == nil {
		return nil
	}
	return store.db.Close()
}

func (store *Store) Namespaces(ctx context.Context) ([]string, error) {
	rows, err := store.db.QueryContext(ctx, "SELECT schema_name FROM information_schema.schemata ORDER BY schema_name")
	if err != nil {
		return nil, redactError("list MySQL databases", err, store.password())
	}
	defer rows.Close()
	var databases []string
	for rows.Next() {
		var database string
		if err := rows.Scan(&database); err != nil {
			return nil, redactError("read MySQL databases", err, store.password())
		}
		databases = append(databases, database)
	}
	if err := rows.Err(); err != nil {
		return nil, redactError("read MySQL databases", err, store.password())
	}
	return databases, nil
}

func (store *Store) CurrentNamespace(ctx context.Context) (string, error) {
	var database sql.NullString
	if err := store.db.QueryRowContext(ctx, "SELECT DATABASE()").Scan(&database); err != nil {
		return "", redactError("read current MySQL database", err, store.password())
	}
	return database.String, nil
}

func (store *Store) StartQuery(ctx context.Context, statement string) (*sqlstream.Cursor, error) {
	if store == nil || store.db == nil {
		return nil, errors.New("MySQL connection is closed")
	}
	if store.readOnly {
		tx, err := store.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
		if err != nil {
			return nil, redactError("start read-only MySQL query", err, store.password())
		}
		cursor, err := sqlstream.Open(ctx, tx, statement)
		if err != nil {
			_ = tx.Rollback()
			return nil, redactError("run read-only MySQL query", err, store.password())
		}
		cursor.OnFinish(func(completed bool) error {
			if completed {
				return redactError("finish read-only MySQL query", tx.Commit(), store.password())
			}
			if err := tx.Rollback(); err != nil && !errors.Is(err, sql.ErrTxDone) {
				return redactError("stop read-only MySQL query", err, store.password())
			}
			return nil
		})
		return cursor, nil
	}
	return sqlstream.Open(ctx, store.db, statement)
}

// StartQueryWithCancellation pins the query to a known MySQL session and
// registers a server-side KILL QUERY action before starting it. Closing a
// client socket alone may not interrupt a long aggregate until MySQL tries to
// send its result.
func (store *Store) StartQueryWithCancellation(ctx context.Context, statement string, register func(connection.QueryCancel)) (*sqlstream.Cursor, error) {
	if store == nil || store.db == nil {
		return nil, errors.New("MySQL connection is closed")
	}
	conn, err := store.db.Conn(ctx)
	if err != nil {
		return nil, redactError("prepare cancellable MySQL query", err, store.password())
	}

	var connectionID uint64
	if err := conn.QueryRowContext(ctx, "SELECT CONNECTION_ID()").Scan(&connectionID); err != nil {
		_ = conn.Close()
		return nil, redactError("identify MySQL query session", err, store.password())
	}
	if register != nil {
		register(func(cancelCtx context.Context) error {
			killer, err := store.db.Conn(cancelCtx)
			if err != nil {
				return redactError("open MySQL query cancellation connection", err, store.password())
			}
			defer killer.Close()
			_, err = killer.ExecContext(cancelCtx, fmt.Sprintf("KILL QUERY %d", connectionID))
			var mysqlErr *mysqldriver.MySQLError
			if errors.As(err, &mysqlErr) && mysqlErr.Number == 1094 {
				// The query completed or its session disconnected before KILL ran.
				return nil
			}
			return redactError("cancel MySQL query on the server", err, store.password())
		})
	}

	var queryer sqlstream.QueryerContext = conn
	var tx *sql.Tx
	if store.readOnly {
		tx, err = conn.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
		if err != nil {
			_ = conn.Close()
			return nil, redactError("start read-only MySQL query", err, store.password())
		}
		queryer = tx
	}
	cursor, err := sqlstream.Open(ctx, queryer, statement)
	if err != nil {
		if tx != nil {
			_ = tx.Rollback()
		}
		_ = conn.Close()
		return nil, redactError("run cancellable MySQL query", err, store.password())
	}
	cursor.OnFinish(func(completed bool) error {
		var finishErr error
		if tx != nil {
			if completed {
				finishErr = tx.Commit()
			} else if err := tx.Rollback(); err != nil && !errors.Is(err, sql.ErrTxDone) {
				finishErr = err
			}
		}
		if closeErr := conn.Close(); finishErr == nil {
			finishErr = closeErr
		}
		if finishErr != nil {
			return redactError("finish cancellable MySQL query", finishErr, store.password())
		}
		return nil
	})
	return cursor, nil
}

func (store *Store) Objects(ctx context.Context, afterSchema, afterType, afterName string, limit int) (connection.ObjectPage, error) {
	if limit <= 0 {
		limit = 200
	}
	if limit > 1000 {
		limit = 1000
	}
	const query = `
		SELECT schema_name, object_name, object_type, definition
		FROM (
			SELECT schema_name, schema_name AS object_name,
			       'schema' AS object_type, CAST('' AS CHAR) AS definition
			FROM information_schema.schemata
			WHERE schema_name NOT IN ('information_schema', 'mysql', 'performance_schema', 'sys')
			UNION ALL
			SELECT tables.table_schema, tables.table_name,
			       CASE WHEN tables.table_type = 'VIEW' THEN 'view' ELSE 'table' END,
			       COALESCE(views.view_definition, '')
			FROM information_schema.tables AS tables
			LEFT JOIN information_schema.views AS views
			  ON views.table_schema = tables.table_schema AND views.table_name = tables.table_name
			WHERE tables.table_schema NOT IN ('information_schema', 'mysql', 'performance_schema', 'sys')
		) AS objects
		WHERE CAST(schema_name AS BINARY) > CAST(? AS BINARY)
		   OR (CAST(schema_name AS BINARY) = CAST(? AS BINARY) AND CAST(object_type AS BINARY) > CAST(? AS BINARY))
		   OR (CAST(schema_name AS BINARY) = CAST(? AS BINARY) AND CAST(object_type AS BINARY) = CAST(? AS BINARY) AND CAST(object_name AS BINARY) > CAST(? AS BINARY))
		ORDER BY CAST(schema_name AS BINARY), CAST(object_type AS BINARY), CAST(object_name AS BINARY)
		LIMIT ?`
	rows, err := store.db.QueryContext(ctx, query,
		afterSchema, afterSchema, afterType, afterSchema, afterType, afterName, limit+1)
	if err != nil {
		return connection.ObjectPage{}, redactError("list MySQL schemas and tables", err, store.password())
	}
	defer rows.Close()
	objects := make([]connection.Object, 0, limit+1)
	for rows.Next() {
		var object connection.Object
		if err := rows.Scan(&object.Schema, &object.Name, &object.Type, &object.SQL); err != nil {
			return connection.ObjectPage{}, redactError("read MySQL object metadata", err, store.password())
		}
		objects = append(objects, object)
	}
	if err := rows.Err(); err != nil {
		return connection.ObjectPage{}, redactError("read MySQL object metadata", err, store.password())
	}
	page := connection.ObjectPage{Objects: objects}
	if len(objects) > limit {
		page.HasMore = true
		page.Objects = page.Objects[:limit]
	}
	if len(page.Objects) > 0 {
		last := page.Objects[len(page.Objects)-1]
		page.NextSchema, page.NextType, page.NextName = last.Schema, last.Type, last.Name
	}
	return page, nil
}

func (store *Store) Columns(ctx context.Context, schema, table string) ([]connection.Column, error) {
	if schema == "" || table == "" {
		return nil, errors.New("MySQL schema and table are required")
	}
	const query = `
		SELECT ordinal_position, column_name, column_type, is_nullable = 'NO',
		       column_key = 'PRI', extra LIKE '%GENERATED%' OR extra LIKE '%auto_increment%' OR extra LIKE '%INVISIBLE%',
	       column_default, extra, collation_name, column_comment
		FROM information_schema.columns
		WHERE table_schema = ? AND table_name = ?
		ORDER BY ordinal_position`
	rows, err := store.db.QueryContext(ctx, query, schema, table)
	if err != nil {
		return nil, redactError("list MySQL columns", err, store.password())
	}
	defer rows.Close()
	columns := make([]connection.Column, 0)
	for rows.Next() {
		var (
			column    connection.Column
			hidden    bool
			def       sql.NullString
			collation sql.NullString
		)
		if err := rows.Scan(&column.Position, &column.Name, &column.Type, &column.NotNull, &column.PrimaryKey, &hidden, &def, &column.Extra, &collation, &column.Comment); err != nil {
			return nil, redactError("read MySQL column metadata", err, store.password())
		}
		if collation.Valid {
			column.Collation = collation.String
		}
		if hidden {
			column.Hidden = 1
		}
		if def.Valid {
			column.Default = def.String
		}
		columns = append(columns, column)
	}
	if err := rows.Err(); err != nil {
		return nil, redactError("read MySQL column metadata", err, store.password())
	}
	return columns, nil
}

func (store *Store) Export(ctx context.Context, statement string, format sqlstream.ExportFormat, output io.Writer) (int64, error) {
	if store == nil || store.config == nil {
		return 0, errors.New("MySQL connection is closed")
	}
	config := store.config.Clone()
	driverConnector, err := mysqldriver.NewConnector(config)
	if err != nil {
		return 0, redactError("configure MySQL export connection", err, config.Passwd)
	}
	db := sql.OpenDB(driverConnector)
	db.SetMaxOpenConns(1)
	defer db.Close()
	if err := db.PingContext(ctx); err != nil {
		return 0, redactError("open MySQL export connection", err, config.Passwd)
	}
	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return 0, redactError("start read-only MySQL export transaction", err, config.Passwd)
	}
	defer tx.Rollback()
	cursor, err := sqlstream.Open(ctx, tx, statement)
	if err != nil {
		return 0, redactError("run MySQL export query", err, config.Passwd)
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

func (store *Store) password() string {
	if store == nil || store.config == nil {
		return ""
	}
	return store.config.Passwd
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
