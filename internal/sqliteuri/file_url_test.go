package sqliteuri

import (
	"net/url"
	"os"
	"path/filepath"
	"testing"
)

func TestFileURLEncodesPlatformPaths(t *testing.T) {
	tests := []struct {
		name    string
		path    string
		windows bool
		wantURI string
	}{
		{
			name:    "windows drive",
			path:    `C:\Users\me\my db #1.sqlite`,
			windows: true,
			wantURI: "file:///C:/Users/me/my%20db%20%231.sqlite",
		},
		{
			name:    "windows UNC",
			path:    `\\db-host\share\my db.sqlite`,
			windows: true,
			wantURI: "file://db-host/share/my%20db.sqlite",
		},
		{
			name:    "posix path",
			path:    "/Users/me/my db #1.sqlite",
			wantURI: "file:///Users/me/my%20db%20%231.sqlite",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := FileURL(test.path, test.windows).String(); got != test.wantURI {
				t.Fatalf("SQLite URI = %q, want %q", got, test.wantURI)
			}
		})
	}
}

func TestSuggestNewFilePathAvoidsExistingFiles(t *testing.T) {
	directory := t.TempDir()
	current := filepath.Join(directory, "data.sqlite")
	if err := os.WriteFile(current, []byte("database"), 0o600); err != nil {
		t.Fatal(err)
	}
	first := filepath.Join(directory, "data-new.sqlite")
	if got := SuggestNewFilePath(current); got != first {
		t.Fatalf("suggestion = %q, want %q", got, first)
	}
	if err := os.WriteFile(first, []byte("database"), 0o600); err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(directory, "data-new-2.sqlite")
	if got := SuggestNewFilePath(current); got != want {
		t.Fatalf("suggestion after collision = %q, want %q", got, want)
	}
}

func TestSuggestNewFilePathUsesTypedMissingPath(t *testing.T) {
	want := filepath.Join(t.TempDir(), "custom.sqlite")
	if got := SuggestNewFilePath(want); got != want {
		t.Fatalf("suggestion = %q, want existing typed path %q", got, want)
	}
}

func TestSuggestNewFilePathUsesDatabaseNameInsideDirectory(t *testing.T) {
	directory := t.TempDir()
	want := filepath.Join(directory, "database-new.sqlite")
	if got := SuggestNewFilePath(directory); got != want {
		t.Fatalf("suggestion = %q, want %q", got, want)
	}
}

func TestDataSourceNameModesAndPragmas(t *testing.T) {
	for _, mode := range []string{"ro", "rw"} {
		t.Run(mode, func(t *testing.T) {
			dsn, err := DataSourceName("/tmp/data #1.sqlite", mode, false)
			if err != nil {
				t.Fatal(err)
			}
			parsed, err := url.Parse(dsn)
			if err != nil {
				t.Fatalf("parse data source name %q: %v", dsn, err)
			}
			if parsed.Scheme != "file" || parsed.Path != "/tmp/data #1.sqlite" {
				t.Fatalf("parsed URI path = %q, scheme %q", parsed.Path, parsed.Scheme)
			}
			query := parsed.Query()
			if got := query.Get("mode"); got != mode {
				t.Fatalf("mode = %q, want %q", got, mode)
			}
			pragmas := query["_pragma"]
			if len(pragmas) != 2 || pragmas[0] != "busy_timeout(5000)" || pragmas[1] != "foreign_keys(1)" {
				t.Fatalf("pragmas = %#v", pragmas)
			}
		})
	}
	if _, err := DataSourceName("/tmp/data.sqlite", "create", false); err == nil {
		t.Fatal("unsupported mode was accepted")
	}
}
