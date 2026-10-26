package sqlformat

import (
	"strings"
	"testing"
)

func TestFormatSQLClausesAndOperators(t *testing.T) {
	got, err := Format("select a,b from users where id=1 and active=true order by id desc")
	if err != nil {
		t.Fatal(err)
	}
	want := "SELECT a, b\nFROM users\nWHERE id = 1\nAND active = TRUE\nORDER BY id DESC"
	if got != want {
		t.Fatalf("Format() = %q, want %q", got, want)
	}
}

func TestFormatPreservesQuotedTextAndComments(t *testing.T) {
	statement := "select `from`, 'where; AND', \"order\" from t -- where\nwhere note = 'x'';y' /* and order */"
	got, err := Format(statement)
	if err != nil {
		t.Fatal(err)
	}
	for _, preserved := range []string{"`from`", "'where; AND'", `"order"`, "-- where", "'x'';y'", "/* and order */"} {
		if !strings.Contains(got, preserved) {
			t.Errorf("formatted SQL %q does not preserve %q", got, preserved)
		}
	}
}

func TestFormatPreservesPostgresDollarQuotedValue(t *testing.T) {
	got, err := Format("select $body$from; where AND$body$ from t")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "$body$from; where AND$body$") {
		t.Fatalf("dollar-quoted body was changed: %q", got)
	}
}

func TestFormatRejectsUnterminatedQuotedText(t *testing.T) {
	for _, statement := range []string{"select 'unfinished", "select /* unfinished", "select $tag$unfinished"} {
		if _, err := Format(statement); err == nil {
			t.Errorf("Format(%q) succeeded; want an error", statement)
		}
	}
}

func TestFormatIsIdempotent(t *testing.T) {
	first, err := Format("select u.id, count(*) from users u left join orders o on o.user_id=u.id where u.active=1 group by u.id")
	if err != nil {
		t.Fatal(err)
	}
	second, err := Format(first)
	if err != nil {
		t.Fatal(err)
	}
	if second != first {
		t.Fatalf("formatter is not idempotent:\nfirst:  %q\nsecond: %q", first, second)
	}
}
