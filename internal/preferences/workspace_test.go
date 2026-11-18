package preferences

import (
	"math"
	"reflect"
	"strings"
	"testing"
)

func TestQueryDraftsOptInRoundTripAndDisableFencesPendingSave(t *testing.T) {
	dir := t.TempDir()
	store, err := OpenAt(dir)
	if err != nil {
		t.Fatal(err)
	}
	if settings := store.Settings(); settings.QueryDraftsEnabled || len(settings.QueryDrafts) != 0 {
		t.Fatal("draft persistence must default to off")
	}
	drafts := []QueryDraft{
		{Title: "查询 2", SQL: "SELECT '中文';\n", ConnectionID: "postgresql:profile-one", Namespace: "reporting"},
		{Title: "查询 3", SQL: "", ConnectionID: "sqlite:/missing/file.db"},
	}
	if err := store.SetQueryDrafts(true, drafts, 1, 2); err != nil {
		t.Fatal(err)
	}
	drafts[0].SQL = "caller mutation"
	reloaded, err := OpenAt(dir)
	if err != nil {
		t.Fatal(err)
	}
	settings := reloaded.Settings()
	if !settings.QueryDraftsEnabled || settings.ActiveQueryDraft != 1 || settings.QueryDrafts[0].SQL != "SELECT '中文';\n" || settings.QueryDrafts[1].SQL != "" {
		t.Fatalf("incorrect restored drafts: %#v", settings)
	}
	settings.QueryDrafts[0].SQL = "snapshot mutation"
	if reloaded.Settings().QueryDrafts[0].SQL == "snapshot mutation" {
		t.Fatal("Settings returned mutable draft backing storage")
	}
	if err := store.SetQueryDrafts(false, nil, 0, 4); err != nil {
		t.Fatal(err)
	}
	if err := store.SetQueryDrafts(true, drafts, 1, 3); err != nil {
		t.Fatal(err)
	}
	reloaded, err = OpenAt(dir)
	if err != nil {
		t.Fatal(err)
	}
	if settings := reloaded.Settings(); settings.QueryDraftsEnabled || len(settings.QueryDrafts) != 0 {
		t.Fatal("late save resurrected cleared drafts")
	}
}

func TestWorkspacePreferencesMergeUnrelatedOutOfOrderUpdates(t *testing.T) {
	store, err := OpenAt(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetAppearance("dark", 10); err != nil {
		t.Fatal(err)
	}
	drafts := []QueryDraft{{SQL: "SELECT 1"}}
	if err := store.SetQueryDrafts(true, drafts, 0, 3); err != nil {
		t.Fatal(err)
	}
	settings := InterfaceSettings{StartupMode: "fullscreen", CloseToTray: true, FontFamily: "Example Sans", FontSize: 18, EditorFont: "Example Mono", EditorFontSize: 16}
	if err := store.SetInterface(settings, 4); err != nil {
		t.Fatal(err)
	}
	if err := store.SetInterface(InterfaceSettings{StartupMode: "normal"}, 2); err != nil {
		t.Fatal(err)
	}
	if err := store.SetLanguage("en", 11); err != nil {
		t.Fatal(err)
	}
	reloaded, err := OpenAt(store.dir)
	if err != nil {
		t.Fatal(err)
	}
	got := reloaded.Settings()
	if got.Appearance != "dark" || got.Language != "en" || got.Interface != settings || !reflect.DeepEqual(got.QueryDrafts, drafts) {
		t.Fatalf("unrelated preferences lost: %#v", got)
	}
}

func TestUpdateCheckIntervalDefaultsValidatesAndPersists(t *testing.T) {
	dir := t.TempDir()
	store, err := OpenAt(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := store.Settings().UpdateCheckIntervalHours; got != 24 {
		t.Fatalf("default update interval = %d, want 24", got)
	}
	if err := store.SetUpdateCheckInterval(12, 2); err != nil {
		t.Fatal(err)
	}
	if err := store.SetUpdateCheckInterval(6, 1); err != nil {
		t.Fatal(err)
	}
	reloaded, err := OpenAt(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := reloaded.Settings().UpdateCheckIntervalHours; got != 12 {
		t.Fatalf("persisted update interval = %d, want 12", got)
	}
	for _, hours := range []int{0, 3, 48, 168} {
		if err := store.SetUpdateCheckInterval(hours, 3); err == nil {
			t.Errorf("unsupported update interval %d accepted", hours)
		}
	}
}

func TestLegacyUpdateCheckIntervalNormalizesToOneDay(t *testing.T) {
	dir := t.TempDir()
	if err := writeSettings(dir, Settings{Version: 1, Interface: InterfaceSettings{StartupMode: "normal"}, UpdateCheckIntervalHours: 168}); err != nil {
		t.Fatal(err)
	}
	store, err := OpenAt(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := store.Settings().UpdateCheckIntervalHours; got != 24 {
		t.Fatalf("legacy update interval = %d, want 24", got)
	}
}

func TestWorkspacePreferencesRejectOversizeAndSanitizeLoadedValues(t *testing.T) {
	store, err := OpenAt(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetQueryDrafts(true, []QueryDraft{{SQL: strings.Repeat("x", MaxQueryDraftSQLBytes+1)}}, 0, 1); err == nil {
		t.Fatal("oversized SQL accepted")
	}
	if err := store.SetQueryDrafts(true, make([]QueryDraft, MaxQueryDrafts+1), 0, 1); err == nil {
		t.Fatal("too many drafts accepted")
	}
	if err := store.SetQueryDrafts(true, []QueryDraft{{}}, 1, 1); err == nil {
		t.Fatal("invalid active index accepted")
	}
	for _, invalid := range []InterfaceSettings{
		{StartupMode: "invalid"}, {StartupMode: "normal", FontSize: 100},
		{StartupMode: "normal", EditorFontSize: float32(math.NaN())},
		{StartupMode: "normal", FontFamily: "bad\nfont"},
	} {
		if err := store.SetInterface(invalid, 1); err == nil {
			t.Errorf("invalid interface settings accepted: %#v", invalid)
		}
	}
	settings := normalizeWorkspaceSettings(Settings{
		QueryDraftsEnabled: true, ActiveQueryDraft: 1,
		QueryDrafts: []QueryDraft{{SQL: strings.Repeat("x", MaxQueryDraftSQLBytes+1)}, {SQL: "SELECT 2"}},
		Interface:   InterfaceSettings{StartupMode: "invalid", FontSize: float32(math.Inf(1))},
	})
	if settings.Interface.StartupMode != "normal" || settings.Interface.FontSize != 0 || len(settings.QueryDrafts) != 1 || settings.ActiveQueryDraft != 0 {
		t.Fatalf("loaded settings were not sanitized: %#v", settings)
	}
}
