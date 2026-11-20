package app

import (
	"slices"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/plugins/updater"
	"github.com/egoist/mygo/ui"
)

func (app *workbench) updateToolbarButton(c *ui.Context) {
	button := iconButton(c, app.localized("检查更新"), actionIcons["update"])
	if button.Clicked() {
		updater.CheckForUpdates()
	}
}

func (app *workbench) updateSettingsUI(c *ui.Context) {
	app.settingRow(c, "自动检查更新", func() {
		automatic := app.updatesEnabled && updater.AutomaticChecks()
		detail := app.localized("开启后按设定间隔检查更新；发现新版本时显示更新窗口。默认关闭。")
		if ui.Switch(c, &automatic).Label(app.localized("自动检查更新")).Tooltip(detail).
			Disabled(!app.updatesEnabled).Changed() {
			updater.SetAutomaticChecks(automatic)
		}
	})
	if !app.updatesEnabled {
		ui.Text(c, app.localized("应用内更新仅适用于安装在当前用户可写目录的正式版本。")).TextColor(c.Theme().TextMuted)
		return
	}
	app.settingRow(c, "检查间隔", func() {
		intervals := []int{1, 2, 6, 12, 24}
		labels := localizedOptions(app, []string{"1 小时", "2 小时", "6 小时", "12 小时", "24 小时"})
		index := slices.Index(intervals, app.updateIntervalHours)
		if index < 0 {
			index = len(intervals) - 1
		}
		selected := labels[index]
		picker := ui.Select(c, &selected, labels).Width(142).MinWidth(0).Label(app.localized("检查间隔")).
			Tooltip(app.localized("更改后下次启动生效。手动检查可随时进行。")).Disabled(app.updateIntervalSaving)
		if picker.Changed() {
			choice := slices.Index(labels, selected)
			if choice >= 0 && intervals[choice] != app.updateIntervalHours {
				app.saveUpdateInterval(intervals[choice])
			}
		}
	})
	app.settingRow(c, "检查更新", func() {
		if last := updater.LastCheck(); !last.IsZero() {
			ui.Text(c, last.Local().Format("2006-01-02 15:04")).TextColor(c.Theme().TextMuted).
				Tooltip(app.localized("上次检查：") + last.Local().Format("2006-01-02 15:04"))
		}
		if iconButton(c, app.localized("检查更新"), actionIcons["update"]).BorderWidth(0).Clicked() {
			updater.CheckForUpdates()
		}
	})
}

func localizedOptions(app *workbench, labels []string) []string {
	options := make([]string, len(labels))
	for i, label := range labels {
		options[i] = app.localized(label)
	}
	return options
}

func (app *workbench) saveUpdateInterval(hours int) {
	if app.preferences == nil || app.updateIntervalSaving || !validUpdateIntervalHours(hours) {
		return
	}
	store := app.preferences
	app.updateIntervalSaving = true
	app.preferenceRevision++
	revision := app.preferenceRevision
	go func() {
		err := store.SetUpdateCheckInterval(hours, revision)
		mygo.RunOnMain(func() {
			if app.shuttingDown || app.preferences != store {
				return
			}
			app.updateIntervalSaving = false
			if err != nil {
				app.reportError("保存更新检查间隔失败", err)
			} else {
				app.updateIntervalHours = hours
				app.status = app.localized("更新检查间隔已保存。")
			}
			app.invalidateSettingsWindow()
		})
	}()
}

func updateInterval(hours int) time.Duration {
	return time.Duration(hours) * time.Hour
}

func updateDialogStrings() map[string]updater.Strings {
	return map[string]updater.Strings{
		"en": {
			UpToDateMessage:  "\nCurrent version: %[2]s%.0[1]s",
			Available:        "A new version is available!%.0[1]s",
			AvailableMessage: "\nVersion %[2]s is now available. You have %[3]s. Install it now?",
		},
		"zh-Hans": {
			UpToDateMessage:  "\n当前版本：%[2]s%.0[1]s",
			Available:        "有新版本可用！%.0[1]s",
			AvailableMessage: "\n版本 %[2]s 现已推出，你当前的版本是 %[3]s。要现在安装吗？",
		},
	}
}

func validUpdateIntervalHours(hours int) bool {
	switch hours {
	case 1, 2, 6, 12, 24:
		return true
	default:
		return false
	}
}
