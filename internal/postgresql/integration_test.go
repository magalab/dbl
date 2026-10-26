package postgresql

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

func TestPostgreSQLIntegration(t *testing.T) {
	host := strings.TrimSpace(os.Getenv("DBL_TEST_POSTGRES_HOST"))
	if host == "" {
		t.Skip("set DBL_TEST_POSTGRES_HOST to run PostgreSQL integration checks")
	}
	port, err := strconv.Atoi(os.Getenv("DBL_TEST_POSTGRES_PORT"))
	if err != nil || port < 1 || port > 65535 {
		t.Fatal("DBL_TEST_POSTGRES_PORT must be a valid TCP port")
	}
	database := os.Getenv("DBL_TEST_POSTGRES_DATABASE")
	username := os.Getenv("DBL_TEST_POSTGRES_USERNAME")
	password := []byte(os.Getenv("DBL_TEST_POSTGRES_PASSWORD"))
	if database == "" || username == "" {
		t.Fatal("DBL_TEST_POSTGRES_DATABASE and DBL_TEST_POSTGRES_USERNAME are required")
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
	tlsMode := strings.TrimSpace(os.Getenv("DBL_TEST_POSTGRES_TLS_MODE"))
	if tlsMode == "" {
		tlsMode = "disable"
	}
	profile := preferences.ConnectionProfile{
		ID: "postgres-integration", Name: "integration", Driver: preferences.DriverPostgreSQL,
		Host: host, Port: port, Database: database, Username: username,
		CredentialRef: credentialRef, TLSMode: tlsMode,
		TLSCAPath: strings.TrimSpace(os.Getenv("DBL_TEST_POSTGRES_CA")),
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	active, err := (Connector{Profile: profile, Vault: vault}).Connect(ctx)
	if err != nil {
		t.Fatalf("connect to PostgreSQL integration server: %v", err)
	}
	store := active.(*Store)
	t.Cleanup(func() { _ = store.Close() })

	schema := fmt.Sprintf("dbl_it_%x", time.Now().UnixNano())
	if _, err := store.db.ExecContext(ctx, `CREATE SCHEMA "`+schema+`"`); err != nil {
		t.Fatalf("create integration schema: %v", err)
	}
	t.Cleanup(func() {
		_, _ = store.db.ExecContext(context.Background(), `DROP SCHEMA IF EXISTS "`+schema+`" CASCADE`)
	})
	createTable := `CREATE TABLE "` + schema + `"."records" (
		id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
		label TEXT NOT NULL,
		payload JSONB NOT NULL DEFAULT '{"answer":42}'::jsonb
	)`
	if _, err := store.db.ExecContext(ctx, createTable); err != nil {
		t.Fatalf("create integration table: %v", err)
	}
	if _, err := store.db.ExecContext(ctx, `INSERT INTO "`+schema+`"."records" (label) VALUES ('first'), ('second')`); err != nil {
		t.Fatalf("insert integration rows: %v", err)
	}

	var foundSchema, foundTable bool
	afterSchema, afterType, afterName := "", "", ""
	for pages := 0; pages < 5000; pages++ {
		page, err := store.Objects(ctx, afterSchema, afterType, afterName, 1)
		if err != nil {
			t.Fatalf("list PostgreSQL objects: %v", err)
		}
		for _, object := range page.Objects {
			if object.Type == "schema" && object.Name == schema {
				foundSchema = true
			}
			if object.Type == "table" && object.Schema == schema && object.Name == "records" {
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

	namespaces, err := store.Namespaces(ctx)
	if err != nil || !slices.Contains(namespaces, schema) {
		t.Fatalf("PostgreSQL schema choices=%v err=%v; missing %q", namespaces, err, schema)
	}
	current, err := store.CurrentNamespace(ctx)
	if err != nil || current == "" {
		t.Fatalf("PostgreSQL current schema=%q err=%v", current, err)
	}

	selectedActive, err := (Connector{Profile: profile, Vault: vault, SearchPath: schema}).Connect(ctx)
	if err != nil {
		t.Fatalf("connect with selected PostgreSQL schema: %v", err)
	}
	selected := selectedActive.(*Store)
	t.Cleanup(func() { _ = selected.Close() })
	selectedSchema, err := selected.CurrentNamespace(ctx)
	if err != nil || selectedSchema != schema {
		t.Fatalf("selected PostgreSQL schema=%q err=%v, want %q", selectedSchema, err, schema)
	}
	selectedCursor, err := selected.StartQuery(ctx, "SELECT count(*) FROM records")
	if err != nil {
		t.Fatalf("query unqualified table in selected PostgreSQL schema: %v", err)
	}
	selectedRows, selectedDone, err := selectedCursor.NextBatch(10)
	if err != nil || !selectedDone || len(selectedRows) != 1 || selectedRows[0][0] != int64(2) {
		_ = selectedCursor.Close()
		t.Fatalf("selected PostgreSQL schema query rows=%v done=%v err=%v", selectedRows, selectedDone, err)
	}

	columns, err := store.Columns(ctx, schema, "records")
	if err != nil {
		t.Fatalf("list PostgreSQL columns: %v", err)
	}
	if len(columns) != 3 || columns[0].Name != "id" || !columns[0].PrimaryKey || columns[0].Hidden == 0 {
		t.Fatalf("unexpected PostgreSQL columns: %#v", columns)
	}

	cursor, err := store.StartQuery(ctx, `SELECT label FROM "`+schema+`"."records" ORDER BY id`)
	if err != nil {
		t.Fatalf("start PostgreSQL query: %v", err)
	}
	rows, done, err := cursor.NextBatch(10)
	if err != nil || !done || len(rows) != 2 || rows[0][0] != "first" {
		_ = cursor.Close()
		t.Fatalf("PostgreSQL query rows=%v done=%v err=%v", rows, done, err)
	}

	readOnlyProfile := profile
	readOnlyProfile.ReadOnly = true
	readOnlyActive, err := (Connector{Profile: readOnlyProfile, Vault: vault}).Connect(ctx)
	if err != nil {
		t.Fatalf("connect read-only PostgreSQL profile: %v", err)
	}
	readOnly := readOnlyActive.(*Store)
	t.Cleanup(func() { _ = readOnly.Close() })
	if _, err := readOnly.StartQuery(ctx, `INSERT INTO "`+schema+`"."records" (label) VALUES ('forbidden')`); err == nil {
		t.Fatal("read-only profile accepted an INSERT")
	}
	if cursor, err := readOnly.StartQuery(ctx, `SET default_transaction_read_only=off`); err != nil {
		t.Fatalf("set per-session default for protection check: %v", err)
	} else {
		if _, _, err := cursor.NextBatch(1); err != nil {
			_ = cursor.Close()
			t.Fatalf("read session-setting result: %v", err)
		}
	}
	if _, err := readOnly.StartQuery(ctx, `INSERT INTO "`+schema+`"."records" (label) VALUES ('forbidden')`); err == nil {
		t.Fatal("read-only profile accepted an INSERT after a session setting change")
	}
	attack := `SET TRANSACTION READ WRITE; INSERT INTO "` + schema + `"."records" (label) VALUES ('forbidden')`
	attackCursor, attackErr := readOnly.StartQuery(ctx, attack)
	if attackErr == nil {
		_, _, attackErr = attackCursor.NextBatch(1)
		_ = attackCursor.Close()
	}
	if attackErr == nil {
		t.Fatal("read-only profile accepted a mode switch and write in one SQL request")
	}
	var rowCount int
	if err := store.db.QueryRowContext(ctx, `SELECT count(*) FROM "`+schema+`"."records"`).Scan(&rowCount); err != nil {
		t.Fatalf("verify read-only profile did not modify rows: %v", err)
	}
	if rowCount != 2 {
		t.Fatalf("read-only profile modified data: row count = %d, want 2", rowCount)
	}

	var exported bytes.Buffer
	count, err := store.Export(ctx, `SELECT payload FROM "`+schema+`"."records" ORDER BY id LIMIT 1`, sqlstream.JSON, &exported)
	if err != nil || count != 1 {
		t.Fatalf("export count=%d err=%v", count, err)
	}
	if !strings.Contains(exported.String(), `{"answer":42}`) {
		t.Fatalf("PostgreSQL JSONB was not exported as structured JSON: %s", exported.String())
	}

	queryCtx, stopQuery := context.WithCancel(ctx)
	result := make(chan error, 1)
	go func() {
		slowCursor, err := store.StartQuery(queryCtx, `SELECT pg_sleep(10)`)
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
		t.Fatal("PostgreSQL query did not stop after context cancellation")
	}

	if err := store.HealthCheck(ctx); err != nil {
		t.Fatalf("connection unusable after query cancellation: %v", err)
	}
}
