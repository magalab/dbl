// Package sqlformat provides a small lexical SQL formatter for the workbench.
// It changes whitespace and recognized keyword casing while preserving quoted
// values, quoted identifiers, and comments.
package sqlformat

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
)

type tokenKind uint8

const (
	wordToken tokenKind = iota
	quotedToken
	commentToken
	symbolToken
)

type token struct {
	text string
	kind tokenKind
}

var keywords = wordSet(strings.Fields(`ADD ALL ALTER ANALYZE AND AS ASC BEGIN BETWEEN BY CALL CASCADE CASE CAST CHECK COLLATE COMMIT CONSTRAINT CREATE CROSS CURRENT_DATE CURRENT_TIME CURRENT_TIMESTAMP DATABASE DEFAULT DELETE DESC DESCRIBE DISTINCT DROP ELSE END EXCEPT EXISTS EXPLAIN FALSE FETCH FIRST FOR FOREIGN FROM FULL FULLTEXT GROUP HAVING IF ILIKE IN INDEX INNER INSERT INTERSECT INTO IS JOIN KEY LEFT LIKE LIMIT LOCK MERGE NATURAL NOT NULL OFFSET ON ONLY OR ORDER OUTER OVER PARTITION PRIMARY PRAGMA REFERENCES REINDEX RELEASE RENAME REPLACE RETURNING RIGHT ROLLBACK ROW ROWS SAVEPOINT SCHEMA SELECT SET SHOW TABLE THEN TO TRANSACTION TRUNCATE TRUE UNION UNIQUE UPDATE USE USING VACUUM VALUES VIEW WHEN WHERE WITH`))

var lineBreakKeywords = wordSet(strings.Fields(`FROM WHERE GROUP ORDER HAVING LIMIT OFFSET FETCH UNION INTERSECT EXCEPT RETURNING VALUES SET JOIN ON`))

// Format returns a readable SQL layout. It does not parse or validate a SQL
// dialect; malformed quoted values and comments are returned as errors so the
// caller can leave the user's buffer untouched.
func Format(statement string) (string, error) {
	tokens, err := lex(statement)
	if err != nil {
		return "", err
	}
	var out bytes.Buffer
	depth := 0
	atLineStart := true
	previous := token{}
	havePrevious := false
	previousWord := ""
	writeIndent := func() {
		if atLineStart {
			out.WriteString(strings.Repeat(" ", depth*2))
			atLineStart = false
		}
	}
	trimSpaces := func() {
		value := out.Bytes()
		for len(value) > 0 && (value[len(value)-1] == ' ' || value[len(value)-1] == '\t') {
			value = value[:len(value)-1]
		}
		out.Truncate(len(value))
	}
	newline := func() {
		trimSpaces()
		value := out.Bytes()
		if len(value) > 0 && value[len(value)-1] != '\n' {
			out.WriteByte('\n')
		}
		atLineStart = true
	}
	space := func() {
		if atLineStart {
			return
		}
		value := out.Bytes()
		if len(value) == 0 {
			return
		}
		last := value[len(value)-1]
		if last != ' ' && last != '\n' && last != '\t' && last != '(' && last != '.' {
			out.WriteByte(' ')
		}
	}
	write := func(value string) {
		writeIndent()
		out.WriteString(value)
	}

	for i, current := range tokens {
		upper := strings.ToUpper(current.text)
		nextWord := ""
		if i+1 < len(tokens) && tokens[i+1].kind == wordToken {
			nextWord = strings.ToUpper(tokens[i+1].text)
		}
		switch current.kind {
		case wordToken:
			if shouldLineBreak(upper, nextWord, previousWord, depth) {
				newline()
			} else if upper == "AND" || upper == "OR" {
				newline()
			} else if havePrevious && needsWordSpace(previous) {
				space()
			}
			if keywords[upper] {
				write(upper)
			} else {
				write(current.text)
			}
			previousWord = upper
		case quotedToken:
			if havePrevious && needsWordSpace(previous) {
				space()
			}
			write(current.text)
			previousWord = ""
		case commentToken:
			if havePrevious && !atLineStart {
				space()
			}
			write(current.text)
			previousWord = ""
			if strings.HasPrefix(current.text, "--") || strings.HasPrefix(current.text, "#") {
				newline()
			} else if strings.HasSuffix(current.text, "\n") {
				atLineStart = true
			}
		case symbolToken:
			switch current.text {
			case "(":
				if previousWord == "IN" || previousWord == "VALUES" || previousWord == "AS" || previousWord == "SELECT" || previousWord == "FROM" || previousWord == "WHERE" || previousWord == "ON" || previousWord == "EXISTS" || previousWord == "NOT" || previousWord == "OVER" {
					space()
				}
				write("(")
				depth++
			case ")":
				if depth > 0 {
					depth--
				}
				trimSpaces()
				write(")")
			case ".":
				trimSpaces()
				write(".")
			case ",":
				trimSpaces()
				write(",")
				space()
			case ";":
				trimSpaces()
				write(";")
				newline()
			case "::":
				trimSpaces()
				write("::")
			case "@":
				trimSpaces()
				write("@")
			default:
				space()
				write(current.text)
				space()
			}
			previousWord = ""
		}
		previous, havePrevious = current, true
	}
	result := strings.TrimSpace(out.String())
	return result, nil
}

