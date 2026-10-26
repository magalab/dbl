package mongodb

import (
	"context"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/magalab/dbl/internal/connection"
	"github.com/magalab/dbl/internal/credentials"
	"github.com/magalab/dbl/internal/preferences"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
)

type testCredentialBackend map[string][]byte

func (b testCredentialBackend) Set(account string, secret []byte) error {
	b[account] = append([]byte(nil), secret...)
	return nil
}
func (b testCredentialBackend) Get(account string) ([]byte, error) {
	value, ok := b[account]
	if !ok {
		return nil, credentials.ErrNotFound
	}
	return append([]byte(nil), value...), nil
}
func (b testCredentialBackend) Delete(account string) error {
	if _, ok := b[account]; !ok {
		return credentials.ErrNotFound
	}
	delete(b, account)
	return nil
}

func TestMongoDBIntegration(t *testing.T) {
	address := strings.TrimSpace(os.Getenv("DBL_TEST_MONGODB_ADDR"))
	if address == "" {
		t.Skip("set DBL_TEST_MONGODB_ADDR to run the optional MongoDB integration test")
	}
	host, portText, err := net.SplitHostPort(address)
	if err != nil {
		t.Fatalf("parse MongoDB address: %v", err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatalf("parse MongoDB port: %v", err)
	}
	database := fmt.Sprintf("dbl_it_%d", time.Now().UnixNano())
	profile := preferences.ConnectionProfile{Driver: preferences.DriverMongoDB, Host: host, Port: port, Database: database, Username: os.Getenv("DBL_TEST_MONGODB_USERNAME"), AuthSource: os.Getenv("DBL_TEST_MONGODB_AUTH_SOURCE"), TLSMode: os.Getenv("DBL_TEST_MONGODB_TLS_MODE"), TLSCAPath: strings.TrimSpace(os.Getenv("DBL_TEST_MONGODB_CA"))}
	if profile.TLSMode == "" {
		profile.TLSMode = "disable"
	}
	backend := testCredentialBackend{}
	vault := credentials.New(backend)
	if password := os.Getenv("DBL_TEST_MONGODB_PASSWORD"); password != "" {
		ref, err := vault.Store([]byte(password))
		if err != nil {
			t.Fatal(err)
		}
		profile.CredentialRef = string(ref)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	adapter, err := (Connector{Profile: profile, Vault: vault}).Connect(ctx)
	if err != nil {
		t.Fatal(err)
	}
	store := adapter.(*Store)
	t.Cleanup(func() {
		cleanupCtx, stop := context.WithTimeout(context.Background(), 15*time.Second)
		defer stop()
		if err := store.client.Database(database).Drop(cleanupCtx); err != nil {
			t.Errorf("drop MongoDB integration database: %v", err)
		}
		if err := adapter.Close(); err != nil {
			t.Errorf("close MongoDB integration connection: %v", err)
		}
	})
	if err := store.InsertDocument(ctx, database, "documents", `{"kind":"book","title":"Moby Dick"}`); err != nil {
		t.Fatal(err)
	}
	collection := store.client.Database(database).Collection("documents")
	if _, err := collection.Indexes().CreateOne(ctx, mongo.IndexModel{Keys: bson.D{{Key: "kind", Value: 1}}}); err != nil {
		t.Fatalf("create integration index: %v", err)
	}
	indexes, err := store.CollectionIndexes(ctx, database, "documents")
	if err != nil {
		t.Fatalf("list integration indexes: %v", err)
	}
	foundIndex := false
	for _, index := range indexes {
		if index.Name == "kind_1" && strings.Contains(index.KeysJSON, `"kind":1`) {
			foundIndex = true
		}
	}
	if !foundIndex {
		t.Fatalf("MongoDB index metadata = %#v, want kind_1", indexes)
	}
	page, err := store.FindDocuments(ctx, database, "documents", `{"kind":"book"}`, `{"title":1}`, 0, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Documents) != 1 || page.Documents[0].IDJSON == "" {
		t.Fatalf("MongoDB find page=%#v", page)
	}
	doc, err := store.GetDocument(ctx, database, "documents", page.Documents[0].IDJSON)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(doc.JSON, "Moby Dick") {
		t.Fatalf("MongoDB document=%s", doc.JSON)
	}
	pipeline := `[{"$match":{"kind":"book"}},{"$project":{"title":1}}]`
	aggregated, err := store.AggregateDocuments(ctx, database, "documents", pipeline, 0, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(aggregated.Documents) != 1 {
		t.Fatalf("MongoDB aggregate count=%d", len(aggregated.Documents))
	}
	if _, err := store.AggregateDocuments(ctx, database, "documents", `[{"$out":"blocked"}]`, 0, 20); err == nil {
		t.Fatal("MongoDB aggregation accepted a write stage")
	}
	replacement := fmt.Sprintf(`{"_id":%s,"kind":"book","title":"Moby Dick (edited)"}`, page.Documents[0].IDJSON)
	if err := store.ReplaceDocument(ctx, database, "documents", page.Documents[0].IDJSON, replacement); err != nil {
		t.Fatal(err)
	}
	updated, err := store.GetDocument(ctx, database, "documents", page.Documents[0].IDJSON)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(updated.JSON, "edited") {
		t.Fatalf("MongoDB replacement=%s", updated.JSON)
	}
	if err := store.ReplaceDocument(ctx, database, "documents", page.Documents[0].IDJSON, `{"_id":"different","kind":"book"}`); err == nil {
		t.Fatal("MongoDB replacement allowed changing _id")
	}
	store.readOnly = true
	if err := store.InsertDocument(ctx, database, "documents", `{"title":"blocked"}`); err == nil {
		t.Fatal("read-only MongoDB insert succeeded")
	}
	if collections, err := store.Collections(ctx, database); err != nil || len(collections) == 0 {
		t.Fatalf("MongoDB collections=%v err=%v", collections, err)
	}
	var _ connection.Documents = store
}

func TestMongoDBTLSHostnameMismatchIntegration(t *testing.T) {
	address := strings.TrimSpace(os.Getenv("DBL_TEST_MONGODB_TLS_MISMATCH_ADDR"))
	if address == "" {
		t.Skip("set DBL_TEST_MONGODB_TLS_MISMATCH_ADDR to run the optional TLS hostname verification test")
	}
	host, portText, err := net.SplitHostPort(address)
	if err != nil {
		t.Fatalf("parse MongoDB mismatch address: %v", err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatalf("parse MongoDB port: %v", err)
	}
	profile := preferences.ConnectionProfile{
		Driver:    preferences.DriverMongoDB,
		Host:      host,
		Port:      port,
		TLSMode:   "verify-full",
		TLSCAPath: strings.TrimSpace(os.Getenv("DBL_TEST_MONGODB_CA")),
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if adapter, err := (Connector{Profile: profile}).Connect(ctx); err == nil {
		_ = adapter.Close()
		t.Fatal("MongoDB verify-full accepted a certificate with a hostname mismatch")
	} else if !strings.Contains(strings.ToLower(err.Error()), "not valid for any names") {
		t.Fatalf("MongoDB hostname mismatch failed for an unexpected reason: %v", err)
	}
}
