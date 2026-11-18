package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/magalab/dbl/internal/preferences"
	"github.com/magalab/dbl/internal/sqlite"
)

func TestRestoredQueryConnectionSafetyAndWorkspaceLimit(t *testing.T) {
	app := newTestWorkbench()
	app.activeQuery.implicitQuery = true
	app.queryDraftsEnabled = true
	var drafts []preferences.QueryDraft
	for i := 0; i < maxWorkspaces; i++ {
		drafts = append(drafts, preferences.QueryDraft{SQL: "CREATE TABLE unexpected (id INTEGER)", ConnectionID: fmt.Sprintf("postgresql:original-%d", i)})
	}
	app.restoreQueryDrafts(drafts, 0)
	path := filepath.Join(t.TempDir(), "other.db")
	store, err := sqlite.Open(context.Background(), path, sqlite.CreateNew)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	app.store = store
	app.openedConnectionID = "sqlite:" + path
	app.executeSQL()
	if app.status != app.localized("请先连接此 SQL 草稿所属的数据库。") || app.busy {
		t.Fatal("SQL execution did not reject a different active database")
	}
	app.registerLiveConnection(app.openedConnectionID, store, preferences.ConnectionProfile{Driver: preferences.DriverSQLite, Path: path})
	app.activateConnectionWorkspace(app.openedConnectionID)
	if app.workspaceCount() != maxWorkspaces || app.activeQuery.queryConnectionID != "postgresql:original-0" || app.hasActiveConnection() {
		t.Fatal("connecting at the tab limit added a tab or rebound an existing draft")
	}
	if app.status != app.localized("工作区已达上限，请先关闭一个标签后再打开此连接的查询工作区。") {
		t.Fatal("tab limit was not explained")
	}
}

func TestQueryDraftSnapshotAndRestoreKeepOnlySQLAndConnectionReferences(t *testing.T) {
	app := newTestWorkbench()
	app.activeQuery.implicitQuery = true
	app.addWorkspace()
	app.activeQuery.query.Buffer.Set("SELECT '未执行';\n")
	app.activeQuery.queryConnectionID = "postgresql:deleted-profile"
	app.activeQuery.queryNamespace = "reports"
	app.addWorkspace()
	app.activeQuery.query.Buffer.Set("")
	app.activeQuery.queryConnectionID = "sqlite:/missing.db"
	data := newQueryWorkspace(100)
	data.dataView = true
	connection := newQueryWorkspace(101)
	connection.connectionView = true
	app.workspaces = append(app.workspaces, data, connection)
	drafts, active, skipped := app.queryDraftSnapshot()
	if len(drafts) != 2 || active != 1 || skipped != 0 {
		t.Fatalf("snapshot count/active/skipped = %d/%d/%d", len(drafts), active, skipped)
	}
	restored := newTestWorkbench()
	restored.activeQuery.implicitQuery = true
	restored.queryDraftsEnabled = true
	restored.restoreQueryDrafts(drafts, active)
	got, gotActive, _ := restored.queryDraftSnapshot()
	if !reflect.DeepEqual(got, drafts) || gotActive != active || restored.hasActiveConnection() {
		t.Fatalf("restore changed SQL or connected automatically: %#v, %d", got, gotActive)
	}
	// Offline SQL tabs must remain editable even when their profile is missing.
	restored.activateWorkspace(1)
	if restored.activeQuery.queryConnectionID != "postgresql:deleted-profile" {
		t.Fatal("could not activate a disconnected SQL draft")
	}
	if restored.canBindActiveQuery("mysql:other") {
		t.Fatal("opening another connection would rebind a restored draft")
	}
	if !restored.canBindActiveQuery("postgresql:deleted-profile") {
		t.Fatal("original connection could not reclaim its draft")
	}
	restored.removeWorkspace(1)
	got, _, _ = restored.queryDraftSnapshot()
	if len(got) != 1 || got[0].ConnectionID != "sqlite:/missing.db" {
		t.Fatalf("closed draft remained in snapshot: %#v", got)
	}
}

