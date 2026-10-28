package app

import (
	"strings"
)

func sqlRiskSummary(statement string) string {
	code := strings.TrimSpace(sqlRiskCode(statement))
	// A single trailing statement terminator is normal SQL syntax. Do not
	// classify an otherwise plain read query as ambiguous because of it.
	code = strings.TrimSpace(strings.TrimRight(code, "; \t\r\n"))
	fields := strings.Fields(code)
	if len(fields) == 0 {
		return "未知或仅注释"
	}
	first := strings.ToUpper(fields[0])
	switch first {
	case "SELECT", "SHOW", "DESCRIBE":
		if riskySQLReadPattern.MatchString(code) || hasUnknownSQLFunctionCall(statement) {
			return "未知或可能有副作用，需要确认"
		}
		return "读取类（仍需检查函数副作用和数据库方言）"
	case "INSERT", "UPDATE", "DELETE", "REPLACE", "UPSERT", "MERGE":
		return "数据修改类"
	case "CREATE", "ALTER", "DROP", "TRUNCATE", "REINDEX", "VACUUM":
		return "结构或维护类"
	default:
		return "未知或可能有副作用；请仔细检查"
	}
}

var safeSQLReadFunctions = map[string]struct{}{
	"ABS": {}, "AVG": {}, "ARRAY_AGG": {}, "ARRAY_LENGTH": {}, "CAST": {}, "CEIL": {}, "CEILING": {},
	"CHAR_LENGTH": {}, "COALESCE": {}, "CONCAT": {}, "CONCAT_WS": {}, "CONVERT": {}, "COUNT": {},
	"DATE": {}, "DATE_TRUNC": {}, "DATETIME": {}, "DECODE": {}, "DENSE_RANK": {}, "EXTRACT": {},
	"FIRST_VALUE": {}, "FLOOR": {}, "FORMAT": {}, "FORMAT_TYPE": {}, "GREATEST": {}, "GROUP_CONCAT": {},
	"IFNULL": {}, "JSON_AGG": {}, "JSON_ARRAY": {}, "JSON_ARRAYAGG": {}, "JSON_EXTRACT": {},
	"JSON_OBJECT": {}, "JSON_OBJECTAGG": {}, "JSON_QUERY": {}, "JSON_VALUE": {}, "JSONB_AGG": {},
	"JSONB_OBJECT_AGG": {}, "LAG": {}, "LAST_VALUE": {}, "LEAD": {}, "LEAST": {}, "LEFT": {},
	"LENGTH": {}, "LOCATE": {}, "LOWER": {}, "LTRIM": {}, "MAX": {}, "MIN": {}, "MOD": {},
	"NTH_VALUE": {}, "NTILE": {}, "NULLIF": {}, "NVL": {}, "OCTET_LENGTH": {}, "POSITION": {},
	"POWER": {}, "RANK": {}, "REGEXP_LIKE": {}, "REGEXP_REPLACE": {}, "REGEXP_SUBSTR": {},
	"REPLACE": {}, "RIGHT": {}, "ROUND": {}, "ROW": {}, "ROW_NUMBER": {}, "RTRIM": {},
	"SAFE_CAST": {}, "STRING_AGG": {}, "SUBSTR": {}, "SUBSTRING": {}, "SUM": {}, "TIME": {},
	"TIMESTAMP": {}, "TO_CHAR": {}, "TO_DATE": {}, "TO_TIMESTAMP": {}, "TRIM": {}, "TRY_CAST": {},
	"TRY_CONVERT": {}, "TYPEOF": {}, "UPPER": {},
}

var sqlSyntaxBeforeParentheses = map[string]struct{}{
	"ALL": {}, "ANY": {}, "ARRAY": {}, "EXISTS": {}, "FILTER": {}, "IN": {}, "LATERAL": {}, "NOT": {}, "OVER": {},
	"SELECT": {}, "SOME": {}, "TABLE": {}, "VALUES": {}, "WITHIN": {},
	"BINARY": {}, "CHAR": {}, "CHARACTER": {}, "DECIMAL": {}, "NCHAR": {}, "NUMERIC": {},
	"NVARCHAR": {}, "VARBINARY": {}, "VARCHAR": {},
}

func hasUnknownSQLFunctionCall(statement string) bool {
	for i := 0; i < len(statement); {
		switch {
		case strings.HasPrefix(statement[i:], "--"):
			i = skipSQLLineComment(statement, i)
		case strings.HasPrefix(statement[i:], "/*"):
			i = skipSQLBlockComment(statement, i)
		case statement[i] == '\'':
			i = skipSQLQuoted(statement, i, '\'')
		case statement[i] == '$':
			if end, ok := skipSQLDollarQuoted(statement, i); ok {
				i = end
			} else {
				i++
			}
		case statement[i] == '"' || statement[i] == '`' || statement[i] == '[':
			close := statement[i]
			if close == '[' {
				close = ']'
			}
			_, end := readSQLQuotedIdentifier(statement, i, close)
			if next := skipSQLTrivia(statement, end); next < len(statement) && statement[next] == '(' {
				return true
			}
			i = end
		case isSQLIdentifierStart(statement[i]):
			start := i
			i++
			for i < len(statement) && isSQLIdentifierPart(statement[i]) {
				i++
			}
			name := strings.ToUpper(statement[start:i])
			if next := skipSQLTrivia(statement, i); next < len(statement) && statement[next] == '(' {
				if previousSQLNonSpace(statement, start) == '.' {
					return true
				}
				if _, syntax := sqlSyntaxBeforeParentheses[name]; !syntax && !isSafeSQLReadFunction(name) {
					return true
				}
			}
		default:
			i++
		}
	}
	return false
}

