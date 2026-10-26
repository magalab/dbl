package sqlstream

import (
	"encoding/csv"
	"strings"
)

// FormatClipboardRow encodes one row as tab-separated text, quoting tabs,
// line breaks and quotes so pasting into a spreadsheet preserves cell values.
func FormatClipboardRow(values []string) string {
	var output strings.Builder
	writer := csv.NewWriter(&output)
	writer.Comma = '\t'
	_ = writer.Write(values)
	writer.Flush()
	return strings.TrimRight(output.String(), "\r\n")
}
