package app

import "strings"

type sqlCompletionRelation struct {
	schema string
	name   string
	alias  string
}

type sqlCompletionContext uint8

const (
	sqlCompletionExpression sqlCompletionContext = iota
	sqlCompletionRelationName
	sqlCompletionQualifiedColumn
)

type sqlCompletionResolvedRelation struct {
	relation sqlCompletionRelation
	schema   string
	name     string
}

func sqlCompletionQualifierMatches(qualifier string, relation sqlCompletionRelation) bool {
	qualifier = strings.Trim(qualifier, "\"`[]")
	return strings.EqualFold(qualifier, relation.alias) || strings.EqualFold(qualifier, relation.name)
}

func sqlCompletionContextAt(tokens []sqlCompletionToken) (sqlCompletionContext, string, string) {
	if len(tokens) == 0 {
		return sqlCompletionExpression, "", ""
	}
	last := tokens[len(tokens)-1]
	if last.text == "." {
		if len(tokens) >= 2 {
			qualifier := tokens[len(tokens)-2].text
			if sqlCompletionRelationQualifierExpected(tokens) {
				return sqlCompletionRelationName, qualifier, ""
			}
			return sqlCompletionQualifiedColumn, "", qualifier
		}
	}
	if sqlCompletionRelationExpected(tokens) {
		return sqlCompletionRelationName, "", ""
	}
	return sqlCompletionExpression, "", ""
}

func sqlCompletionRelationExpected(tokens []sqlCompletionToken) bool {
	if len(tokens) == 0 {
		return false
	}
	last := tokens[len(tokens)-1]
	if last.quoted {
		return false
	}
	if sqlCompletionRelationOperator(last.lower) {
		return true
	}
	return last.text == "," && sqlCompletionHasOpenFromClause(tokens)
}

func sqlCompletionRelationQualifierExpected(tokens []sqlCompletionToken) bool {
	if len(tokens) < 3 || tokens[len(tokens)-1].text != "." {
		return false
	}
	qualifier := tokens[len(tokens)-2]
	if !qualifier.identifier {
		return false
	}
	start := len(tokens) - 2
	for start > 0 {
		previous := tokens[start-1]
		if sqlCompletionRelationOperator(previous.lower) || (previous.text == "," && sqlCompletionHasOpenFromClause(tokens[:start])) {
			return start == len(tokens)-2
		}
		if sqlCompletionClause(previous.lower) || previous.text == ";" {
			return false
		}
		start--
	}
	return false
}

func sqlCompletionHasOpenFromClause(tokens []sqlCompletionToken) bool {
	open := false
	for _, token := range tokens {
		if !token.identifier || token.quoted {
			continue
		}
		switch token.lower {
		case "from", "join":
			open = true
		case "where", "group", "order", "having", "limit", "offset", "union", "returning", "set", "values":
			open = false
		}
	}
	return open
}

func sqlCompletionRelationOperator(word string) bool {
	switch word {
	case "from", "join", "into", "update":
		return true
	default:
		return false
	}
}

func sqlCompletionClause(word string) bool {
	switch word {
	case "where", "group", "order", "having", "limit", "offset", "union", "returning", "set", "values", "on", "when", "then", "else", "left", "right", "full", "inner", "outer", "cross", "natural":
		return true
	default:
		return false
	}
}

func parseSQLCompletionRelations(tokens []sqlCompletionToken) []sqlCompletionRelation {
	relations := make([]sqlCompletionRelation, 0, 4)
	fromOpen := false
	depth := 0
	for index := 0; index < len(tokens); index++ {
		token := tokens[index]
		if token.quoted || !token.identifier {
			if token.text == "(" {
				depth++
			} else if token.text == ")" && depth > 0 {
				depth--
			} else if token.text == "," && fromOpen && depth == 0 {
				relation, next := parseSQLCompletionRelationAt(tokens, index+1)
				if relation.name != "" {
					relations = append(relations, relation)
				}
				if next > index {
					index = next - 1
				}
			}
			continue
		}
		switch token.lower {
		case "from", "join":
			fromOpen = true
			relation, next := parseSQLCompletionRelationAt(tokens, index+1)
			if relation.name != "" {
				relations = append(relations, relation)
			}
			if next > index+1 {
				index = next - 1
			}
		case "update", "into":
			relation, next := parseSQLCompletionRelationAt(tokens, index+1)
			if relation.name != "" {
				relations = append(relations, relation)
			}
			if next > index+1 {
				index = next - 1
			}
		case "where", "group", "order", "having", "limit", "offset", "union", "returning", "set", "values":
			fromOpen = false
		}
	}
	return relations
}

func parseSQLCompletionRelationAt(tokens []sqlCompletionToken, index int) (sqlCompletionRelation, int) {
	relation := sqlCompletionRelation{}
	for index < len(tokens) && tokens[index].identifier && !tokens[index].quoted && (tokens[index].lower == "only" || tokens[index].lower == "lateral") {
		index++
	}
	if index >= len(tokens) || !tokens[index].identifier || tokens[index].text == "(" {
		return relation, index
	}
	first := tokens[index].text
	index++
	if index+1 < len(tokens) && tokens[index].text == "." && tokens[index+1].identifier {
		relation.schema, relation.name = first, tokens[index+1].text
		index += 2
	} else {
		relation.name = first
	}
	if index < len(tokens) && !tokens[index].quoted && tokens[index].lower == "as" {
		index++
		if index < len(tokens) && tokens[index].identifier {
			relation.alias = tokens[index].text
			index++
		}
	} else if index < len(tokens) && tokens[index].identifier && !tokens[index].quoted && !sqlCompletionClause(tokens[index].lower) && !sqlCompletionRelationOperator(tokens[index].lower) && tokens[index].lower != "on" {
		relation.alias = tokens[index].text
		index++
	}
	if relation.alias == "" {
		relation.alias = relation.name
	}
	return relation, index
}
