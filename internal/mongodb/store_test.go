package mongodb

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.mongodb.org/mongo-driver/v2/bson"
)

func TestMongoTLSConfigCustomCAValidation(t *testing.T) {
	if _, err := tlsConfig("localhost", "require", "/does/not/matter.pem"); err == nil || !strings.Contains(err.Error(), "requires verify-full or verify-ca") {
		t.Fatalf("custom CA with require TLS mode error = %v", err)
	}
	if _, err := tlsConfig("localhost", "verify-full", filepath.Join(t.TempDir(), "missing.pem")); err == nil || !strings.Contains(err.Error(), "read MongoDB CA certificate") {
		t.Fatalf("missing custom CA error = %v", err)
	}
	invalidCA := filepath.Join(t.TempDir(), "invalid.pem")
	if err := os.WriteFile(invalidCA, []byte("not a certificate"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := tlsConfig("localhost", "verify-ca", invalidCA); err == nil || !strings.Contains(err.Error(), "contains no valid certificates") {
		t.Fatalf("invalid custom CA error = %v", err)
	}
}

func TestDecodeObjectAndDocumentIDFilters(t *testing.T) {
	document, err := decodeObject(`{"name":"Ada","nested":{"active":true}}`)
	if err != nil || len(document) != 2 {
		t.Fatalf("decoded Mongo document=%#v err=%v", document, err)
	}
	if _, err := decodeObject(`[1,2]`); err == nil {
		t.Fatal("array accepted where object required")
	}
	objectID := `{"$oid":"65a000000000000000000001"}`
	filter, err := idFilter(objectID)
	if err != nil {
		t.Fatal(err)
	}
	if len(filter) != 1 || filter[0].Key != "_id" {
		t.Fatalf("Mongo ID filter=%#v", filter)
	}
	textFilter, err := idFilter(`"invoice-7"`)
	if err != nil || textFilter[0].Value != "invoice-7" {
		t.Fatalf("string ID filter=%#v err=%v", textFilter, err)
	}
	if _, err := idFilter(`{"$oid":"bad"}`); err == nil {
		t.Fatal("malformed Extended JSON id was accepted")
	}
}

func TestMongoDocumentEncodingAndPreviewBounds(t *testing.T) {
	rawBytes, err := bson.Marshal(bson.D{{Key: "_id", Value: "item-1"}, {Key: "name", Value: "Ada"}})
	if err != nil {
		t.Fatal(err)
	}
	jsonBytes, err := bson.MarshalExtJSON(bson.Raw(rawBytes), true, false)
	if err != nil {
		t.Fatal(err)
	}
	document, err := documentFromJSON(bson.Raw(rawBytes), jsonBytes)
	if err != nil {
		t.Fatal(err)
	}
	if document.IDJSON != `"item-1"` || !strings.Contains(document.JSON, `"name"`) {
		t.Fatalf("encoded document=%#v", document)
	}
	largeRaw, err := bson.Marshal(bson.D{
		{Key: "_id", Value: "large-item"},
		{Key: "payload", Value: strings.Repeat("x", maxDocumentBytes+10)},
	})
	if err != nil {
		t.Fatal(err)
	}
	largeJSON, err := bson.MarshalExtJSON(bson.Raw(largeRaw), true, false)
	if err != nil {
		t.Fatal(err)
	}
	clipped, err := documentFromJSON(bson.Raw(largeRaw), largeJSON)
	if err != nil {
		t.Fatal(err)
	}
	if len(clipped.JSON) != maxDocumentBytes || !clipped.Truncated {
		t.Fatalf("large doc len=%d truncated=%v", len(clipped.JSON), clipped.Truncated)
	}
	if got := bounded(maxPageSize + 20); got != maxPageSize {
		t.Fatalf("page limit=%d", got)
	}
}

func TestMongoReadSafetyValidation(t *testing.T) {
	for _, text := range []string{`[{"$out":"backup"}]`, `[{"$facet":{"copy":[{"$merge":"archive"}]}}]`} {
		var pipeline bson.A
		if err := bson.UnmarshalExtJSON([]byte(text), true, &pipeline); err != nil {
			t.Fatal(err)
		}
		if err := validateReadOnlyPipeline(pipeline); err == nil {
			t.Fatalf("write pipeline was accepted: %s", text)
		}
	}
	var readPipeline bson.A
	if err := bson.UnmarshalExtJSON([]byte(`[{"$match":{"kind":"book"}}]`), true, &readPipeline); err != nil {
		t.Fatal(err)
	}
	if err := validateReadOnlyPipeline(readPipeline); err != nil {
		t.Fatalf("read-only pipeline rejected: %v", err)
	}
	projection, err := decodeObject(`{"name":1}`)
	if err != nil {
		t.Fatal(err)
	}
	if excludedID(projection) {
		t.Fatal("inclusion projection unexpectedly excludes _id")
	}
	projection, err = decodeObject(`{"_id":0,"name":1}`)
	if err != nil {
		t.Fatal(err)
	}
	if !excludedID(projection) {
		t.Fatal("_id exclusion was not detected")
	}
}
