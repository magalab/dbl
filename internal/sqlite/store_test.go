package sqlite

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/magalab/dbl/internal/connection"
	"github.com/magalab/dbl/internal/sqlstream"
	modernsqlite "modernc.org/sqlite"
)

func TestOpenCreatesOnlyWhenRequested(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "new.db")

	if _, err := Open(ctx, path, ReadWriteExisting); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("opening missing file returned %v, want os.ErrNotExist", err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("read-write open unexpectedly created the file: stat error = %v", err)
	}

	store, err := Open(ctx, path, CreateNew)
	if err != nil {
		t.Fatalf("create SQLite file: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close new SQLite file: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("created SQLite file is missing: %v", err)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm()&0o077 != 0 {
			t.Fatalf("new database permissions are %04o, want no group/other access", info.Mode().Perm())
		}
	}
	if _, err := Open(ctx, path, CreateNew); err == nil {
		t.Fatal("creating over an existing file succeeded")
	}
}

func TestConnectionCapabilityHealthAndErrorClassification(t *testing.T) {
	ctx := context.Background()
	adapter, err := (Connector{Path: filepath.Join(t.TempDir(), "health.db"), Access: CreateNew}).Connect(ctx)
	if err != nil {
		t.Fatal(err)
	}
	store, ok := adapter.(*Store)
	if !ok {
		t.Fatalf("connector returned %T, want *Store", adapter)
	}
	if store.Driver() != "sqlite" {
		t.Fatalf("driver = %q, want sqlite", store.Driver())
	}
	if err := store.HealthCheck(ctx); err != nil {
		t.Fatalf("healthy connection returned %v", err)
	}
	namespaces, err := store.Namespaces(ctx)
	if err != nil || len(namespaces) != 1 || namespaces[0] != "main" {
		t.Fatalf("SQLite namespaces=%v err=%v, want [main]", namespaces, err)
	}
	current, err := store.CurrentNamespace(ctx)
	if err != nil || current != "main" {
		t.Fatalf("SQLite current namespace=%q err=%v, want main", current, err)
	}
	if got := store.ClassifyError(context.DeadlineExceeded); got != connection.ErrorTimeout {
		t.Fatalf("deadline classified as %v, want timeout", got)
	}
	if got := store.ClassifyError(errors.New("database is locked")); got != connection.ErrorBusy {
		t.Fatalf("locked database classified as %v, want busy", got)
	}
	if got := store.ClassifyError(errors.New("no such table: missing")); got != connection.ErrorQuery {
		t.Fatalf("missing table classified as %v, want query error", got)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	if err := store.HealthCheck(ctx); err == nil {
		t.Fatal("closed connection passed health check")
	}
}

func TestReadOnlyStoreRejectsWrites(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "readonly.db")
	store, err := Open(ctx, path, CreateNew)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.ExecContext(ctx, "CREATE TABLE records (value TEXT)"); err != nil {
		t.Fatalf("create test table: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	readOnly, err := Open(ctx, path, ReadOnly)
	if err != nil {
		t.Fatal(err)
	}
	defer readOnly.Close()
	if _, err := readOnly.db.ExecContext(ctx, "INSERT INTO records (value) VALUES ('blocked')"); err == nil {
		t.Fatal("write through read-only connection succeeded")
	}
	attachedPath := filepath.Join(t.TempDir(), "attached.db")
	attach := "ATTACH DATABASE '" + strings.ReplaceAll(attachedPath, "'", "''") + "' AS external"
	if _, err := readOnly.db.ExecContext(ctx, attach); err == nil {
		t.Fatal("ATTACH succeeded through a read-only connection")
	}
	if _, err := os.Stat(attachedPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("read-only ATTACH created a database: stat error = %v", err)
	}
	if _, err := readOnly.db.ExecContext(ctx, "PRAGMA query_only = OFF"); err == nil {
		t.Fatal("changing query_only succeeded through a read-only connection")
	}
	columns, err := readOnly.Columns(ctx, "", "records")
	if err != nil || len(columns) != 1 || columns[0].Name != "value" {
		t.Fatalf("read-only schema inspection = %#v, err=%v", columns, err)
	}
	var count int
	if err := readOnly.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM records").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("read-only database contains %d rows after rejected write", count)
	}
}

func TestReadOnlyAuthorizationPolicy(t *testing.T) {
	authorize := readOnlyAuthorizer()
	tests := []struct {
		name   string
		action modernsqlite.AuthorizerActionCode
		arg1   string
		arg2   string
		want   modernsqlite.AuthorizerReturnCode
	}{
		{name: "select", action: modernsqlite.AuthSelect, want: modernsqlite.AuthorizerOK},
		{name: "table metadata pragma", action: modernsqlite.AuthPragma, arg1: "table_xinfo", arg2: "records", want: modernsqlite.AuthorizerOK},
		{name: "read-only pragma value", action: modernsqlite.AuthPragma, arg1: "query_only", want: modernsqlite.AuthorizerOK},
		{name: "pragma mutation", action: modernsqlite.AuthPragma, arg1: "query_only", arg2: "OFF", want: modernsqlite.AuthorizerDeny},
		{name: "unknown pragma", action: modernsqlite.AuthPragma, arg1: "wal_checkpoint", want: modernsqlite.AuthorizerDeny},
		{name: "ordinary function", action: modernsqlite.AuthFunction, arg2: "length", want: modernsqlite.AuthorizerOK},
		{name: "extension loading", action: modernsqlite.AuthFunction, arg2: "load_extension", want: modernsqlite.AuthorizerDeny},
		{name: "file read function", action: modernsqlite.AuthFunction, arg2: "readfile", want: modernsqlite.AuthorizerDeny},
		{name: "file write function", action: modernsqlite.AuthFunction, arg2: "writefile", want: modernsqlite.AuthorizerDeny},
		{name: "attach", action: modernsqlite.AuthAttach, want: modernsqlite.AuthorizerDeny},
		{name: "insert", action: modernsqlite.AuthInsert, want: modernsqlite.AuthorizerDeny},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := authorize(test.action, test.arg1, test.arg2, "main", ""); got != test.want {
				t.Fatalf("authorization result = %v, want %v", got, test.want)
			}
		})
	}
}

