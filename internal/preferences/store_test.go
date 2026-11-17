package preferences

import (
	"errors"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestSettingsWriteCrashHelper(t *testing.T) {
	if stage := os.Getenv("DBL_PREFERENCES_KILL_STAGE"); stage != "" {
		dir := os.Getenv("DBL_PREFERENCES_CRASH_DIR")
		marker := os.Getenv("DBL_PREFERENCES_KILL_MARKER")
		err := writeSettingsWithCheckpoint(dir, Settings{Version: 1, Appearance: "dark"}, func(current string) {
			if current == stage {
				if err := os.WriteFile(marker, []byte("ready"), 0o600); err != nil {
					t.Fatal(err)
				}
				for {
					time.Sleep(time.Hour)
				}
			}
		})
		if err != nil {
			t.Fatal(err)
		}
		return
	}
	stage := os.Getenv("DBL_PREFERENCES_CRASH_STAGE")
	if stage == "" {
		return
	}
	dir := os.Getenv("DBL_PREFERENCES_CRASH_DIR")
	err := writeSettingsWithCheckpoint(dir, Settings{Version: 1, Appearance: "dark"}, func(current string) {
		if current == stage {
			os.Exit(86)
		}
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestSettingsWriteProcessKillKeepsOldOrCompleteNewFile(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "dbl")
	for _, test := range []struct {
		stage string
		want  string
	}{
		{stage: "written", want: "light"},
		{stage: "replaced", want: "dark"},
	} {
		if err := writeSettings(dir, Settings{Version: 1, Appearance: "light"}); err != nil {
			t.Fatalf("seed old settings: %v", err)
		}
		marker := filepath.Join(t.TempDir(), "ready")
		cmd := exec.Command(os.Args[0], "-test.run=^TestSettingsWriteCrashHelper$")
		cmd.Env = append(os.Environ(),
			"DBL_PREFERENCES_KILL_STAGE="+test.stage,
			"DBL_PREFERENCES_CRASH_DIR="+dir,
			"DBL_PREFERENCES_KILL_MARKER="+marker,
		)
		if err := cmd.Start(); err != nil {
			t.Fatalf("start child at checkpoint %q: %v", test.stage, err)
		}
		deadline := time.Now().Add(10 * time.Second)
		for {
			if _, err := os.Stat(marker); err == nil {
				break
			} else if !os.IsNotExist(err) {
				_ = cmd.Process.Kill()
				_ = cmd.Wait()
				t.Fatalf("wait for child checkpoint %q: %v", test.stage, err)
			}
			if time.Now().After(deadline) {
				_ = cmd.Process.Kill()
				_ = cmd.Wait()
				t.Fatalf("child did not reach checkpoint %q", test.stage)
			}
			time.Sleep(10 * time.Millisecond)
		}
		if err := cmd.Process.Kill(); err != nil {
			_ = cmd.Wait()
			t.Fatalf("kill child at checkpoint %q: %v", test.stage, err)
		}
		if err := cmd.Wait(); err == nil {
			t.Fatalf("child survived kill at checkpoint %q", test.stage)
		}
		store, err := OpenAt(dir)
		if err != nil {
			t.Fatalf("reopen after kill at %q: %v", test.stage, err)
		}
		if got := store.Settings().Appearance; got != test.want {
			t.Errorf("appearance after kill at %q = %q, want %q", test.stage, got, test.want)
		}
	}
}

func TestSettingsWriteProcessExitKeepsOldOrCompleteNewFile(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "dbl")
	for _, test := range []struct {
		stage string
		want  string
	}{
		{stage: "created", want: "light"},
		{stage: "written", want: "light"},
		{stage: "synced", want: "light"},
		{stage: "closed", want: "light"},
		{stage: "replaced", want: "dark"},
	} {
		if err := writeSettings(dir, Settings{Version: 1, Appearance: "light"}); err != nil {
			t.Fatalf("seed old settings: %v", err)
		}
		cmd := exec.Command(os.Args[0], "-test.run=^TestSettingsWriteCrashHelper$")
		cmd.Env = append(os.Environ(), "DBL_PREFERENCES_CRASH_STAGE="+test.stage, "DBL_PREFERENCES_CRASH_DIR="+dir)
		if err := cmd.Run(); err == nil {
			t.Fatalf("child exited successfully at checkpoint %q", test.stage)
		} else if exitError, ok := err.(*exec.ExitError); !ok || exitError.ExitCode() != 86 {
			t.Fatalf("child exit at checkpoint %q = %v, want exit code 86", test.stage, err)
		}
		store, err := OpenAt(dir)
		if err != nil {
			t.Fatalf("reopen after exit at %q: %v", test.stage, err)
		}
		if got := store.Settings().Appearance; got != test.want {
			t.Errorf("appearance after exit at %q = %q, want %q", test.stage, got, test.want)
		}
	}
}

func TestOpenStartsWithEmptySettings(t *testing.T) {
	store, err := OpenAt(filepath.Join(t.TempDir(), "missing"))
	if err != nil {
		t.Fatal(err)
	}
	got := store.Settings()
	if got.Version != 1 || got.LastSQLitePath != "" || len(got.RecentSQLitePaths) != 0 {
		t.Fatalf("initial settings = %#v, want empty version 1", got)
	}
}

func TestAppearancePersistsAndRejectsInvalidOrStaleUpdates(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "dbl")
	store, err := OpenAt(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetAppearance("sepia", 1); err == nil {
		t.Fatal("invalid appearance was accepted")
	}
	if err := store.SetAppearance("dark", 2); err != nil {
		t.Fatal(err)
	}
	if err := store.SetAppearance("light", 1); err == nil {
		t.Fatal("stale appearance update was accepted")
	}
	reloaded, err := OpenAt(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := reloaded.Settings().Appearance; got != "dark" {
		t.Fatalf("appearance = %q, want dark", got)
	}
}

func TestQueryHistoryIsOptInBoundedAndClearable(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "dbl")
	store, err := OpenAt(dir)
	if err != nil {
		t.Fatal(err)
	}
	if settings := store.Settings(); settings.QueryHistoryEnabled || len(settings.QueryHistory) != 0 {
		t.Fatalf("new settings unexpectedly retain query history: %#v", settings)
	}
	started := time.Date(2026, 10, 26, 1, 2, 3, 0, time.UTC)
	entries := []QueryHistoryEntry{{ID: 1, SQL: "SELECT 1", Started: started, Duration: 5 * time.Millisecond, Rows: 1, Outcome: "完成"}}
	if err := store.SetQueryHistory(true, entries, 1); err != nil {
		t.Fatal(err)
	}
	entries[0].SQL = "mutated caller copy"
	if got := store.Settings().QueryHistory[0].SQL; got != "SELECT 1" {
		t.Fatalf("store retained caller mutation: %q", got)
	}
	reloaded, err := OpenAt(dir)
	if err != nil {
		t.Fatal(err)
	}
	if settings := reloaded.Settings(); !settings.QueryHistoryEnabled || len(settings.QueryHistory) != 1 || settings.QueryHistory[0].SQL != "SELECT 1" {
		t.Fatalf("persisted query history = %#v", settings)
	}
	if err := reloaded.SetQueryHistory(false, nil, 1); err != nil {
		t.Fatal(err)
	}
	reloaded, err = OpenAt(dir)
	if err != nil {
		t.Fatal(err)
	}
	if settings := reloaded.Settings(); settings.QueryHistoryEnabled || len(settings.QueryHistory) != 0 {
		t.Fatalf("disabled query history remained on disk: %#v", settings)
	}
}

func TestQueryHistoryRejectsInvalidRecordsAndKeepsNewestLimit(t *testing.T) {
	store, err := OpenAt(filepath.Join(t.TempDir(), "dbl"))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	invalid := QueryHistoryEntry{ID: 1, SQL: strings.Repeat("x", MaxQueryHistorySQLBytes+1), Started: now}
	if err := store.SetQueryHistory(true, []QueryHistoryEntry{invalid}, 1); err == nil {
		t.Fatal("oversized SQL history entry was accepted")
	}
	entries := make([]QueryHistoryEntry, MaxQueryHistoryEntries+1)
	for i := range entries {
		entries[i] = QueryHistoryEntry{ID: uint64(i + 1), SQL: "SELECT 1", Started: now, Outcome: "done"}
	}
	if err := store.SetQueryHistory(true, entries, 1); err != nil {
		t.Fatal(err)
	}
	got := store.Settings().QueryHistory
	if len(got) != MaxQueryHistoryEntries || got[0].ID != 2 || got[len(got)-1].ID != MaxQueryHistoryEntries+1 {
		t.Fatalf("bounded query history = first %#v, last %#v, len=%d", got[0], got[len(got)-1], len(got))
	}
}

func TestQueryHistoryWritesMergeWithNewerUnrelatedPreferenceUpdates(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "dbl")
	store, err := OpenAt(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetAppearance("dark", 5); err != nil {
		t.Fatal(err)
	}
	entry := QueryHistoryEntry{ID: 1, SQL: "SELECT 1", Started: time.Now().UTC(), Outcome: "完成"}
	if err := store.SetQueryHistory(true, []QueryHistoryEntry{entry}, 4); err != nil {
		t.Fatal(err)
	}
	settings := store.Settings()
	if settings.Appearance != "dark" || !settings.QueryHistoryEnabled || len(settings.QueryHistory) != 1 {
		t.Fatalf("stale history update overwrote unrelated settings: %#v", settings)
	}
	older := QueryHistoryEntry{ID: 2, SQL: "SELECT 2", Started: time.Now().UTC(), Outcome: "完成"}
	if err := store.SetQueryHistory(true, []QueryHistoryEntry{older}, 3); err != nil {
		t.Fatal(err)
	}
	if got := store.Settings().QueryHistory; len(got) != 1 || got[0].ID != 1 {
		t.Fatalf("older query history update replaced newer history: %#v", got)
	}
}

func TestLayoutPersistsAndReturnsAnIndependentCopy(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "dbl")
	store, err := OpenAt(dir)
	if err != nil {
		t.Fatal(err)
	}
	want := Layout{LeftWidth: 275.5, InspectorWidth: 310, ResultHeight: 420}
	if err := store.SetLayout(want, 1); err != nil {
		t.Fatal(err)
	}
	got := store.Settings()
	if got.Layout == nil || *got.Layout != want {
		t.Fatalf("layout = %#v, want %#v", got.Layout, want)
	}
	got.Layout.LeftWidth = 1
	if store.Settings().Layout.LeftWidth != want.LeftWidth {
		t.Fatal("mutating returned settings changed the store's layout")
	}
	reloaded, err := OpenAt(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := reloaded.Settings().Layout; got == nil || *got != want {
		t.Fatalf("reloaded layout = %#v, want %#v", got, want)
	}
	if err := store.SetLayout(Layout{LeftWidth: 300, InspectorWidth: 250, ResultHeight: 350}, 1); err == nil {
		t.Fatal("stale layout revision was accepted")
	}
}

func TestLayoutRejectsInvalidDimensions(t *testing.T) {
	store, err := OpenAt(filepath.Join(t.TempDir(), "dbl"))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetLayout(Layout{LeftWidth: -1, InspectorWidth: 0, ResultHeight: 5000}, 1); err == nil {
		t.Fatal("layout with no valid dimensions was accepted")
	}
	if err := store.SetLayout(Layout{LeftWidth: float32(math.NaN()), InspectorWidth: float32(math.Inf(1)), ResultHeight: 400}, 1); err != nil {
		t.Fatalf("one valid dimension should allow sanitizing malformed values: %v", err)
	}
	if got := store.Settings().Layout; got == nil || *got != (Layout{ResultHeight: 400}) {
		t.Fatalf("sanitized layout = %#v, want only valid result height", got)
	}
}

func TestPreferenceUpdatesPreserveUnrelatedSettings(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "dbl")
	store, err := OpenAt(dir)
	if err != nil {
		t.Fatal(err)
	}
	profile, err := store.SaveSQLiteProfile(SQLiteProfile{Name: "Local", Path: "/db/local.sqlite"}, 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.RememberSQLitePath("/db/recent.sqlite", 2); err != nil {
		t.Fatal(err)
	}
	if err := store.SetAppearance("dark", 3); err != nil {
		t.Fatal(err)
	}
	layout := Layout{LeftWidth: 240, InspectorWidth: 280, ResultHeight: 360}
	if err := store.SetLayout(layout, 4); err != nil {
		t.Fatal(err)
	}
	reloaded, err := OpenAt(dir)
	if err != nil {
		t.Fatal(err)
	}
	got := reloaded.Settings()
	if got.Appearance != "dark" || got.LastSQLitePath != "/db/recent.sqlite" || got.Layout == nil || *got.Layout != layout {
		t.Fatalf("settings fields did not survive subsequent updates: %#v", got)
	}
	if len(got.SQLiteProfiles) != 1 || got.SQLiteProfiles[0] != profile {
		t.Fatalf("SQLite profile did not survive subsequent updates: %#v", got.SQLiteProfiles)
	}
}

func TestOpenRejectsUnknownAppearance(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte(`{"version":1,"appearance":"sepia"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenAt(dir); err == nil {
		t.Fatal("unknown persisted appearance was accepted")
	}
}

func TestRememberSQLitePathsPersistsRecentListAndPrivatePermissions(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "config", "dbl")
	store, err := OpenAt(dir)
	if err != nil {
		t.Fatal(err)
	}
	for revision, path := range []string{"/db/one.sqlite", "/db/two.sqlite", "/db/one.sqlite"} {
		if err := store.RememberSQLitePath(path, uint64(revision+1)); err != nil {
			t.Fatal(err)
		}
	}

	got := store.Settings()
	wantPaths := []string{"/db/one.sqlite", "/db/two.sqlite"}
	if got.LastSQLitePath != wantPaths[0] || !reflect.DeepEqual(got.RecentSQLitePaths, wantPaths) {
		t.Fatalf("settings = %#v, want most recent paths %#v", got, wantPaths)
	}

	fileInfo, err := os.Stat(filepath.Join(dir, "settings.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" {
		if fileInfo.Mode().Perm() != 0o600 {
			t.Fatalf("settings file mode = %o, want 600", fileInfo.Mode().Perm())
		}
		dirInfo, err := os.Stat(dir)
		if err != nil {
			t.Fatal(err)
		}
		if dirInfo.Mode().Perm() != 0o700 {
			t.Fatalf("settings directory mode = %o, want 700", dirInfo.Mode().Perm())
		}
	}

	reloaded, err := OpenAt(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := reloaded.Settings(); !reflect.DeepEqual(got, store.Settings()) {
		t.Fatalf("reloaded settings = %#v, want %#v", got, store.Settings())
	}
}

func TestOpenRestrictsExistingPreferencesDirectoryAndFile(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "config", "dbl")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte(`{"version":1}`), 0o644); err != nil {
		t.Fatal(err)
	}
	store, err := OpenAt(dir)
	if err != nil {
		t.Fatal(err)
	}
	if store.Settings().Version != 1 {
		t.Fatalf("settings version = %d, want 1", store.Settings().Version)
	}
	if _, err := os.Stat(filepath.Join(dir, "settings.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("legacy JSON file was not removed after migration: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "settings.toml")); err != nil {
		t.Fatalf("migrated TOML file is missing: %v", err)
	}
	if runtime.GOOS != "windows" {
		for _, test := range []struct {
			path string
			want os.FileMode
		}{{dir, 0o700}, {filepath.Join(dir, "settings.toml"), 0o600}} {
			info, err := os.Stat(test.path)
			if err != nil {
				t.Fatal(err)
			}
			if got := info.Mode().Perm(); got != test.want {
				t.Errorf("permissions for %s = %o, want %o", test.path, got, test.want)
			}
		}
	}
}

func TestOpenRestrictsExistingPreferencesDirectoryWithoutSettingsFile(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "config", "dbl")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenAt(dir); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(dir)
		if err != nil {
			t.Fatal(err)
		}
		if got := info.Mode().Perm(); got != 0o700 {
			t.Fatalf("preferences directory permissions = %o, want 700", got)
		}
	}
}

func TestRememberSQLitePathIgnoresStaleRevision(t *testing.T) {
	store, err := OpenAt(filepath.Join(t.TempDir(), "dbl"))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.RememberSQLitePath("/db/new.sqlite", 2); err != nil {
		t.Fatal(err)
	}
	if err := store.RememberSQLitePath("/db/old.sqlite", 1); err != nil {
		t.Fatal(err)
	}
	if got := store.Settings().LastSQLitePath; got != "/db/new.sqlite" {
		t.Fatalf("last path = %q, want latest revision", got)
	}
}

func TestRememberSQLitePathKeepsOnlyTenMostRecent(t *testing.T) {
	store, err := OpenAt(filepath.Join(t.TempDir(), "dbl"))
	if err != nil {
		t.Fatal(err)
	}
	for revision := uint64(1); revision <= 12; revision++ {
		path := fmt.Sprintf("/db/%d.sqlite", revision)
		if err := store.RememberSQLitePath(path, revision); err != nil {
			t.Fatal(err)
		}
	}
	got := store.Settings()
	if len(got.RecentSQLitePaths) != maxRecentSQLitePaths {
		t.Fatalf("recent path count = %d, want %d", len(got.RecentSQLitePaths), maxRecentSQLitePaths)
	}
	if got.LastSQLitePath != "/db/12.sqlite" || got.RecentSQLitePaths[9] != "/db/3.sqlite" {
		t.Fatalf("recent paths have wrong order/range: %#v", got.RecentSQLitePaths)
	}
}

func TestSQLiteProfileCRUDPersistsOnlyConnectionMetadata(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "dbl")
	store, err := OpenAt(dir)
	if err != nil {
		t.Fatal(err)
	}
	profile, err := store.SaveSQLiteProfile(SQLiteProfile{
		Name:     "Local analytics",
		Group:    "Development",
		Path:     "/data/analytics.sqlite",
		ReadOnly: true,
		Favorite: true,
	}, 1)
	if err != nil {
		t.Fatal(err)
	}
	if profile.ID == "" {
		t.Fatal("created profile has no stable ID")
	}
	contents, err := os.ReadFile(filepath.Join(dir, "settings.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(contents), "password") || strings.Contains(string(contents), "secret") {
		t.Fatalf("settings unexpectedly contain credential fields: %s", contents)
	}

	updated, err := store.SaveSQLiteProfile(SQLiteProfile{
		ID:    profile.ID,
		Name:  "Analytics",
		Group: "Work",
		Path:  profile.Path,
	}, 2)
	if err != nil {
		t.Fatal(err)
	}
	if updated.ID != profile.ID || updated.Name != "Analytics" || updated.Group != "Work" || updated.ReadOnly || updated.Favorite {
		t.Fatalf("updated profile = %#v", updated)
	}
	if _, err := store.SaveSQLiteProfile(SQLiteProfile{Name: "analytics", Path: "/other.sqlite"}, 3); err == nil {
		t.Fatal("duplicate profile name was accepted")
	}

	reloaded, err := OpenAt(dir)
	if err != nil {
		t.Fatal(err)
	}
	profiles := reloaded.Settings().SQLiteProfiles
	if len(profiles) != 1 || profiles[0] != updated {
		t.Fatalf("loaded profiles = %#v, want %#v", profiles, updated)
	}
	if err := reloaded.DeleteSQLiteProfile(profile.ID, 3); err != nil {
		t.Fatal(err)
	}
	if got := reloaded.Settings().SQLiteProfiles; len(got) != 0 {
		t.Fatalf("profiles after delete = %#v, want empty", got)
	}
}

func TestSQLiteProfileRejectsInvalidAndStaleUpdates(t *testing.T) {
	store, err := OpenAt(filepath.Join(t.TempDir(), "dbl"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.SaveSQLiteProfile(SQLiteProfile{Name: "Missing path"}, 1); err == nil {
		t.Fatal("profile without path was accepted")
	}
	profile, err := store.SaveSQLiteProfile(SQLiteProfile{Name: "One", Path: "/one.sqlite"}, 2)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.SaveSQLiteProfile(SQLiteProfile{ID: profile.ID, Name: "Old", Path: "/old.sqlite"}, 1); err == nil {
		t.Fatal("stale profile update was accepted")
	}
	if err := store.DeleteSQLiteProfile(profile.ID, 1); err == nil {
		t.Fatal("stale profile delete was accepted")
	}
}

func TestRememberSQLitePathPreservesSavedProfiles(t *testing.T) {
	store, err := OpenAt(filepath.Join(t.TempDir(), "dbl"))
	if err != nil {
		t.Fatal(err)
	}
	profile, err := store.SaveSQLiteProfile(SQLiteProfile{Name: "Saved", Path: "/saved.sqlite"}, 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.RememberSQLitePath("/recent.sqlite", 2); err != nil {
		t.Fatal(err)
	}
	settings := store.Settings()
	if settings.LastSQLitePath != "/recent.sqlite" {
		t.Fatalf("last path = %q, want recent path", settings.LastSQLitePath)
	}
	if len(settings.SQLiteProfiles) != 1 || settings.SQLiteProfiles[0] != profile {
		t.Fatalf("profiles after path update = %#v, want %#v", settings.SQLiteProfiles, profile)
	}
}

func TestSQLiteProfileUpdatePreservesRecentPaths(t *testing.T) {
	store, err := OpenAt(filepath.Join(t.TempDir(), "dbl"))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.RememberSQLitePath("/recent.sqlite", 1); err != nil {
		t.Fatal(err)
	}
	if _, err := store.SaveSQLiteProfile(SQLiteProfile{Name: "Saved", Path: "/saved.sqlite"}, 2); err != nil {
		t.Fatal(err)
	}
	settings := store.Settings()
	if settings.LastSQLitePath != "/recent.sqlite" || len(settings.RecentSQLitePaths) != 1 || settings.RecentSQLitePaths[0] != "/recent.sqlite" {
		t.Fatalf("recent paths after profile save = %#v", settings.RecentSQLitePaths)
	}
}

func TestSQLiteProfileLimit(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "dbl")
	profiles := make([]SQLiteProfile, maxSQLiteProfiles)
	for i := range profiles {
		profiles[i] = SQLiteProfile{
			ID:   fmt.Sprintf("profile-%03d", i),
			Name: fmt.Sprintf("Profile %03d", i),
			Path: fmt.Sprintf("/db/%03d.sqlite", i),
		}
	}
	if err := writeSettings(dir, Settings{Version: 1, SQLiteProfiles: profiles}); err != nil {
		t.Fatal(err)
	}
	store, err := OpenAt(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.SaveSQLiteProfile(SQLiteProfile{Name: "One too many", Path: "/db/extra.sqlite"}, 1); err == nil {
		t.Fatal("profile limit was not enforced")
	}
}

func TestOpenRejectsNonRegularPreferencesFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.toml")
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenAt(dir); err == nil {
		t.Fatal("OpenAt accepted a directory as the preferences file")
	}
}

func TestSettingsTrimsAndDeduplicatesLoadedPaths(t *testing.T) {
	dir := t.TempDir()
	data := []byte(`{"version":1,"recent_sqlite_paths":[" /db/a.sqlite ","/db/b.sqlite","/db/a.sqlite",""]}`)
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := OpenAt(dir)
	if err != nil {
		t.Fatal(err)
	}
	got := store.Settings()
	want := []string{"/db/a.sqlite", "/db/b.sqlite"}
	if got.LastSQLitePath != want[0] || !reflect.DeepEqual(got.RecentSQLitePaths, want) {
		t.Fatalf("settings = %#v, want last path %q and recent %#v", got, want[0], want)
	}
}
