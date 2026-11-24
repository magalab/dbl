package app

import (
	"runtime"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

func (app *workbench) openSettings() {
	if app.window == nil {
		return
	}
	if app.settingsWindow != nil {
		app.settingsWindow.Show()
		app.settingsWindow.Focus()
		return
	}
	app.interfaceDraft = app.interfaceSettings
	if app.fontFamilies == nil {
		app.fontFamilies = ui.FontFamilies()
	}
	win := mygo.NewWindow(mygo.WindowOptions{
		Title:     app.localized("设置"),
		Width:     520,
		Height:    500,
		MinWidth:  480,
		MinHeight: 420,
		Parent:    app.window,
		// Reset the old oversized settings-window frame once; subsequent
		// user resizing is remembered under this key.
		StateKey:      "dbl-settings-v2",
		TitleBarStyle: mygo.TitleBarHiddenInset,
		Content:       ui.View(app.settingsView),
	})
	app.settingsWindow = win
	win.OnClosed(func() {
		if app.settingsWindow == win {
			app.settingsWindow = nil
		}
	})
	win.Show()
	win.Focus()
}

func (app *workbench) openAISettings() {
	app.settingsTab = "ai"
	if app.settingsWindow != nil {
		app.settingsWindow.Invalidate()
	}
	app.openSettings()
}

func (app *workbench) invalidateSettingsWindow() {
	if app.settingsWindow != nil {
		app.settingsWindow.Invalidate()
	}
}

func (app *workbench) settingsView(c *ui.Context) {
	if c.Shortcut(0, ui.KeyEscape) || c.Shortcut(ui.Cmd, ui.KeyW) {
		if app.settingsWindow != nil {
			app.settingsWindow.Close()
		}
	}
	defaultFontSize := c.Theme().FontSize
	app.applyInterfaceTheme(c)
	bar := c.TitleBar()
	ui.Column(c).Fill().Children(func() {
		ui.Row(c).FillWidth().Height(max(bar.Height, c.Theme().FontSize+18)).Padding(0, bar.Right+12, 0, bar.Left+12).Gap(8).AlignItems(ui.Center).DragWindow().Children(func() {
			for _, tab := range [...]struct{ id, label, icon string }{
				{"general", "常规", "settings"},
				{"interface", "界面", "win-maximize"},
				{"shortcuts", "快捷键", "keyboard"},
				{"ai", "AI 助手", "sparkle"},
			} {
				button := iconButton(c, app.localized(tab.label), actionIcons[tab.icon]).
					Size(34, 30).Padding(6).Role(ui.RoleTab).Selected(app.settingsTab == tab.id).Shrink(0)
				if button.Clicked() {
					app.settingsTab = tab.id
				}
			}
		})
		ui.Scroll(c).Grow(1).Children(func() {
			ui.Column(c).FillWidth().Padding(14).Gap(10).Children(func() {
				switch app.settingsTab {
				case "interface":
					app.interfaceSettingsUI(c, defaultFontSize)
				case "ai":
					app.aiProviderSettings(c)
				case "shortcuts":
					app.settingsShortcut(c, "新建连接…", "N")
					app.settingsShortcut(c, "打开 SQLite 文件…", "O")
					app.settingsShortcut(c, "新建查询工作区", "T")
					app.settingsShortcut(c, "执行 SQL", "Enter")
					app.settingsShortcut(c, "打开设置…", ",")
					app.settingsShortcut(c, "左侧边栏", "B")
					app.settingsShortcut(c, "AI 助手", "Shift + B")
				default:
					app.generalSettings(c)
				}
			})
		})
	})
}

func (app *workbench) settingRow(c *ui.Context, label string, controls func()) {
	ui.Row(c).FillWidth().MinHeight(32).Gap(10).AlignItems(ui.Center).Children(func() {
		ui.Text(c, app.localized(label)).Grow(1).MinWidth(0)
		controls()
	})
}

func (app *workbench) generalSettings(c *ui.Context) {
	app.settingRow(c, "外观", func() {
		for _, option := range []struct{ value, label, icon string }{
			{"system", "外观：跟随系统", "system"},
			{"light", "外观：浅色", "sun"},
			{"dark", "外观：深色", "moon"},
		} {
			button := iconButton(c, app.localized(option.label), actionIcons[option.icon])
			button.Selected(app.appearance == option.value)
			button.Disabled(app.busy || app.exporting || app.dialogOpen || app.preferences == nil)
			if button.Clicked() {
				app.setAppearance(option.value)
			}
		}
	})
	app.settingRow(c, "语言", func() {
		label, current, next := "切换语言 · English", "简体中文", "en"
		if app.language == "en" {
			label, current, next = "切换语言 · 中文", "English", "zh"
		}
		ui.Text(c, current).TextColor(c.Theme().TextMuted)
		button := iconButton(c, app.localized(label), actionIcons["languages"]).BorderWidth(0)
		button.Disabled(app.busy || app.exporting || app.dialogOpen || app.preferences == nil)
		if button.Clicked() {
			app.setLanguage(next)
		}
	})
	app.settingRow(c, "SQL 草稿", func() {
		enabled := app.queryDraftsEnabled
		if ui.Switch(c, &enabled).Label(app.localized("自动保存并恢复 SQL 草稿")).
			Tooltip(app.localized("SQL 草稿保存详情")).Disabled(app.queryDraftsSaving || app.preferences == nil).Changed() {
			app.setQueryDraftsEnabled(enabled)
		}
	})
	app.settingRow(c, "查询历史", func() {
		detail := app.localized("默认关闭。开启后会把 SQL 文本、时间、行数和状态写入本机设置文件；SQL 可能包含敏感值。关闭会删除已保存历史。")
		if ui.Switch(c, &app.queryHistoryEnabled).Label(app.localized("将最近查询 SQL 保存在本机")).Tooltip(detail).
			Disabled(app.busy || app.exporting || app.dialogOpen || app.queryHistorySaving || app.cursor != nil || app.preferences == nil).Changed() {
			app.setQueryHistoryEnabled(app.queryHistoryEnabled)
		}
	})
	app.settingRow(c, "表格写入", func() {
		enabled := !app.skipTableChangeConfirmation
		detail := app.localized("提交前核对目标、事务和变更预览；关闭后点击提交修改将直接写入数据库。默认开启。")
		if ui.Switch(c, &enabled).Label(app.localized("提交表格修改前确认")).Tooltip(detail).
			Disabled(app.tableChangeConfirmationSaving || app.preferences == nil).Changed() {
			app.setTableChangeConfirmationEnabled(enabled)
		}
	})
	app.updateSettingsUI(c)
}

func (app *workbench) settingsShortcut(c *ui.Context, label, keys string) {
	ui.Row(c).FillWidth().MinHeight(28).Gap(12).AlignItems(ui.Center).Children(func() {
		modifier := "Ctrl"
		if runtime.GOOS == "darwin" {
			modifier = "⌘"
		}
		ui.Text(c, app.localized(label)).Grow(1).SingleLine()
		ui.Text(c, modifier+" + "+keys).TextColor(c.Theme().TextMuted).SingleLine()
	})
}

func (app *workbench) aiProviderSettings(c *ui.Context) {
	app.aiProviderPicker(c, true)
	app.settingRow(c, "HTTPS endpoint", func() {
		ui.TextInput(c, &app.aiEndpoint).Width(260).MinWidth(0).
			Placeholder(app.localized("https://provider.example/v1/chat/completions")).Label(app.localized("HTTPS endpoint")).
			Disabled(app.aiRunning || app.aiPrepared || app.busy || app.exporting || app.dialogOpen)
	})
	app.aiModelSelector(c)
	previousKey := app.aiAPIKey
	app.settingRow(c, "API key", func() {
		ui.TextInput(c, &app.aiAPIKey).Width(260).MinWidth(0).Placeholder(app.localized("留空保留已保存 key")).Password().
			Label(app.localized("API key（仅存入本机加密凭据库）")).
			Disabled(app.aiRunning || app.aiPrepared || app.busy || app.exporting || app.dialogOpen || app.aiRemoveKey)
	})
	if previousKey != app.aiAPIKey {
		delete(app.aiModelCache, app.aiModelCacheKey())
	}
	if app.aiAPIKey != "" {
		clearKey := ui.Button(c, app.localized("清除未保存 API key"))
		clearKey.Disabled(app.aiRunning || app.aiPrepared || app.busy || app.exporting || app.dialogOpen)
		if clearKey.Clicked() {
			app.aiAPIKey = ""
			app.status = app.localized("未保存的 AI API key 已清除。 ")
		}
	}
	if app.aiSettings.APIKeyRef != "" {
		ui.Text(c, app.localized("已保存 API key 将保留；配置文件只保存引用。")).TextColor(c.Theme().TextMuted)
		app.settingRow(c, "保存时移除已保存 API key", func() {
			ui.Switch(c, &app.aiRemoveKey).Label(app.localized("保存时移除已保存 API key")).
				Disabled(app.aiRunning || app.aiPrepared || app.busy || app.exporting || app.dialogOpen || app.aiEnabled || app.aiAPIKey != "")
		})

	}
	app.settingRow(c, "启用 AI", func() {
		detail := app.localized("启用 AI（请求会发送到上方 provider）")
		ui.Switch(c, &app.aiEnabled).Label(detail).Tooltip(detail).
			Disabled(app.aiRunning || app.aiPrepared || app.busy || app.exporting || app.dialogOpen)
	})

	save := ui.PrimaryButton(c, app.localized("保存 AI 设置"))
	save.Disabled(app.aiRunning || app.aiPrepared || app.busy || app.exporting || app.dialogOpen || app.preferences == nil)
	if save.Clicked() {
		app.saveAISettings()
	}
}

func setAppearanceSource(appearance string) {
	switch appearance {
	case "light":
		mygo.Theme.SetSource(mygo.ThemeLight)
	case "dark":
		mygo.Theme.SetSource(mygo.ThemeDark)
	default:
		mygo.Theme.SetSource(mygo.ThemeSystem)
	}
}
