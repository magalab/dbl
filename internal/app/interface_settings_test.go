package app

import (
	"errors"
	"fmt"
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

func TestFontSettingsFilterAndAcceptCustomSizes(t *testing.T) {
	app := newTestWorkbench()
	app.language = "en"
	app.settingsTab = "interface"
	app.fontFamilies = []ui.FontFamily{{Name: "Example Sans"}, {Name: "Example Mono", Monospaced: true}}
	tt := ui.NewTester(app.settingsView, 520, 620)
	for label, size := range map[string]float32{"界面字号": ui.LightTheme().FontSize, "编辑器字号": 13} {
		field, ok := tt.AccessByRole(ui.RoleTextField, app.localized(label))
		if !ok || field.Placeholder != fmt.Sprintf("System default (%g)", size) {
			t.Fatalf("%s did not show the default size: %+v", label, field)
		}
	}
	click := func(label string) {
		t.Helper()
		if err := tt.Click(label); err != nil {
			t.Fatal(err)
		}
	}
	focus := func(role ui.Role, label string) {
		t.Helper()
		control, ok := tt.AccessByRole(role, app.localized(label))
		if !ok {
			t.Fatalf("missing control %q", label)
		}
		r := control.Bounds
		tt.ClickAt(r.X+r.W/2, r.Y+r.H/2)
	}
	focus(ui.RoleComboBox, "界面字体")
	tt.Key(ui.Cmd, ui.KeyA)
	tt.Type("sans")
	if _, ok := tt.Find("Example Mono"); ok {
		t.Fatal("font picker did not filter out non-matching families")
	}
	click("Example Sans")
	if app.interfaceDraft.FontFamily != "Example Sans" {
		t.Fatal("filtered font was not selected")
	}
	focus(ui.RoleTextField, "界面字号")
	tt.Type("17")
	tt.Key(0, ui.KeyEnter)
	if app.interfaceDraft.FontSize != 17 {
		t.Fatalf("custom font size = %v", app.interfaceDraft.FontSize)
	}
	tt.Key(ui.Cmd, ui.KeyA)
	tt.Key(0, ui.KeyBackspace)
	tt.Key(0, ui.KeyEnter)
	if app.interfaceDraft.FontSize != 0 {
		t.Fatal("clearing the size did not restore the system default")
	}
	focus(ui.RoleTextField, "编辑器字号")
	tt.Type("17.5")
	tt.Key(0, ui.KeyEnter)
	if app.interfaceDraft.EditorFontSize != 0 || !tt.HasText("Enter a whole number from 10 to 32.") {
		t.Fatal("invalid editor font size was not rejected")
	}
}
