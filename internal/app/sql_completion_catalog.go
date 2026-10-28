package app

import "github.com/magalab/dbl/internal/connection"

func sqlCompletionKeywords(driver connection.Driver) []string {
	keywords := []string{
		"SELECT", "WITH", "RECURSIVE", "FROM", "JOIN", "INNER JOIN", "LEFT JOIN", "LEFT OUTER JOIN", "RIGHT JOIN", "FULL JOIN", "CROSS JOIN",
		"ON", "WHERE", "AND", "OR", "NOT", "IN", "EXISTS", "BETWEEN", "LIKE", "IS NULL", "IS NOT NULL", "AS",
		"DISTINCT", "ALL", "CASE", "WHEN", "THEN", "ELSE", "END", "GROUP BY", "HAVING", "ORDER BY", "ASC", "DESC", "NULLS FIRST", "NULLS LAST",
		"LIMIT", "OFFSET", "FETCH FIRST", "UNION", "UNION ALL", "INTERSECT", "EXCEPT", "INSERT INTO", "VALUES", "UPDATE", "SET", "DELETE FROM",
		"CREATE", "ALTER", "DROP", "TABLE", "VIEW", "INDEX", "PRIMARY KEY", "FOREIGN KEY", "REFERENCES", "NOT NULL", "DEFAULT",
		"BEGIN", "COMMIT", "ROLLBACK", "EXPLAIN", "ANALYZE", "CAST", "COLLATE", "OVER", "PARTITION BY", "WINDOW", "WITHIN GROUP",
	}
	switch driver {
	case connection.DriverPostgreSQL:
		keywords = append(keywords, "ILIKE", "RETURNING", "ON CONFLICT", "DO NOTHING", "DO UPDATE", "JSONB", "FILTER (WHERE)")
	case connection.DriverMySQL:
		keywords = append(keywords, "REPLACE INTO", "ON DUPLICATE KEY UPDATE", "SHOW", "DESCRIBE", "USE", "AUTO_INCREMENT")
	case connection.DriverSQLite:
		keywords = append(keywords, "PRAGMA", "WITHOUT ROWID", "ON CONFLICT", "DO NOTHING", "DO UPDATE", "RETURNING")
	}
	return keywords
}

func sqlCompletionFunctions(driver connection.Driver) []string {
	functions := []string{
		"COUNT", "SUM", "AVG", "MIN", "MAX", "COALESCE", "NULLIF", "CAST", "ROUND", "ABS", "LOWER", "UPPER", "LENGTH", "TRIM", "LTRIM", "RTRIM",
		"SUBSTRING", "REPLACE", "CONCAT", "CONCAT_WS", "POSITION", "CURRENT_DATE", "CURRENT_TIME", "CURRENT_TIMESTAMP", "NOW", "EXTRACT",
		"DATE", "TIME", "DATETIME", "STRFTIME", "ROW_NUMBER", "RANK", "DENSE_RANK", "LAG", "LEAD", "FIRST_VALUE", "LAST_VALUE", "JSON_VALUE", "JSON_QUERY",
	}
	switch driver {
	case connection.DriverPostgreSQL:
		functions = append(functions, "STRING_AGG", "ARRAY_AGG", "JSON_AGG", "JSONB_AGG", "JSON_BUILD_OBJECT", "JSONB_BUILD_OBJECT", "TO_CHAR", "DATE_TRUNC", "GENERATE_SERIES")
	case connection.DriverMySQL:
		functions = append(functions, "GROUP_CONCAT", "JSON_ARRAYAGG", "JSON_OBJECTAGG", "IFNULL", "DATE_FORMAT", "TIMESTAMPDIFF", "UUID")
	case connection.DriverSQLite:
		functions = append(functions, "GROUP_CONCAT", "JSON_GROUP_ARRAY", "JSON_OBJECT", "IIF", "TOTAL", "PRINTF")
	}
	return functions
}
