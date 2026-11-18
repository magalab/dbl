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
		Title:         app.localized("设置"),
		Width:         520,
		Height:        620,
		MinWidth:      480,
		MinHeight:     460,
		Parent:        app.window,
		StateKey:      "dbl-settings",
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
	app.applyInterfaceTheme(c)
	bar := c.TitleBar()
	ui.Column(c).Fill().Children(func() {
		ui.Row(c).FillWidth().Height(max(bar.Height, c.Theme().FontSize+18)).Padding(0, bar.Right+12, 0, bar.Left+12).AlignItems(ui.Center).DragWindow().Children(func() {
			ui.Text(c, app.localized("设置")).Bold().SingleLine()
		})
		ui.ScrollHorizontal(c).FillWidth().Height(c.Theme().FontSize + 32).Shrink(0).Children(func() {
			ui.Row(c).Padding(4, 14).Gap(8).Shrink(0).Children(func() {
				for _, tab := range []struct{ id, label string }{
					{"general", "常规"}, {"interface", "界面"}, {"shortcuts", "快捷键"}, {"ai", "AI 助手"},
				} {
					button := ui.Button(c, app.localized(tab.label)).Role(ui.RoleTab).Selected(app.settingsTab == tab.id).Shrink(0)
					if button.Clicked() {
						app.settingsTab = tab.id
					}
				}
			})
		})
		ui.Scroll(c).Grow(1).Children(func() {
			ui.Column(c).Padding(14).Gap(8).Children(func() {
				theme := c.Theme()
				if app.settingsTab == "interface" {
					app.interfaceSettingsUI(c)
					return
				}
				if app.settingsTab == "ai" {
					ui.Column(c).Padding(10).Gap(6).Background(theme.Surface).Border(1, theme.Border).Radius(theme.Radius).Children(func() {
						app.aiProviderSettings(c)
					})
					return
				}
				if app.settingsTab == "shortcuts" {
					ui.Column(c).Padding(14).Gap(12).Background(theme.Surface).Border(1, theme.Border).Radius(theme.Radius).Children(func() {
						ui.Text(c, app.localized("应用操作")).Bold().Role(ui.RoleHeading).Level(2)
						ui.Grid(c).Columns(2).GapX(20).GapY(10).Children(func() {
							app.settingsShortcut(c, "新建连接…", "N")
							app.settingsShortcut(c, "打开 SQLite 文件…", "O")
							app.settingsShortcut(c, "新建查询工作区", "T")
							app.settingsShortcut(c, "执行 SQL", "Enter")
							app.settingsShortcut(c, "打开设置…", ",")
						})
					})
					ui.Column(c).Padding(14).Gap(12).Background(theme.Surface).Border(1, theme.Border).Radius(theme.Radius).Children(func() {
						ui.Text(c, app.localized("面板快捷键")).Bold().Role(ui.RoleHeading).Level(2)
						ui.Grid(c).Columns(2).GapX(20).GapY(10).Children(func() {
							app.settingsShortcut(c, "左侧边栏", "B")
							app.settingsShortcut(c, "AI 助手", "Shift + B")
						})
					})
					return
				}

				ui.Column(c).Padding(14).Gap(12).Background(theme.Surface).Border(1, theme.Border).Radius(theme.Radius).Children(func() {
					ui.Text(c, app.localized("外观")).Bold().Role(ui.RoleHeading).Level(2)
					ui.Row(c).Gap(8).Children(func() {
						ui.Text(c, app.localized("当前：")+app.localized(appearanceLabel(app.appearance))).Grow(1).TextColor(theme.TextMuted)
						for _, option := range []struct {
							value, label, icon string
						}{
							{"system", "外观：跟随系统", "system"},
							{"light", "外观：浅色", "sun"},
							{"dark", "外观：深色", "moon"},
						} {
							appearance := iconButton(c, app.localized(option.label), actionIcons[option.icon])
							appearance.Selected(app.appearance == option.value)
							appearance.Disabled(app.busy || app.exporting || app.dialogOpen || app.preferences == nil)
							if appearance.Clicked() {
								app.setAppearance(option.value)
							}
						}
					})
					ui.Row(c).Gap(8).Children(func() {
						ui.Text(c, app.localized("语言")).Grow(1)
						languageLabel, nextLanguage := "切换语言 · English", "en"
						if app.language == "en" {
							languageLabel, nextLanguage = "切换语言 · 中文", "zh"
						}
						language := iconButton(c, app.localized(languageLabel), actionIcons["languages"])
						language.Disabled(app.busy || app.exporting || app.dialogOpen || app.preferences == nil)
						if language.Clicked() {
							app.setLanguage(nextLanguage)
						}
					})
				})

				ui.Column(c).Padding(14).Gap(8).Background(theme.Surface).Border(1, theme.Border).Radius(theme.Radius).Children(func() {
					ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
						ui.Text(c, app.localized("SQL 草稿")).Bold().Role(ui.RoleHeading).Level(2).Grow(1)
						info := iconButton(c, app.localized("SQL 草稿保存说明"), actionIcons["info"])
						info.Size(26, 26).Padding(3).Tooltip(app.localized("SQL 草稿保存详情"))
					})
					enabled := app.queryDraftsEnabled
					drafts := ui.Checkbox(c, &enabled, app.localized("自动保存并恢复 SQL 草稿"))
					drafts.Disabled(app.queryDraftsSaving || app.preferences == nil)
					if drafts.Changed() {
						app.setQueryDraftsEnabled(enabled)
					}
					ui.Text(c, app.localized("草稿仅保存在本机；重启后不会自动连接或执行。")).TextColor(theme.TextMuted)
				})

				app.updateSettingsUI(c)

				ui.Column(c).Padding(14).Gap(8).Background(theme.Surface).Border(1, theme.Border).Radius(theme.Radius).Children(func() {
					ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
						ui.Text(c, app.localized("查询历史")).Bold().Role(ui.RoleHeading).Level(2).Grow(1)
						info := iconButton(c, app.localized("查询历史保存说明"), actionIcons["info"])
						info.Size(26, 26).Padding(3).Tooltip(app.localized("默认关闭。开启后会把 SQL 文本、时间、行数和状态写入本机设置文件；SQL 可能包含敏感值。关闭会删除已保存历史。"))
					})
					history := ui.Checkbox(c, &app.queryHistoryEnabled, app.localized("将最近查询 SQL 保存在本机"))
					history.Disabled(app.busy || app.exporting || app.dialogOpen || app.queryHistorySaving || app.cursor != nil || app.preferences == nil)
					if history.Changed() {
						app.setQueryHistoryEnabled(app.queryHistoryEnabled)
					}
					ui.Text(c, app.localized("默认仅保留在本次运行；开启后保存到本机。")).TextColor(theme.TextMuted)
				})
			})
		})
	})
}

