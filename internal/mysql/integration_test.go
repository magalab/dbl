package mysql

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/magalab/dbl/internal/connection"
	"github.com/magalab/dbl/internal/credentials"
	"github.com/magalab/dbl/internal/preferences"
	"github.com/magalab/dbl/internal/sqlstream"
)

func TestMySQLIntegration(t *testing.T) {
	host := strings.TrimSpace(os.Getenv("DBL_TEST_MYSQL_HOST"))
	if host == "" {
		t.Skip("set DBL_TEST_MYSQL_HOST to run MySQL integration checks")
	}
	port, err := strconv.Atoi(os.Getenv("DBL_TEST_MYSQL_PORT"))
	if err != nil || port < 1 || port > 65535 {
		t.Fatal("DBL_TEST_MYSQL_PORT must be a valid TCP port")
	}
	database := os.Getenv("DBL_TEST_MYSQL_DATABASE")
	username := os.Getenv("DBL_TEST_MYSQL_USERNAME")
	password := []byte(os.Getenv("DBL_TEST_MYSQL_PASSWORD"))
	if database == "" || username == "" {
		t.Fatal("DBL_TEST_MYSQL_DATABASE and DBL_TEST_MYSQL_USERNAME are required")
	}

	vault := credentials.New(&memoryVault{})
	var credentialRef string
	if len(password) > 0 {
		ref, err := vault.Store(password)
		if err != nil {
			clear(password)
			t.Fatal(err)
		}
		credentialRef = string(ref)
	}
	clear(password)
	tlsMode := strings.TrimSpace(os.Getenv("DBL_TEST_MYSQL_TLS_MODE"))
	if tlsMode == "" {
		tlsMode = "disable"
	}
	profile := preferences.ConnectionProfile{
		ID: "mysql-integration", Name: "integration", Driver: preferences.DriverMySQL,
		Host: host, Port: port, Database: database, Username: username,
		CredentialRef: credentialRef, TLSMode: tlsMode,
		TLSCAPath: strings.TrimSpace(os.Getenv("DBL_TEST_MYSQL_CA")),
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	active, err := (Connector{Profile: profile, Vault: vault}).Connect(ctx)
	if err != nil {
		t.Fatalf("connect to MySQL integration server: %v", err)
	}
	store := active.(*Store)
	t.Cleanup(func() { _ = store.Close() })
	namespaces, err := store.Namespaces(ctx)
	if err != nil || !slices.Contains(namespaces, database) {
		t.Fatalf("MySQL database choices=%v err=%v; missing %q", namespaces, err, database)
	}
	current, err := store.CurrentNamespace(ctx)
	if err != nil || current != database {
		t.Fatalf("MySQL current database=%q err=%v, want %q", current, err, database)
	}

	table := fmt.Sprintf("dbl_it_%x", time.Now().UnixNano())
	quotedTable := "`" + table + "`"
	if _, err := store.db.ExecContext(ctx, `CREATE TABLE `+quotedTable+` (
		id BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
		label VARCHAR(128) NOT NULL,
		payload JSON NOT NULL
	)`); err != nil {
		t.Fatalf("create integration table: %v", err)
	}
	t.Cleanup(func() { _, _ = store.db.ExecContext(context.Background(), `DROP TABLE IF EXISTS `+quotedTable) })
	if _, err := store.db.ExecContext(ctx, `INSERT INTO `+quotedTable+` (label, payload) VALUES ('first', '{"answer":42}'), ('second', '{"answer":43}')`); err != nil {
		t.Fatalf("insert integration rows: %v", err)
	}

	var foundSchema, foundTable bool
	afterSchema, afterType, afterName := "", "", ""
	for pages := 0; pages < 5000; pages++ {
		page, err := store.Objects(ctx, afterSchema, afterType, afterName, 1)
		if err != nil {
			t.Fatalf("list MySQL objects: %v", err)
		}
		for _, object := range page.Objects {
			if object.Type == "schema" && object.Name == database {
				foundSchema = true
			}
			if object.Type == "table" && object.Schema == database && object.Name == table {
				foundTable = true
			}
		}
		if !page.HasMore {
			break
		}
		afterSchema, afterType, afterName = page.NextSchema, page.NextType, page.NextName
	}
	if !foundSchema || !foundTable {
		t.Fatalf("schema-aware object listing found schema=%v table=%v", foundSchema, foundTable)
	}

	columns, err := store.Columns(ctx, database, table)
	if err != nil {
		t.Fatalf("list MySQL columns: %v", err)
	}
	if len(columns) != 3 || columns[0].Name != "id" || !columns[0].PrimaryKey || columns[0].Hidden == 0 {
		t.Fatalf("unexpected MySQL columns: %#v", columns)
	}

	cursor, err := store.StartQuery(ctx, `SELECT label FROM `+quotedTable+` ORDER BY id`)
	if err != nil {
		t.Fatalf("start MySQL query: %v", err)
	}
	rows, done, err := cursor.NextBatch(10)
	if err != nil || !done || len(rows) != 2 || !bytes.Equal(rows[0][0].([]byte), []byte("first")) {
		_ = cursor.Close()
		t.Fatalf("MySQL query rows=%v done=%v err=%v", rows, done, err)
	}

	readOnlyProfile := profile
	readOnlyProfile.ReadOnly = true
	readOnlyActive, err := (Connector{Profile: readOnlyProfile, Vault: vault}).Connect(ctx)
	if err != nil {
		t.Fatalf("connect read-only MySQL profile: %v", err)
	}
	readOnly := readOnlyActive.(*Store)
	t.Cleanup(func() { _ = readOnly.Close() })
	readOnly.db.SetMaxOpenConns(1)
	if _, err := readOnly.StartQuery(ctx, `INSERT INTO `+quotedTable+` (label, payload) VALUES ('forbidden', '{}')`); err == nil {
		t.Fatal("read-only profile accepted an INSERT")
	}
	settingCursor, err := readOnly.StartQuery(ctx, `SET SESSION TRANSACTION READ WRITE`)
	if err != nil {
		t.Fatalf("set later MySQL transaction default for read-only protection check: %v", err)
	}
	if _, done, err := settingCursor.NextBatch(1); err != nil || !done {
		_ = settingCursor.Close()
		t.Fatalf("finish MySQL session transaction setting: done=%v err=%v", done, err)
	}
	attackCursor, attackErr := readOnly.StartQuery(ctx, `INSERT INTO `+quotedTable+` (label, payload) VALUES ('forbidden-after-session-change', '{}')`)
	if attackErr == nil {
		_, _, attackErr = attackCursor.NextBatch(1)
		_ = attackCursor.Close()
	}
	if attackErr == nil {
		t.Fatal("read-only profile accepted an INSERT after changing the session transaction default")
	}
	var rowCount int
	if err := store.db.QueryRowContext(ctx, `SELECT count(*) FROM `+quotedTable).Scan(&rowCount); err != nil {
		t.Fatalf("verify read-only profile did not modify rows: %v", err)
	}
	if rowCount != 2 {
		t.Fatalf("read-only profile modified data: row count = %d, want 2", rowCount)
	}

	var exported bytes.Buffer
	count, err := store.Export(ctx, `SELECT payload FROM `+quotedTable+` ORDER BY id LIMIT 1`, sqlstream.JSON, &exported)
	if err != nil || count != 1 {
		t.Fatalf("export count=%d err=%v", count, err)
	}
	if !strings.Contains(exported.String(), `{"answer":42}`) {
		t.Fatalf("MySQL JSON was not exported as structured JSON: %s", exported.String())
	}

	queryCtx, stopQuery := context.WithCancel(ctx)
	result := make(chan error, 1)
	go func() {
		slowCursor, err := store.StartQuery(queryCtx, `SELECT SLEEP(10)`)
		if err == nil {
			_, _, err = slowCursor.NextBatch(1)
			_ = slowCursor.Close()
		}
		result <- err
	}()
	time.Sleep(100 * time.Millisecond)
	stopQuery()
	select {
	case err := <-result:
		if err == nil || (connection.ClassifyCommonError(err) != connection.ErrorCanceled && store.ClassifyError(err) != connection.ErrorCanceled) {
			t.Fatalf("canceled query error = %v, want canceled", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("MySQL query did not stop after context cancellation")
	}
	if err := store.HealthCheck(ctx); err != nil {
		t.Fatalf("connection unusable after query cancellation: %v", err)
	}
}