func TestQueryDraftExitFlushCapturesLatestEditsAndFencesPendingSave(t *testing.T) {
	app := newTestWorkbench()
	store, err := preferences.OpenAt(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	app.preferences, app.queryDraftsEnabled = store, true
	app.preferenceRevision = 1
	old, _, _ := app.queryDraftSnapshot()
	app.queryDraftSaveTimer = time.NewTimer(time.Hour)
	app.activeQuery.query.Buffer.Set("SELECT 'latest before close'")
	if err := app.flushQueryDrafts(); err != nil {
		t.Fatal(err)
	}
	if app.queryDraftSaveTimer != nil {
		t.Fatal("exit left pending timer running")
	}
	if err := store.SetQueryDrafts(true, old, 0, 1); err != nil {
		t.Fatal(err)
	}
	if got := store.Settings().QueryDrafts[0].SQL; got != "SELECT 'latest before close'" {
		t.Fatalf("pending save replaced flushed SQL: %q", got)
	}
	app.queryDraftsEnabled, app.queryDraftsSaving = false, true
	if err := app.flushQueryDrafts(); err != nil {
		t.Fatal(err)
	}
	if settings := store.Settings(); settings.QueryDraftsEnabled || len(settings.QueryDrafts) != 0 {
		t.Fatal("exit while disabling restored drafts")
	}
}

func TestQueryDraftFlushReportsStorageFailureAndOversizeIsNotTruncated(t *testing.T) {
	app := newTestWorkbench()
	app.activeQuery.query.Buffer.Set(strings.Repeat("字", preferences.MaxQueryDraftSQLBytes/3+1))
	drafts, _, skipped := app.queryDraftSnapshot()
	if len(drafts) != 0 || skipped != 1 {
		t.Fatal("oversized draft was truncated or saved")
	}
	path := filepath.Join(t.TempDir(), "not-a-directory")
	store, err := preferences.OpenAt(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("blocked"), 0o600); err != nil {
		t.Fatal(err)
	}
	app.preferences, app.queryDraftsEnabled = store, true
	if err := app.flushQueryDrafts(); err == nil {
		t.Fatal("failed exit save was silently accepted")
	}
}

func TestInterfaceSettingsAndDraftsAppearInBothLanguages(t *testing.T) {
	for _, language := range []string{"zh", "en"} {
		app := newTestWorkbench()
		app.language = language
		app.settingsTab = "general"
		tester := ui.NewTester(app.settingsView, 520, 620)
		if !tester.HasText(app.localized("SQL 草稿")) {
			t.Fatal("settings missing draft persistence section")
		}
		if err := tester.Click(app.localized("界面")); err != nil {
			t.Fatal(err)
		}
		tester.Frame()
		for _, label := range []string{"窗口启动方式", "界面字体", "编辑器字体"} {
			if !tester.HasText(app.localized(label)) {
				t.Errorf("%s settings missing %s", language, label)
			}
		}
		if err := tester.Click(app.localized("正常窗口")); err != nil {
			t.Fatal(err)
		}
		tester.Frame()
		if err := tester.Click(app.localized("全屏")); err != nil {
			t.Fatal(err)
		}
		tester.Frame()
		if app.interfaceDraft.StartupMode != "fullscreen" {
			t.Fatal("localized startup option did not update the draft")
		}
	}
}

func TestInterfaceFontAppliesToThemeAndEditor(t *testing.T) {
	app := newTestWorkbench()
	app.interfaceSettings = preferences.InterfaceSettings{FontFamily: "Example Sans", FontSize: 18, EditorFont: "Example Mono", EditorFontSize: 16}
	var family string
	var size float32
	ui.NewTester(func(c *ui.Context) {
		app.applyInterfaceTheme(c)
		family, size = c.Theme().Font, c.Theme().FontSize
	}, 520, 620)
	app.applyEditorFont(app.activeQuery.query)
	if family != "Example Sans" || size != 18 || app.activeQuery.query.Font.Family != "Example Mono" || app.activeQuery.query.Font.Size != 16 {
		t.Fatal("font settings were not applied to UI and editor independently")
	}
	app.interfaceSettings = preferences.InterfaceSettings{}
	app.applyEditorFont(app.activeQuery.query)
	if app.activeQuery.query.Font.Family != "" || app.activeQuery.query.Font.Size != 0 {
		t.Fatal("editor font did not reset to system defaults")
	}
}