func (app *workbench) settingsShortcut(c *ui.Context, label, keys string) {
	ui.Row(c).Gap(12).Children(func() {
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
	endpoint := ui.TextInput(c, &app.aiEndpoint).Placeholder(app.localized("https://provider.example/v1/chat/completions")).Label(app.localized("HTTPS endpoint"))
	endpoint.Disabled(app.aiRunning || app.aiPrepared || app.busy || app.exporting || app.dialogOpen)
	app.aiModelSelector(c)
	previousKey := app.aiAPIKey
	key := ui.TextInput(c, &app.aiAPIKey).Placeholder(app.localized("留空保留已保存 key")).Password().Label(app.localized("API key（仅存入本机加密凭据库）"))
	key.Disabled(app.aiRunning || app.aiPrepared || app.busy || app.exporting || app.dialogOpen || app.aiRemoveKey)
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
		remove := ui.Checkbox(c, &app.aiRemoveKey, app.localized("保存时移除已保存 API key"))
		remove.Disabled(app.aiRunning || app.aiPrepared || app.busy || app.exporting || app.dialogOpen || app.aiEnabled || app.aiAPIKey != "")

	}
	enabled := ui.Checkbox(c, &app.aiEnabled, app.localized("启用 AI（请求会发送到上方 provider）"))
	enabled.Disabled(app.aiRunning || app.aiPrepared || app.busy || app.exporting || app.dialogOpen)

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
