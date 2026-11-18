package app

import (
	"errors"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/magalab/dbl/internal/preferences"
)

func TestInterfaceAutosaveFailureRestoresSavedSettingsAndShowsError(t *testing.T) {
	app := newTestWorkbench()
	app.language = "en"
	app.settingsTab = "interface"
	app.interfaceSettings = preferences.InterfaceSettings{StartupMode: "normal", FontSize: 14}
	app.interfaceDraft = preferences.InterfaceSettings{StartupMode: "fullscreen", FontSize: 20}
	app.interfaceSaving = true

	app.finishInterfaceSettingsSave(app.interfaceDraft, errors.New("save failed"))
	if app.interfaceSaving || app.interfaceDraft != app.interfaceSettings {
		t.Fatal("failed save did not restore the saved settings")
	}
	tester := ui.NewTester(app.settingsView, 800, 800)
	if !tester.HasText("Failed to save interface settings") {
		t.Fatal("settings page did not show the save error")
	}
}

func TestInterfaceSettingsExplainAutosaveAndHaveNoFooterButtons(t *testing.T) {
	app := newTestWorkbench()
	app.settingsTab = "interface"
	tester := ui.NewTester(app.settingsView, 800, 800)
	if !tester.HasText("更改会自动保存") {
		t.Fatal("interface settings do not explain that changes are saved automatically")
	}
	for _, label := range []string{"保存界面设置", "恢复默认界面设置"} {
		if tester.HasText(label) {
			t.Errorf("interface settings still show the footer action %q", label)
		}
	}
}