func TestWALAllowsReadOnlySnapshotDuringWriteTransaction(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "wal.db")
	store, err := Open(ctx, path, CreateNew)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	var mode string
	if err := store.db.QueryRowContext(ctx, "PRAGMA journal_mode=WAL").Scan(&mode); err != nil {
		t.Fatalf("enable WAL: %v", err)
	}
	if !strings.EqualFold(mode, "wal") {
		t.Fatalf("journal mode = %q, want WAL", mode)
	}
	if _, err := store.db.ExecContext(ctx, "CREATE TABLE records (value TEXT)"); err != nil {
		t.Fatal(err)
	}
	transaction, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer transaction.Rollback()
	if _, err := transaction.ExecContext(ctx, "INSERT INTO records VALUES ('uncommitted')"); err != nil {
		t.Fatal(err)
	}
	readOnly, err := Open(ctx, path, ReadOnly)
	if err != nil {
		t.Fatalf("open WAL database read-only while writer is active: %v", err)
	}
	defer readOnly.Close()
	var count int
	if err := readOnly.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM records").Scan(&count); err != nil {
		t.Fatalf("read snapshot during write transaction: %v", err)
	}
	if count != 0 {
		t.Fatalf("read-only snapshot saw %d rows, want committed snapshot with 0 rows", count)
	}
}

func TestCanceledQueryLeavesStoreUsable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cancel.db")
	store, err := Open(context.Background(), path, CreateNew)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cursor, err := store.StartQuery(ctx, `WITH RECURSIVE numbers(value) AS (
		VALUES(1) UNION ALL SELECT value + 1 FROM numbers WHERE value < 1000000000
	) SELECT value FROM numbers`)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	firstBatch, done, err := cursor.NextBatch(128)
	if err != nil || done || len(firstBatch) != 128 {
		cancel()
		_ = cursor.Close()
		t.Fatalf("first recursive batch = %d rows, done=%v, err=%v; want 128 rows and an open cursor", len(firstBatch), done, err)
	}
	cancel()
	_, done, err = cursor.NextBatch(128)
	if !done {
		_ = cursor.Close()
		t.Fatalf("cursor remained open after cancellation: err=%v", err)
	}
	if err := cursor.Close(); err != nil {
		t.Fatalf("close canceled cursor: %v", err)
	}

	check, err := store.StartQuery(context.Background(), "SELECT 42")
	if err != nil {
		t.Fatalf("start query after cancellation: %v", err)
	}
	rows, done, err := check.NextBatch(2)
	if err != nil || !done || len(rows) != 1 || rows[0][0] != int64(42) {
		_ = check.Close()
		t.Fatalf("query after cancellation = %#v, done=%v, err=%v", rows, done, err)
	}
	if err := check.Close(); err != nil {
		t.Fatalf("close follow-up cursor: %v", err)
	}
}

