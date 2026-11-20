package app

import (
	"fmt"
	"runtime"
	"slices"
	"strconv"
	"strings"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
	"github.com/magalab/dbl/internal/preferences"
)

const defaultEditorFontSize = 13 // MyGo editor fallback.

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

func (app *workbench) interfaceSettingsUI(c *ui.Context, defaultFontSize float32) {
	theme := c.Theme()
	ui.Text(c, app.localized("更改会自动保存。 ")).TextColor(theme.TextMuted)
	if app.interfaceSaveFailed {
		ui.Text(c, app.localized("保存界面设置失败")).TextColor(theme.Danger)
	}
	app.startupModeSetting(c)
	if runtime.GOOS == "windows" {
		app.settingRow(c, "关闭窗口时最小化到托盘", func() {
			closeToTray := app.interfaceDraft.CloseToTray
			check := ui.Switch(c, &closeToTray).Label(app.localized("关闭窗口时最小化到托盘")).
				Tooltip(app.localized("关闭窗口后，左键点击托盘图标可恢复窗口，右键可打开菜单。")).Disabled(app.interfaceSaving)
			if check.Changed() {
				app.interfaceDraft.CloseToTray = closeToTray
				app.saveInterfaceSettings()
			}
		})
	}
	app.fontSettingsGroup(c, false, defaultFontSize)
	app.fontSettingsGroup(c, true, defaultEditorFontSize)
}

func (app *workbench) startupModeSetting(c *ui.Context) {
	modes := [...]string{"normal", "maximized", "fullscreen"}
	labels := [...]string{"正常", "最大化", "全屏"}
	icons := [...]string{"win-normal", "win-maximize", "win-fullscreen"}
	selected := max(0, slices.Index(modes[:], app.interfaceDraft.StartupMode))
	theme := c.Theme()
	app.settingRow(c, "窗口启动方式", func() {
		control := ui.SegmentedBase(c, &selected, len(modes))
		track := control.Track.Padding(2).Gap(2).AlignItems(ui.Stretch).
			Radius(theme.Radius).Border(1, theme.Border).Background(theme.Surface).
			Label(app.localized("窗口启动方式")).Disabled(app.interfaceSaving)
		track.Children(func() {
			for i, label := range labels {
				name := app.localized(label)
				detail := app.localized("窗口启动方式在下次打开应用时生效。")
				segment := control.Segment(i).Size(34, 26).Radius(max(0, theme.Radius-2)).
					Label(name).Tooltip(name + "\n" + detail)
				if i == selected {
					segment.Background(theme.Accent).TextColor(theme.AccentText)
				} else if segment.Hovered() {
					segment.Background(theme.SurfaceHover)
				}
				segment.Children(func() { ui.Icon(c, actionIcons[icons[i]]).Size(16, 16) })
			}
		})
		if track.Changed() {
			app.interfaceDraft.StartupMode = modes[selected]
			app.saveInterfaceSettings()
		}
	})
}

func (app *workbench) fontSettingsGroup(c *ui.Context, code bool, defaultSize float32) {
	fontLabel, sizeLabel := "界面字体", "界面字号"
	family, size := &app.interfaceDraft.FontFamily, &app.interfaceDraft.FontSize
	preview, previewLabel := "DBL 数据工作台 · Aa 0123", "界面字体预览"
	if code {
		fontLabel, sizeLabel = "编辑器字体", "编辑器字号"
		family, size = &app.interfaceDraft.EditorFont, &app.interfaceDraft.EditorFontSize
		preview, previewLabel = "SELECT 名称, id FROM users;", "编辑器字体预览"
	}
	theme := c.Theme()
	app.settingRow(c, fontLabel, func() { app.fontPreference(c, fontLabel, family, code) })
	app.settingRow(c, sizeLabel, func() { app.fontSizePreference(c, sizeLabel, size, defaultSize) })
	font := *family
	if code && font == "" {
		font = "monospace"
	}
	ui.Text(c, app.localized(preview)).Font(font).FontSize(app.previewFontSize(code, defaultSize)).
		FillWidth().Padding(5, 10).SingleLine().Ellipsis("…").TextColor(theme.TextMuted).Label(app.localized(previewLabel))
}

func (app *workbench) previewFontSize(code bool, defaultSize float32) float32 {
	if code {
		if app.interfaceDraft.EditorFontSize != 0 {
			return app.interfaceDraft.EditorFontSize
		}
		return defaultSize
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
	picker := ui.Combobox(c, &selected, options).Width(208).MinWidth(0).Label(app.localized(label))
	picker.Disabled(app.interfaceSaving)
	if picker.Changed() {
		*value = selected
		if selected == defaultLabel {
			*value = ""
		}
		app.saveInterfaceSettings()
	}
}

func (app *workbench) fontSizePreference(c *ui.Context, label string, value *float32, defaultSize float32) {
	type sizeInput struct {
		value   float32
		text    string
		invalid bool
	}
	column := ui.Column(c).Width(208).MinWidth(0).Gap(4)
	draft := ui.Local(column, "sizeInput", func() sizeInput {
		return sizeInput{value: -1}
	})
	if draft.value != *value {
		*draft = sizeInput{value: *value}
		if *value != 0 {
			draft.text = strconv.FormatFloat(float64(*value), 'f', -1, 32)
		}
	}
	column.Children(func() {
		defaultLabel := fmt.Sprintf(app.localized("系统默认（%g）"), defaultSize)
		input := ui.TextInput(c, &draft.text).FillWidth().Label(app.localized(label)).
			Placeholder(defaultLabel).Tooltip(defaultLabel + "\n" + app.localized("输入字号后按回车保存，留空恢复系统默认。")).
			Disabled(app.interfaceSaving)
		if input.Changed() {
			draft.invalid = false
		}
		if input.Submitted() {
			text := strings.TrimSpace(draft.text)
			var size int
			if text != "" {
				n, err := strconv.Atoi(text)
				draft.invalid = err != nil || n < 10 || n > 32
				size = n
			}
			if !draft.invalid && *value != float32(size) {
				*value = float32(size)
				app.saveInterfaceSettings()
			}
		}
		if draft.invalid {
			ui.Text(c, app.localized("请输入 10 到 32 之间的整数。")).TextColor(c.Theme().Danger)
		}
	})
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
	app.interfaceSaveFailed = false
	app.preferenceRevision++
	revision := app.preferenceRevision
	go func() {
		err := store.SetInterface(settings, revision)
		mygo.RunOnMain(func() {
			if app.shuttingDown || app.preferences != store {
				return
			}
			app.finishInterfaceSettingsSave(settings, err)
			if err == nil {
				app.syncTray()
			}
			app.invalidateSettingsWindow()
			app.window.Invalidate()
		})
	}()
}

func (app *workbench) finishInterfaceSettingsSave(settings preferences.InterfaceSettings, err error) {
	app.interfaceSaving = false
	app.interfaceSaveFailed = err != nil
	if err != nil {
		app.interfaceDraft = app.interfaceSettings
		app.reportError("保存界面设置失败", err)
		return
	}
	app.interfaceSettings, app.interfaceDraft = settings, settings
	app.status = app.localized("界面设置已保存。")
}