func previousSQLNonSpace(statement string, index int) byte {
	for index--; index >= 0; index-- {
		if statement[index] != ' ' && statement[index] != '\t' && statement[index] != '\r' && statement[index] != '\n' {
			return statement[index]
		}
	}
	return 0
}

func isSafeSQLReadFunction(name string) bool {
	_, ok := safeSQLReadFunctions[strings.ToUpper(name)]
	return ok
}

func isSQLIdentifierStart(char byte) bool {
	return char == '_' || char >= 'A' && char <= 'Z' || char >= 'a' && char <= 'z' || char >= 0x80
}

func isSQLIdentifierPart(char byte) bool {
	return isSQLIdentifierStart(char) || char >= '0' && char <= '9' || char == '$'
}

func skipSQLTrivia(statement string, index int) int {
	for index < len(statement) {
		switch {
		case statement[index] == ' ' || statement[index] == '\t' || statement[index] == '\r' || statement[index] == '\n':
			index++
		case strings.HasPrefix(statement[index:], "--"):
			index = skipSQLLineComment(statement, index)
		case strings.HasPrefix(statement[index:], "/*"):
			index = skipSQLBlockComment(statement, index)
		default:
			return index
		}
	}
	return index
}

func skipSQLLineComment(statement string, index int) int {
	if newline := strings.IndexByte(statement[index:], '\n'); newline >= 0 {
		return index + newline + 1
	}
	return len(statement)
}

func skipSQLBlockComment(statement string, index int) int {
	depth := 1
	for index += 2; index < len(statement); {
		switch {
		case strings.HasPrefix(statement[index:], "/*"):
			depth++
			index += 2
		case strings.HasPrefix(statement[index:], "*/"):
			depth--
			index += 2
			if depth == 0 {
				return index
			}
		default:
			index++
		}
	}
	return len(statement)
}

func skipSQLQuoted(statement string, index int, quote byte) int {
	for index++; index < len(statement); index++ {
		if statement[index] == quote {
			if index+1 < len(statement) && statement[index+1] == quote {
				index++
				continue
			}
			return index + 1
		}
	}
	return len(statement)
}

func readSQLQuotedIdentifier(statement string, index int, close byte) (string, int) {
	start := index + 1
	for index = start; index < len(statement); index++ {
		if statement[index] == close {
			if index+1 < len(statement) && statement[index+1] == close {
				index++
				continue
			}
			return statement[start:index], index + 1
		}
	}
	return statement[start:], len(statement)
}

func skipSQLDollarQuoted(statement string, index int) (int, bool) {
	endTag := index + 1
	for endTag < len(statement) && (statement[endTag] == '_' || statement[endTag] >= 'A' && statement[endTag] <= 'Z' || statement[endTag] >= 'a' && statement[endTag] <= 'z' || statement[endTag] >= '0' && statement[endTag] <= '9') {
		endTag++
	}
	if endTag >= len(statement) || statement[endTag] != '$' {
		return index, false
	}
	tag := statement[index : endTag+1]
	if close := strings.Index(statement[endTag+1:], tag); close >= 0 {
		return endTag + 1 + close + len(tag), true
	}
	return len(statement), true
}

func sqlRiskCode(statement string) string {
	code := []byte(statement)
	source := string(code)
	blank := func(start, end int) {
		for i := start; i < end; i++ {
			if code[i] != '\n' && code[i] != '\r' {
				code[i] = ' '
			}
		}
	}
	for i := 0; i < len(code); {
		switch {
		case strings.HasPrefix(source[i:], "--"):
			end := skipSQLLineComment(source, i)
			blank(i, end)
			i = end
		case strings.HasPrefix(source[i:], "/*"):
			end := skipSQLBlockComment(source, i)
			blank(i, end)
			i = end
		case code[i] == '\'':
			end := skipSQLQuoted(source, i, '\'')
			blank(i, end)
			i = end
		case code[i] == '"' || code[i] == '`' || code[i] == '[':
			close := code[i]
			if close == '[' {
				close = ']'
			}
			end := skipSQLQuoted(source, i, close)
			blank(i, end)
			i = end
		case code[i] == '$':
			if end, ok := skipSQLDollarQuoted(source, i); ok {
				blank(i, end)
				i = end
			} else {
				i++
			}
		default:
			i++
		}
	}
	return string(code)
}

func requiresSQLConfirmation(statement string, readOnly bool) bool {
	return !readOnly && !strings.HasPrefix(sqlRiskSummary(statement), "读取类")
}
