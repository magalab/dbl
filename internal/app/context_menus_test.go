package app

import (
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/magalab/dbl/internal/connection"
	"github.com/magalab/dbl/internal/preferences"
)

func TestInstanceInfoOpensDetails(t *testing.T) {
	app := newTestWorkbench()
	app.language = "en"
	app.showInstanceInfo(preferences.ConnectionProfile{
		Name: "Example connection", Driver: preferences.DriverPostgreSQL,
		Host: "2001:db8::1", Port: 5432, Database: "sample", Username: "postgres",
		CredentialRef: "private-credential-reference",
	})
	tt := ui.NewTester(app.confirmationModal, 800, 620)
	for _, text := range []string{"Instance Info", "********", "Example connection", "PostgreSQL", "[2001:db8::1]:5432", "sample", "postgresql://postgres:********@[2001:db8::1]:5432/sample?sslmode=verify-full"} {
		if !tt.HasText(text) {
			t.Errorf("instance details missing %q", text)
		}
	}
	for _, name := range []string{"Connection Name", "Host / Endpoint", "Database", "DSN"} {
		if _, ok := tt.AccessByRole(ui.RoleButton, "Copy "+name); !ok {
			t.Errorf("missing copy control for %s", name)
		}
	}
	if _, ok := tt.AccessByRole(ui.RoleButton, "Show password"); !ok {
		t.Fatal("DSN row is missing its password visibility control")
	}
	if _, ok := tt.AccessByRole(ui.RoleButton, "Copy Database Type"); ok {
		t.Error("database type still has a separate copy control")
	}
	if tt.HasText("private-credential-reference") {
		t.Fatal("instance details exposed a credential reference")
	}
	if err := tt.Click("Close"); err != nil {
		t.Fatal(err)
	}
	if app.dialogOpen {
		t.Fatal("instance dialog remained open")
	}
}

func TestInstanceInfoActionOnDisconnectedProfile(t *testing.T) {
	app := newTestWorkbench()
	app.language = "en"
	app.connectionProfiles = []preferences.ConnectionProfile{{
		ID: "sample", Name: "Offline Postgres", Driver: preferences.DriverPostgreSQL,
		Host: "localhost", Port: 5432,
	}}
	tt := ui.NewTester(func(c *ui.Context) {
		ui.Column(c).Width(285).Children(func() { app.connectionTree(c) })
		app.confirmationModal(c)
	}, 800, 620)
	if _, ok := tt.Find("Instance Info"); ok {
		t.Fatal("row action unexpectedly visible at rest")
	}
	tt.Move(110, 14)
	if err := tt.Click("Instance Info"); err != nil {
		t.Fatal(err)
	}
	if !app.dialogOpen || !tt.HasText("localhost:5432") || app.busy || app.hasActiveConnection() {
		t.Fatal("instance info did not open offline or attempted a connection")
	}
	if _, ok := tt.AccessByRole(ui.RoleButton, "Close"); !ok {
		t.Fatal("dialog is missing its accessible close icon")
	}
}

func TestCopyFieldButtonShowsTemporarySuccess(t *testing.T) {
	app := newTestWorkbench()
	app.language = "en"
	var copied string
	write := func(value string) error {
		copied = value
		return nil
	}
	tt := ui.NewTester(func(c *ui.Context) {
		for _, label := range []string{"DSN", "Host"} {
			row := ui.Row(c)
			row.Children(func() { app.copyFieldButton(c, row, label, label+" value", write) })
		}
	}, 300, 150)
	if err := tt.Click("Copy DSN"); err != nil {
		t.Fatal(err)
	}
	if copied != "DSN value" {
		t.Fatalf("clipboard received %q", copied)
	}
	if _, ok := tt.AccessByRole(ui.RoleButton, "Copied DSN"); !ok {
		t.Fatal("copied field did not acknowledge success")
	}
	if _, ok := tt.AccessByRole(ui.RoleButton, "Copy Host"); !ok {
		t.Fatal("copy changed another field")
	}
	tt.Advance(1600 * time.Millisecond)
	if _, ok := tt.AccessByRole(ui.RoleButton, "Copy DSN"); !ok {
		t.Fatal("button did not return to the copy state")
	}
}

func TestSQLConnectionOverviewShowsConnectionDetails(t *testing.T) {
	app := newTestWorkbench()
	app.language = "en"
	app.openedLabel = "Example PostgreSQL · localhost/example"
	app.openedDriver = connection.DriverPostgreSQL
	app.openedReadOnly = true
	app.openedSQLProfile = preferences.ConnectionProfile{Name: "Example PostgreSQL", Host: "localhost", Port: 5432, Database: "example"}
	app.sqlNamespaces = []string{"public", "reporting"}
	app.sqlNamespace = "public"
	app.openedConnectionID = "test"
	app.liveConnections = map[string]liveConnection{"test": {sqlVersion: "16.4"}}
	app.objects = []connection.Object{{Schema: "public", Type: "table"}, {Schema: "public", Type: "view"}}

	tt := ui.NewTester(app.sqlConnectionOverview, 800, 620)
	for _, text := range []string{"Example PostgreSQL", "Connected", "localhost:5432", "example", "Read-only", "Schemas", "public", "reporting", "Current schema", "16.4", "Loaded: 1 tables · 1 views"} {
		if !tt.HasText(text) {
			t.Errorf("connection overview missing %q", text)
		}
	}
	if _, ok := tt.AccessByRole(ui.RoleHeading, "Example PostgreSQL"); !ok || tt.HasText("Example PostgreSQL · localhost/example") {
		t.Fatal("overview heading should only show the connection name")
	}
	if _, ok := tt.AccessByRole(ui.RoleButton, "New Query Workspace"); !ok {
		t.Fatal("connection overview missing the new query icon button")
	}
	for _, label := range []string{"Database version", "Host / Endpoint", "Database", "Access mode"} {
		if _, ok := tt.AccessByRole(ui.RoleButton, "Copy "+label); !ok {
			t.Errorf("missing copy button for %q", label)
		}
	}
}

func TestMongoOverviewShowsVisibleDatabasesAndOptionalMetadata(t *testing.T) {
	app := newTestWorkbench()
	app.language = "en"
	app.openedConnectionID = "mongodb:sample"
	app.mongoDatabases = []string{"sample", "warehouse"}
	app.liveConnections = map[string]liveConnection{app.openedConnectionID: {
		profile:       preferences.ConnectionProfile{Name: "Sample Mongo", Host: "localhost", Port: 27017},
		mongoOverview: connection.MongoOverview{Version: "8.0", Topology: "replica"},
	}}
	tt := ui.NewTester(app.mongoConnectionOverview, 800, 620)
	for _, text := range []string{"Sample Mongo", "localhost:27017", "8.0", "Replica set", "Visible databases", "warehouse"} {
		if !tt.HasText(text) {
			t.Errorf("MongoDB overview missing %q", text)
		}
	}
	for _, database := range []string{"sample", "warehouse"} {
		if _, ok := tt.AccessByRole(ui.RoleButton, database); !ok {
			t.Errorf("visible MongoDB database %q is not a button", database)
		}
	}
	app.mongoSelectedDB = "warehouse"
	app.mongoCollections = []string{"items", "orders"}
	tt.Frame()
	if !tt.HasText("warehouse · 2 visible collections") || app.mongoSelectedColl != "" {
		t.Fatal("selecting a database did not show its collection count without opening a collection")
	}
}