func TestObjectsColumnsAndStreamingExport(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "metadata.db")
	store, err := Open(ctx, path, CreateNew)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	for _, statement := range []string{
		`CREATE TABLE "odd""table" (id INTEGER PRIMARY KEY, payload BLOB NOT NULL, payload_size INTEGER GENERATED ALWAYS AS (length(payload)) STORED)`,
		`CREATE INDEX records_payload_idx ON "odd""table" (payload)`,
		`CREATE TABLE sqliteX_records (id INTEGER)`,
		`INSERT INTO "odd""table" VALUES (1, X'00FF')`,
	} {
		if _, err := store.db.ExecContext(ctx, statement); err != nil {
			t.Fatalf("execute %q: %v", statement, err)
		}
	}

	page, err := store.Objects(ctx, "", "", "", 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Objects) != 1 || page.Objects[0].Type != "index" || !page.HasMore {
		t.Fatalf("first object page = %#v, want one index and another page", page)
	}
	page, err = store.Objects(ctx, page.NextSchema, page.NextType, page.NextName, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Objects) != 1 || page.Objects[0].Name != `odd"table` || page.Objects[0].Type != "table" || !page.HasMore {
		t.Fatalf("second object page = %#v, want the quoted table and another page", page)
	}
	page, err = store.Objects(ctx, page.NextSchema, page.NextType, page.NextName, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Objects) != 1 || page.Objects[0].Name != "sqliteX_records" || page.HasMore {
		t.Fatalf("third object page = %#v, want the sqliteX user table", page)
	}
	columns, err := store.Columns(ctx, "", `odd"table`)
	if err != nil {
		t.Fatal(err)
	}
	if len(columns) != 3 || columns[0].Name != "id" || !columns[0].PrimaryKey || columns[1].Name != "payload" || columns[2].Name != "payload_size" || columns[2].Hidden != 3 {
		t.Fatalf("columns = %#v, unexpected table metadata", columns)
	}

	var output bytes.Buffer
	count, err := store.Export(ctx, `SELECT id, payload FROM "odd""table"`, sqlstream.JSON, &output)
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("exported %d rows, want 1", count)
	}
	var document struct {
		Columns []string `json:"columns"`
		Rows    [][]any  `json:"rows"`
	}
	if err := json.Unmarshal(output.Bytes(), &document); err != nil {
		t.Fatalf("decode exported JSON %q: %v", output.String(), err)
	}
	if len(document.Columns) != 2 || len(document.Rows) != 1 || document.Rows[0][1] != "AP8=" {
		t.Fatalf("unexpected exported document: %+v", document)
	}
}

func TestExportRejectsDatabaseAndPragmaChanges(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "export-readonly.db")
	store, err := Open(ctx, path, CreateNew)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, err := store.db.ExecContext(ctx, "CREATE TABLE records (value INTEGER)"); err != nil {
		t.Fatal(err)
	}

	attachedPath := filepath.Join(t.TempDir(), "must-not-be-created.db")
	for _, statement := range []string{
		"ATTACH DATABASE '" + strings.ReplaceAll(attachedPath, "'", "''") + "' AS external",
		"PRAGMA query_only = OFF",
	} {
		if _, err := store.Export(ctx, statement, sqlstream.JSON, &bytes.Buffer{}); err == nil {
			t.Errorf("export accepted prohibited statement %q", statement)
		}
	}
	if _, err := os.Stat(attachedPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("prohibited ATTACH created an external database: stat error = %v", err)
	}

	var output bytes.Buffer
	if _, err := store.Export(ctx, "SELECT value FROM records", sqlstream.JSON, &output); err != nil {
		t.Fatalf("read-only SELECT export after rejected statements: %v", err)
	}
}

func BenchmarkSQLiteScan10K(b *testing.B) {
	b.StopTimer()
	ctx := context.Background()
	path := filepath.Join(b.TempDir(), "scan.db")
	store, err := Open(ctx, path, CreateNew)
	if err != nil {
		b.Fatal(err)
	}
	defer store.Close()
	if _, err := store.db.ExecContext(ctx, "CREATE TABLE scan_data (id INTEGER PRIMARY KEY, payload TEXT NOT NULL)"); err != nil {
		b.Fatal(err)
	}
	transaction, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		b.Fatal(err)
	}
	insert, err := transaction.PrepareContext(ctx, "INSERT INTO scan_data (payload) VALUES (?)")
	if err != nil {
		b.Fatal(err)
	}
	payload := strings.Repeat("x", 128)
	for i := 0; i < 10_000; i++ {
		if _, err := insert.ExecContext(ctx, payload); err != nil {
			b.Fatal(err)
		}
	}
	if err := insert.Close(); err != nil {
		b.Fatal(err)
	}
	if err := transaction.Commit(); err != nil {
		b.Fatal(err)
	}
	for _, batchSize := range []int{128, 512, 1024} {
		b.Run("batch_"+strconv.Itoa(batchSize), func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(10_000 * int64(len(payload)))
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				cursor, err := store.StartQuery(ctx, "SELECT id, payload FROM scan_data ORDER BY id")
				if err != nil {
					b.Fatal(err)
				}
				var count int
				for {
					rows, done, err := cursor.NextBatch(batchSize)
					if err != nil {
						_ = cursor.Close()
						b.Fatal(err)
					}
					count += len(rows)
					if done {
						break
					}
				}
				if err := cursor.Close(); err != nil {
					b.Fatal(err)
				}
				if count != 10_000 {
					b.Fatalf("scanned %d rows, want 10000", count)
				}
			}
		})
	}
}
