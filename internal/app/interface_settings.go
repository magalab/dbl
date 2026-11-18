package app

import (
	"slices"
	"strconv"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
	"github.com/magalab/dbl/internal/preferences"
)

func (app *workbench) applyInterfaceTheme(c *ui.Context) {
	if app.interfaceSettings.FontFamily == "" && app.interfaceSettings.FontSize == 0 {
		return
	}
	theme := *c.Theme()
	if app.interfaceSettings.FontFamily != "" {
		theme.Font = app.interfaceSettings.FontFamily
	}
	if app.interfaceSettings.FontSize != 0 {
		theme.FontSize = app.interfaceSettings.FontSize
	}
	c.SetTheme(&theme)
}

func (app *workbench) applyEditorFont(state *editor.State) {
	state.Font.Family = app.interfaceSettings.EditorFont
	state.Font.Size = app.interfaceSettings.EditorFontSize
}

func (app *workbench) interfaceSettingsUI(c *ui.Context) {
	theme := c.Theme()
	ui.Column(c).Padding(14).Gap(12).Background(theme.Surface).Border(1, theme.Border).Radius(theme.Radius).Children(func() {
		ui.Text(c, app.localized("窗口启动方式")).Bold().Role(ui.RoleHeading).Level(2)
		modes := []string{"normal", "maximized", "fullscreen"}
		labels := []string{app.localized("正常窗口"), app.localized("最大化"), app.localized("全屏")}
		index := slices.Index(modes, app.interfaceDraft.StartupMode)
		selected := labels[max(0, index)]
		mode := ui.Select(c, &selected, labels).FillWidth().Label(app.localized("窗口启动方式"))
		mode.Disabled(app.interfaceSaving)
		if mode.Changed() {
			app.interfaceDraft.StartupMode = modes[slices.Index(labels, selected)]
		}
		ui.Text(c, app.localized("窗口启动方式在下次打开应用时生效。")).TextColor(theme.TextMuted)
	})
	ui.Column(c).Padding(14).Gap(12).Background(theme.Surface).Border(1, theme.Border).Radius(theme.Radius).Children(func() {
		ui.Text(c, app.localized("字体")).Bold().Role(ui.RoleHeading).Level(2)
		app.fontSettingsGroup(c, false)
		app.fontSettingsGroup(c, true)
		ui.Text(c, app.localized("字体保存后即时生效；缺失字体由系统回退。编辑器字体用于 SQL 和 JSON 编辑器。")).TextColor(theme.TextMuted)
	})
	ui.Row(c).Gap(8).Children(func() {
		reset := iconButton(c, app.localized("恢复默认界面设置"), actionIcons["refresh"])
		reset.Disabled(app.interfaceSaving)
		if reset.Clicked() {
			app.interfaceDraft = preferences.InterfaceSettings{StartupMode: "normal"}
		}
		ui.Spacer(c)
		save := iconPrimaryButton(c, app.localized("保存界面设置"), actionIcons["save"])
		save.Disabled(app.interfaceSaving || app.preferences == nil || app.interfaceDraft == app.interfaceSettings)
		if save.Clicked() {
			app.saveInterfaceSettings()
		}
	})
}

