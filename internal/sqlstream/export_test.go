package sqlstream

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"io"
	"reflect"
	"testing"
)

type sliceReader struct {
	batches [][][]any
	index   int
}

func (reader *sliceReader) NextBatch(int) ([][]any, bool, error) {
	if reader.index >= len(reader.batches) {
		return nil, true, nil
	}
	batch := reader.batches[reader.index]
	reader.index++
	return batch, reader.index == len(reader.batches), nil
}

func TestWriteCSVStreamsRowsAndEscapesFields(t *testing.T) {
	reader := &sliceReader{batches: [][][]any{{
		{"a,b", "line 1\nline 2", []byte{0x00, 0xff}, []byte("hello")},
		{nil, int64(42), []byte("ok"), []byte{0xff}},
	}}}
	var output bytes.Buffer
	count, err := WriteCSV(&output, []Column{
		{Name: "name"},
		{Name: "value"},
		{Name: "blob", DatabaseType: "BLOB"},
		{Name: "text", DatabaseType: "TEXT"},
	}, reader)
	if err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("exported %d rows, want 2", count)
	}
	records, err := csv.NewReader(&output).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	want := [][]string{
		{"name", "value", "blob", "text"},
		{"a,b", "line 1\nline 2", "base64:AP8=", "hello"},
		{"", "42", "base64:b2s=", "base64:/w=="},
	}
	if !reflect.DeepEqual(records, want) {
		t.Fatalf("CSV records = %#v, want %#v", records, want)
	}
}

func TestFormatClipboardRowQuotesSpreadsheetFields(t *testing.T) {
	values := []string{"plain", "with\ttab", "line 1\nline 2", `a"b`}
	formatted := FormatClipboardRow(values)
	reader := csv.NewReader(bytes.NewBufferString(formatted))
	reader.Comma = '\t'
	got, err := reader.Read()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, values) {
		t.Fatalf("clipboard row decoded as %#v, want %#v", got, values)
	}
}

func TestWriteJSONProducesValidStreamingDocument(t *testing.T) {
	reader := &sliceReader{batches: [][][]any{{{"hello", int64(7)}}, {{nil, []byte{0x1, 0x2}}}}}
	var output bytes.Buffer
	count, err := WriteJSON(&output, []Column{{Name: "text"}, {Name: "bytes"}}, reader)
	if err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("exported %d rows, want 2", count)
	}
	var got struct {
		Columns []string `json:"columns"`
		Rows    [][]any  `json:"rows"`
	}
	if err := json.Unmarshal(output.Bytes(), &got); err != nil {
		t.Fatalf("invalid JSON %q: %v", output.String(), err)
	}
	if !reflect.DeepEqual(got.Columns, []string{"text", "bytes"}) || len(got.Rows) != 2 {
		t.Fatalf("unexpected JSON export: %+v", got)
	}
	if got.Rows[1][1] != "AQI=" {
		t.Fatalf("binary JSON value = %v, want base64 AQI=", got.Rows[1][1])
	}
}

func TestWriteJSONUsesColumnTypesForByteValues(t *testing.T) {
	reader := &sliceReader{batches: [][][]any{{{
		[]byte("hello"),
		[]byte{0x00, 0xff},
		[]byte("unknown type"),
	}}}}
	var output bytes.Buffer
	_, err := WriteJSON(&output, []Column{
		{Name: "text", DatabaseType: "TEXT"},
		{Name: "blob", DatabaseType: "BLOB"},
		{Name: "unknown"},
	}, reader)
	if err != nil {
		t.Fatal(err)
	}
	var document struct {
		Rows [][]any `json:"rows"`
	}
	if err := json.Unmarshal(output.Bytes(), &document); err != nil {
		t.Fatal(err)
	}
	want := []any{"hello", "AP8=", "dW5rbm93biB0eXBl"}
	if !reflect.DeepEqual(document.Rows[0], want) {
		t.Fatalf("JSON row = %#v, want %#v", document.Rows[0], want)
	}
}

func TestWriteJSONKeepsPostgreSQLJSONBAsStructuredJSON(t *testing.T) {
	reader := &sliceReader{batches: [][][]any{{{[]byte(`{"answer":42}`)}}}}
	var output bytes.Buffer
	_, err := WriteJSON(&output, []Column{{Name: "payload", DatabaseType: "JSONB"}}, reader)
	if err != nil {
		t.Fatal(err)
	}
	var document struct {
		Rows [][]map[string]int `json:"rows"`
	}
	if err := json.Unmarshal(output.Bytes(), &document); err != nil {
		t.Fatalf("invalid JSON %q: %v", output.String(), err)
	}
	if len(document.Rows) != 1 || len(document.Rows[0]) != 1 || document.Rows[0][0]["answer"] != 42 {
		t.Fatalf("JSONB value = %#v, want structured object {answer:42}", document.Rows)
	}
}

func TestWriteExportReturnsReaderError(t *testing.T) {
	reader := errorReader{}
	_, err := WriteCSV(io.Discard, []Column{{Name: "x"}}, reader)
	if err != io.EOF {
		t.Fatalf("got error %v, want EOF", err)
	}
}

type errorReader struct{}

func (errorReader) NextBatch(int) ([][]any, bool, error) { return nil, false, io.EOF }
