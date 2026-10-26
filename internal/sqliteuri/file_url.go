// Package sqliteuri builds file URLs for SQLite connections.
package sqliteuri

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

// FileURL converts an absolute filesystem path to a SQLite file URL. The
// windows flag is explicit so path forms can be tested on any host platform.
func FileURL(absPath string, windows bool) *url.URL {
	path := filepath.ToSlash(absPath)
	if windows {
		path = strings.ReplaceAll(absPath, `\`, "/")
		if strings.HasPrefix(path, "//") {
			unc := strings.TrimPrefix(path, "//")
			if separator := strings.IndexByte(unc, '/'); separator > 0 {
				return &url.URL{Scheme: "file", Host: unc[:separator], Path: unc[separator:]}
			}
		}
		if len(path) >= 3 && path[1] == ':' && path[2] == '/' {
			path = "/" + path
		}
	}
	return &url.URL{Scheme: "file", Path: path}
}

// SuggestNewFilePath proposes an unused SQLite filename near currentPath.
// If currentPath does not exist, it is already a suitable save-panel default.
func SuggestNewFilePath(currentPath string) string {
	if currentPath == "" {
		return "database.sqlite"
	}
	info, err := os.Stat(currentPath)
	if err != nil {
		return currentPath
	}
	directory := filepath.Dir(currentPath)
	base := filepath.Base(currentPath)
	if info.IsDir() {
		directory = currentPath
		base = "database.sqlite"
	}
	extension := filepath.Ext(base)
	name := strings.TrimSuffix(base, extension)
	if extension == "" {
		extension = ".sqlite"
	}
	for suffix := 1; ; suffix++ {
		candidateName := name + "-new" + extension
		if suffix > 1 {
			candidateName = fmt.Sprintf("%s-new-%d%s", name, suffix, extension)
		}
		candidate := filepath.Join(directory, candidateName)
		if _, err := os.Lstat(candidate); os.IsNotExist(err) {
			return candidate
		} else if err != nil {
			return candidate
		}
	}
}

// DataSourceName adds the SQLite access mode and connection pragmas to a file
// URL. Only read-only and read-write-existing modes are accepted; creation is
// handled separately by the adapter before connecting.
func DataSourceName(absPath, mode string, windows bool) (string, error) {
	if mode != "ro" && mode != "rw" {
		return "", fmt.Errorf("unsupported SQLite file mode %q", mode)
	}
	uri := FileURL(absPath, windows)
	query := url.Values{}
	query.Set("mode", mode)
	query.Add("_pragma", "busy_timeout(5000)")
	query.Add("_pragma", "foreign_keys(1)")
	uri.RawQuery = query.Encode()
	return uri.String(), nil
}