func (app *workbench) fontSettingsGroup(c *ui.Context, code bool) {
	fontLabel, sizeLabel := "界面字体", "界面字号"
	family, size := &app.interfaceDraft.FontFamily, &app.interfaceDraft.FontSize
	preview, previewLabel := "DBL 数据工作台 · Aa 0123", "界面字体预览"
	if code {
		fontLabel, sizeLabel = "编辑器字体", "编辑器字号"
		family, size = &app.interfaceDraft.EditorFont, &app.interfaceDraft.EditorFontSize
		preview, previewLabel = "SELECT 名称, id FROM users;", "编辑器字体预览"
	}
	theme := c.Theme()
	sizeWidth := max(float32(144), theme.FontSize*10+20)
	width, _ := c.Size()
	// Account for the page and card padding. Stack both fields when larger
	// text would leave too little room for a readable font-family picker.
	sideBySide := width-56 >= max(float32(200), theme.FontSize*12)+sizeWidth+12
	fields := func() {
		fontField := ui.Column(c).Gap(6).MinWidth(0)
		if sideBySide {
			fontField.Grow(1)
		} else {
			fontField.FillWidth()
		}
		fontField.Children(func() { app.fontPreference(c, fontLabel, family, code) })
		sizeField := ui.Column(c).Gap(6)
		if sideBySide {
			sizeField.Width(sizeWidth).Shrink(0)
		} else {
			sizeField.FillWidth()
		}
		sizeField.Children(func() { app.fontSizePreference(c, sizeLabel, size) })
	}
	ui.Column(c).FillWidth().Gap(8).Children(func() {
		if sideBySide {
			ui.Row(c).FillWidth().Gap(12).Children(fields)
		} else {
			ui.Column(c).FillWidth().Gap(10).Children(fields)
		}
		font := *family
		if code && font == "" {
			font = "monospace"
		}
		ui.Column(c).FillWidth().Padding(8, 10).Background(theme.Background).Radius(theme.Radius).Children(func() {
			ui.Text(c, app.localized(preview)).Font(font).FontSize(app.previewFontSize(code, theme.FontSize)).
				FillWidth().SingleLine().Ellipsis("…").Label(app.localized(previewLabel))
		})
	})
}

func (app *workbench) previewFontSize(code bool, defaultSize float32) float32 {
	if code {
		if app.interfaceDraft.EditorFontSize != 0 {
			return app.interfaceDraft.EditorFontSize
		}
		return 13
	}
	if app.interfaceDraft.FontSize != 0 {
		return app.interfaceDraft.FontSize
	}
	return defaultSize
}

func (app *workbench) fontPreference(c *ui.Context, label string, value *string, code bool) {
	defaultLabel := app.localized("系统默认")
	if code {
		defaultLabel = app.localized("系统等宽字体")
	}
	options := []string{defaultLabel}
	for _, family := range app.fontFamilies {
		if !code || family.Monospaced {
			options = append(options, family.Name)
		}
	}
	selected := *value
	if selected == "" {
		selected = defaultLabel
	} else if !slices.Contains(options, selected) {
		options = append(options, selected)
	}
	ui.Text(c, app.localized(label))
	picker := ui.Select(c, &selected, options).FillWidth().MinWidth(0).Label(app.localized(label))
	picker.Disabled(app.interfaceSaving)
	if picker.Changed() {
		*value = selected
		if selected == defaultLabel {
			*value = ""
		}
	}
}

func (app *workbench) fontSizePreference(c *ui.Context, label string, value *float32) {
	defaultLabel := app.localized("系统默认")
	options := []string{defaultLabel, "10", "11", "12", "13", "14", "15", "16", "18", "20", "24", "28", "32"}
	selected := defaultLabel
	if *value != 0 {
		selected = strconv.FormatFloat(float64(*value), 'f', -1, 32)
		if !slices.Contains(options, selected) {
			options = append(options, selected)
		}
	}
	ui.Text(c, app.localized(label))
	picker := ui.Select(c, &selected, options).FillWidth().MinWidth(0).Label(app.localized(label))
	picker.Disabled(app.interfaceSaving)
	if picker.Changed() {
		*value = 0
		if selected != defaultLabel {
			size, _ := strconv.ParseFloat(selected, 32)
			*value = float32(size)
		}
	}
}

func (app *workbench) saveInterfaceSettings() {
	if app.preferences == nil || app.window == nil || app.interfaceSaving {
		return
	}
	store, settings := app.preferences, app.interfaceDraft
	if settings.StartupMode == "" {
		settings.StartupMode = "normal"
	}
	app.interfaceSaving = true
	app.preferenceRevision++
	revision := app.preferenceRevision
	go func() {
		err := store.SetInterface(settings, revision)
		mygo.RunOnMain(func() {
			if app.shuttingDown || app.preferences != store {
				return
			}
			app.interfaceSaving = false
			if err != nil {
				app.reportError("保存界面设置失败", err)
			} else {
				app.interfaceSettings, app.interfaceDraft = settings, settings
				app.status = app.localized("界面设置已保存。")
			}
			app.invalidateSettingsWindow()
			app.window.Invalidate()
		})
	}()
}
