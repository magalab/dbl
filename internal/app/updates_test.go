package app

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestUpdateSettingsUIExplainsUnavailableInstallLocation(t *testing.T) {
	app := newTestWorkbench()
	app.language = "en"
	tester := ui.NewTester(app.settingsView, 520, 720)
	if !tester.HasText(app.localized("应用内更新仅适用于安装在当前用户可写目录的正式版本。")) {
		t.Fatal("update UI did not explain why updates are unavailable")
	}
	if tester.HasText(app.localized("检查更新")) {
		t.Fatal("update check action should be hidden when updates are unavailable")
	}
}