func shouldLineBreak(current, next, previous string, depth int) bool {
	if current == "AND" || current == "OR" {
		return false
	}
	if (current == "LEFT" || current == "RIGHT" || current == "FULL" || current == "INNER" || current == "CROSS" || current == "NATURAL") && next == "JOIN" {
		return true
	}
	if current == "JOIN" && previous != "LEFT" && previous != "RIGHT" && previous != "FULL" && previous != "INNER" && previous != "CROSS" && previous != "NATURAL" {
		return true
	}
	if !lineBreakKeywords[current] {
		return false
	}
	if depth < 0 {
		return false
	}
	return true
}

func needsWordSpace(previous token) bool {
	if previous.kind == symbolToken {
		switch previous.text {
		case "(", ".", "@", "::":
			return false
		case ",", ")":
			return true
		default:
			return true
		}
	}
	return true
}

func wordSet(words []string) map[string]bool {
	set := make(map[string]bool, len(words))
	for _, word := range words {
		set[word] = true
	}
	return set
}

func lex(statement string) ([]token, error) {
	tokens := make([]token, 0, len(statement)/4)
	for i := 0; i < len(statement); {
		c := statement[i]
		if isSpace(c) {
			i++
			continue
		}
		if c == '-' && i+1 < len(statement) && statement[i+1] == '-' || c == '#' {
			start := i
			for i < len(statement) && statement[i] != '\n' {
				i++
			}
			tokens = append(tokens, token{text: statement[start:i], kind: commentToken})
			continue
		}
		if c == '/' && i+1 < len(statement) && statement[i+1] == '*' {
			start, depth := i, 1
			i += 2
			for i < len(statement) && depth > 0 {
				switch {
				case i+1 < len(statement) && statement[i] == '/' && statement[i+1] == '*':
					depth++
					i += 2
				case i+1 < len(statement) && statement[i] == '*' && statement[i+1] == '/':
					depth--
					i += 2
				default:
					i++
				}
			}
			if depth != 0 {
				return nil, errors.New("unterminated SQL block comment")
			}
			tokens = append(tokens, token{text: statement[start:i], kind: commentToken})
			continue
		}
		if c == '\'' || c == '"' || c == '`' {
			value, next, err := scanQuoted(statement, i, c)
			if err != nil {
				return nil, err
			}
			tokens = append(tokens, token{text: value, kind: quotedToken})
			i = next
			continue
		}
		if c == '$' {
			if value, next, ok, err := scanDollarQuoted(statement, i); err != nil {
				return nil, err
			} else if ok {
				tokens = append(tokens, token{text: value, kind: quotedToken})
				i = next
				continue
			}
		}
		if isWordByte(c) {
			start := i
			for i < len(statement) && isWordByte(statement[i]) {
				i++
			}
			tokens = append(tokens, token{text: statement[start:i], kind: wordToken})
			continue
		}
		operator := longestOperator(statement[i:])
		if operator != "" {
			tokens = append(tokens, token{text: operator, kind: symbolToken})
			i += len(operator)
		} else {
			tokens = append(tokens, token{text: statement[i : i+1], kind: symbolToken})
			i++
		}
	}
	return tokens, nil
}

func scanQuoted(statement string, start int, quote byte) (string, int, error) {
	for i := start + 1; i < len(statement); i++ {
		if statement[i] == '\\' && i+1 < len(statement) {
			i++
			continue
		}
		if statement[i] == quote {
			if i+1 < len(statement) && statement[i+1] == quote {
				i++
				continue
			}
			return statement[start : i+1], i + 1, nil
		}
	}
	return "", start, fmt.Errorf("unterminated SQL quoted value starting at byte %d", start)
}

func scanDollarQuoted(statement string, start int) (string, int, bool, error) {
	i := start + 1
	for i < len(statement) && (isWordByte(statement[i]) && statement[i] != '$') {
		i++
	}
	if i >= len(statement) || statement[i] != '$' {
		return "", start, false, nil
	}
	delimiter := statement[start : i+1]
	end := strings.Index(statement[i+1:], delimiter)
	if end < 0 {
		return "", start, true, fmt.Errorf("unterminated SQL dollar-quoted value starting at byte %d", start)
	}
	end += i + 1 + len(delimiter)
	return statement[start:end], end, true, nil
}

func longestOperator(statement string) string {
	for _, operator := range []string{"->>", "#>>", "!~*", "::", "<=", ">=", "<>", "!=", "||", "&&", "->", "#>", "!~", "~*", ":=", "=>", "<<", ">>"} {
		if strings.HasPrefix(statement, operator) {
			return operator
		}
	}
	return ""
}

func isSpace(c byte) bool { return c == ' ' || c == '\t' || c == '\r' || c == '\n' }

func isWordByte(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c >= 0x80 || c == '$'
}
