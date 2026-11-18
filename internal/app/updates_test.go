package app

import (
	"fmt"
	"strings"
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

func TestUpdateDialogAvailableTextOmitsAppNameAndAddsSpacing(t *testing.T) {
	for language, texts := range updateDialogStrings() {
		upToDate := fmt.Sprintf(texts.UpToDateMessage, "DBL", "0.1.4")
		if strings.Contains(upToDate, "DBL") || strings.Contains(upToDate, "%!") || !strings.Contains(upToDate, "0.1.4") {
			t.Errorf("%s up-to-date message should show only the current version and format correctly: %q", language, upToDate)
		}
		if language == "zh-Hans" && upToDate != "\n当前版本：0.1.4" {
			t.Errorf("unexpected Chinese up-to-date message: %q", upToDate)
		}
		if language == "en" && upToDate != "\nCurrent version: 0.1.4" {
			t.Errorf("unexpected English up-to-date message: %q", upToDate)
		}
		available := fmt.Sprintf(texts.Available, "DBL")
		if strings.Contains(available, "DBL") || strings.Contains(texts.AvailableMessage, "DBL") {
			t.Errorf("%s updater text repeats the app name", language)
		}
		if strings.Contains(available, "%!") {
			t.Errorf("%s updater title has invalid format: %q", language, available)
		}
		if !strings.HasPrefix(texts.AvailableMessage, "\n") {
			t.Errorf("%s updater message has no spacing before the version", language)
		}
		message := fmt.Sprintf(texts.AvailableMessage, "DBL", "0.1.4", "0.1.3")
		if strings.Contains(message, "%!") {
			t.Errorf("%s updater message has invalid format: %q", language, message)
		}
	}
}
