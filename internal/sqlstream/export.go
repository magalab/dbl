package sqlstream

import (
	"encoding/base64"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
	"unicode/utf8"
)

type BatchReader interface {
	NextBatch(batchSize int) (rows [][]any, done bool, err error)
}

var ErrNoResultColumns = errors.New("query returned no result columns")

type ExportFormat uint8

const (
	CSV ExportFormat = iota
	JSON
)

func WriteCSV(output io.Writer, columns []Column, source BatchReader) (int64, error) {
	if len(columns) == 0 {
		return 0, ErrNoResultColumns
	}
	writer := csv.NewWriter(output)
	header := make([]string, len(columns))
	for i, column := range columns {
		header[i] = column.Name
	}
	if err := writer.Write(header); err != nil {
		return 0, err
	}

	var total int64
	for {
		rows, done, err := source.NextBatch(DefaultBatchSize)
		if err != nil {
			return total, err
		}
		for _, row := range rows {
			record := make([]string, len(columns))
			for i := range record {
				if i < len(row) {
					record[i] = exportText(row[i], columns[i].DatabaseType)
				}
			}
			if err := writer.Write(record); err != nil {
				return total, err
			}
			total++
		}
		writer.Flush()
		if err := writer.Error(); err != nil {
			return total, err
		}
		if done {
			return total, nil
		}
	}
}

// WriteJSON streams an object containing column names and a rows array. Text
// columns returned as byte slices are decoded as UTF-8; other byte slices use
// encoding/json's base64 representation to preserve binary values.
func WriteJSON(output io.Writer, columns []Column, source BatchReader) (int64, error) {
	if len(columns) == 0 {
		return 0, ErrNoResultColumns
	}
	encoder := json.NewEncoder(output)
	if _, err := io.WriteString(output, `{"columns":`); err != nil {
		return 0, err
	}
	names := make([]string, len(columns))
	for i, column := range columns {
		names[i] = column.Name
	}
	if err := encoder.Encode(names); err != nil {
		return 0, err
	}
	if _, err := io.WriteString(output, `,"rows":[`); err != nil {
		return 0, err
	}

	var total int64
	first := true
	for {
		rows, done, err := source.NextBatch(DefaultBatchSize)
		if err != nil {
			return total, err
		}
		for _, row := range rows {
			if !first {
				if _, err := io.WriteString(output, ","); err != nil {
					return total, err
				}
			}
			first = false
			encodedRow := make([]any, len(columns))
			for i := range encodedRow {
				if i < len(row) {
					encodedRow[i] = exportJSONValue(row[i], columns[i].DatabaseType)
				}
			}
			if err := encoder.Encode(encodedRow); err != nil {
				return total, err
			}
			total++
		}
		if done {
			if _, err := io.WriteString(output, "]}"); err != nil {
				return total, err
			}
			return total, nil
		}
	}
}

func exportJSONValue(value any, databaseType string) any {
	data, ok := value.([]byte)
	if !ok {
		return value
	}
	typ := strings.ToUpper(databaseType)
	if (typ == "JSON" || typ == "JSONB") && json.Valid(data) {
		return json.RawMessage(data)
	}
	textColumn := strings.Contains(typ, "TEXT") || strings.Contains(typ, "CHAR") || strings.Contains(typ, "CLOB")
	if textColumn && utf8.Valid(data) {
		return string(data)
	}
	return data
}

func exportText(value any, databaseType string) string {
	switch value := value.(type) {
	case nil:
		return ""
	case []byte:
		typ := strings.ToUpper(databaseType)
		if typ == "" || strings.Contains(typ, "BLOB") || strings.Contains(typ, "BINARY") || strings.Contains(typ, "BYTEA") || !utf8.Valid(value) {
			return "base64:" + base64.StdEncoding.EncodeToString(value)
		}
		return strings.ToValidUTF8(string(value), "�")
	case time.Time:
		return value.Format(time.RFC3339Nano)
	default:
		return fmt.Sprint(value)
	}
}
