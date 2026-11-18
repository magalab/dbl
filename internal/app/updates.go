package app

import (
	"slices"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/plugins/updater"
	"github.com/egoist/mygo/ui"
)

func (app *workbench) updateToolbarButton(c *ui.Context) {
	if !app.updatesEnabled {
		return
	}
	button := iconButton(c, app.localized("检查更新"), actionIcons["refresh"])
	if button.Clicked() {
		updater.CheckForUpdates()
	}
}

func (app *workbench) updateSettingsUI(c *ui.Context) {
	theme := c.Theme()
	ui.Column(c).Padding(12).Gap(8).Background(theme.Surface).Border(1, theme.Border).Radius(theme.Radius).Children(func() {
		ui.Text(c, app.localized("应用更新")).Bold().Role(ui.RoleHeading).Level(2)
		if !app.updatesEnabled {
			ui.Text(c, app.localized("应用内更新仅适用于安装在当前用户可写目录的正式版本。")).TextColor(theme.TextMuted)
			return
		}
		automatic := updater.AutomaticChecks()
		check := ui.Checkbox(c, &automatic, app.localized("自动检查更新"))
		if check.Changed() {
			updater.SetAutomaticChecks(automatic)
		}
		ui.Text(c, app.localized("开启后按设定间隔检查更新；发现新版本时显示更新窗口。默认关闭。")).FillWidth().TextColor(theme.TextMuted)

		intervals := []int{6, 12, 24, 48, 168}
		labels := []string{"6 小时", "12 小时", "24 小时", "48 小时", "7 天"}
		index := slices.Index(intervals, app.updateIntervalHours)
		if index < 0 {
			index = 2
		}
		selected := app.localized(labels[index])
		ui.Row(c).FillWidth().Gap(12).AlignItems(ui.Center).Children(func() {
			ui.Column(c).Grow(1).MinWidth(0).Gap(2).Children(func() {
				ui.Text(c, app.localized("检查间隔")).Bold()
				ui.Text(c, app.localized("更改后下次启动生效。手动检查可随时进行。")).FillWidth().TextColor(theme.TextMuted)
			})
			picker := ui.Select(c, &selected, localizedOptions(app, labels)).Width(142).MinWidth(0).Label(app.localized("检查间隔"))
			picker.Disabled(app.updateIntervalSaving)
			if picker.Changed() {
				choice := slices.IndexFunc(labels, func(label string) bool { return app.localized(label) == selected })
				if choice >= 0 && intervals[choice] != app.updateIntervalHours {
					app.saveUpdateInterval(intervals[choice])
				}
			}
		})
		if last := updater.LastCheck(); !last.IsZero() {
			ui.Text(c, app.localized("上次检查：")+last.Local().Format("2006-01-02 15:04")).TextColor(theme.TextMuted)
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
	if app.preferences == nil || app.updateIntervalSaving || hours < 1 || hours > 168 {
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
