package app

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/magalab/dbl/internal/connection"
)

type sqlCompletionToken struct {
	text       string
	lower      string
	start      int
	end        int
	identifier bool
	quoted     bool
}

type sqlCompletionRange struct {
	start      int
	end        int
	includeEnd bool
}

func lexSQLCompletionTokens(sql string, driver connection.Driver) ([]sqlCompletionToken, []sqlCompletionRange) {
	tokens := make([]sqlCompletionToken, 0, len(sql)/5)
	opaque := make([]sqlCompletionRange, 0, 8)
	for offset := 0; offset < len(sql); {
		r, size := utf8.DecodeRuneInString(sql[offset:])
		if unicode.IsSpace(r) {
			offset += size
			continue
		}
		if strings.HasPrefix(sql[offset:], "--") || (driver == connection.DriverMySQL && r == '#') {
			start := offset
			for offset < len(sql) && sql[offset] != '\n' && sql[offset] != '\r' {
				offset++
			}
			opaque = append(opaque, sqlCompletionRange{start: start, end: offset, includeEnd: offset == len(sql)})
			continue
		}
		if strings.HasPrefix(sql[offset:], "/*") {
			start := offset
			offset += 2
			depth := 1
			for offset < len(sql) && depth > 0 {
				switch {
				case strings.HasPrefix(sql[offset:], "/*"):
					depth++
					offset += 2
				case strings.HasPrefix(sql[offset:], "*/"):
					depth--
					offset += 2
				default:
					_, width := utf8.DecodeRuneInString(sql[offset:])
					offset += width
				}
			}
			opaque = append(opaque, sqlCompletionRange{start: start, end: offset, includeEnd: depth > 0})
			continue
		}
		if r == '\'' {
			start := offset
			var closed bool
			offset, closed = scanSQLQuoted(sql, offset, '\'', true)
			opaque = append(opaque, sqlCompletionRange{start: start, end: offset, includeEnd: !closed})
			continue
		}
		if r == '"' || r == '`' || r == '[' {
			start := offset
			closeQuote := r
			if r == '[' {
				closeQuote = ']'
			}
			var closed bool
			offset, closed = scanSQLQuoted(sql, offset, closeQuote, false)
			opaque = append(opaque, sqlCompletionRange{start: start, end: offset, includeEnd: !closed})
			if closed {
				content := sql[start+1 : offset-1]
				content = strings.ReplaceAll(content, string([]byte{byte(closeQuote), byte(closeQuote)}), string(closeQuote))
				tokens = append(tokens, sqlCompletionToken{text: content, lower: strings.ToLower(content), start: start, end: offset, identifier: true, quoted: true})
			}
			continue
		}
		if r == '$' {
			if delimiter, end, ok := sqlDollarQuoteDelimiter(sql, offset); ok {
				start := offset
				closing := strings.Index(sql[end:], delimiter)
				if closing < 0 {
					offset = len(sql)
				} else {
					offset = end + closing + len(delimiter)
				}
				opaque = append(opaque, sqlCompletionRange{start: start, end: offset, includeEnd: closing < 0})
				continue
			}
		}
		if sqlIdentifierStart(r) {
			start := offset
			offset += size
			for offset < len(sql) {
				next, width := utf8.DecodeRuneInString(sql[offset:])
				if !sqlIdentifierContinue(next) {
					break
				}
				offset += width
			}
			word := sql[start:offset]
			tokens = append(tokens, sqlCompletionToken{text: word, lower: strings.ToLower(word), start: start, end: offset, identifier: true})
			continue
		}
		start := offset
		offset += size
		tokens = append(tokens, sqlCompletionToken{text: sql[start:offset], lower: strings.ToLower(sql[start:offset]), start: start, end: offset})
	}
	return tokens, opaque
}

func scanSQLQuoted(sql string, start int, closing rune, backslashEscape bool) (int, bool) {
	_, width := utf8.DecodeRuneInString(sql[start:])
	offset := start + width
	for offset < len(sql) {
		r, size := utf8.DecodeRuneInString(sql[offset:])
		if backslashEscape && r == '\\' {
			offset += size
			if offset < len(sql) {
				_, escapedSize := utf8.DecodeRuneInString(sql[offset:])
				offset += escapedSize
			}
			continue
		}
		if r == closing {
			nextOffset := offset + size
			if nextOffset < len(sql) {
				next, nextSize := utf8.DecodeRuneInString(sql[nextOffset:])
				if next == closing {
					offset = nextOffset + nextSize
					continue
				}
			}
			return nextOffset, true
		}
		offset += size
	}
	return len(sql), false
}

func sqlDollarQuoteDelimiter(sql string, start int) (string, int, bool) {
	end := start + 1
	for end < len(sql) {
		r, size := utf8.DecodeRuneInString(sql[end:])
		if r == '$' {
			return sql[start : end+size], end + size, true
		}
		if !(unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_') {
			return "", 0, false
		}
		end += size
	}
	return "", 0, false
}

func sqlIdentifierStart(r rune) bool {
	return unicode.IsLetter(r) || r == '_' || r == '$'
}

func sqlIdentifierContinue(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '$'
}

func sqlCompletionInsideOpaqueRange(ranges []sqlCompletionRange, offset int) bool {
	for _, span := range ranges {
		if offset >= span.start && (offset < span.end || span.includeEnd && offset == span.end) {
			return true
		}
	}
	return false
}

func sqlCompletionStatement(tokens []sqlCompletionToken, offset int) []sqlCompletionToken {
	start, end := 0, len(tokens)
	for index, token := range tokens {
		if token.text != ";" {
			continue
		}
		if token.start < offset {
			start = index + 1
		} else {
			end = index
			break
		}
	}
	if start > end {
		start = end
	}
	return tokens[start:end]
}
