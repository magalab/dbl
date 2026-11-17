package app

import "testing"

func TestDMLExecutionKeepsReturningAndMultipleStatementsOnQueryPath(t *testing.T) {
	for _, statement := range []string{
		"INSERT INTO records VALUES (1) RETURNING id",
		"INSERT INTO records VALUES (1); SELECT * FROM records",
		"/* note */ UPDATE records SET id = 2; SELECT id FROM records",
		"WITH changed AS (DELETE FROM records RETURNING *) SELECT * FROM changed",
		"SELECT * FROM records",
	} {
		if _, ok := dmlStatementVerb(statement); ok {
			t.Fatalf("statement with results would use Exec: %q", statement)
		}
	}
	for _, statement := range []string{
		"INSERT INTO records VALUES (1);",
		"-- comment\nUPDATE records SET id = 2",
		"/* comment */ DELETE FROM records WHERE id = 2;",
	} {
		if _, ok := dmlStatementVerb(statement); !ok {
			t.Fatalf("direct DML did not use Exec: %q", statement)
		}
	}
}
