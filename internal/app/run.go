package app

import (
	"log"
	"runtime"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/plugins/updater"
	"github.com/egoist/mygo/plugins/updater/native"
	"github.com/egoist/mygo/ui"
	"github.com/magalab/dbl/resources"
)

// Run initializes and starts the desktop application.
func Run() {
	workbench := newWorkbench()
	workbench.updatesEnabled = mygo.Updater.Enabled()
	mygo.App.SetLanguages("en", "zh-Hans")
	mygo.App.SetLocale(mygoLanguageTag(workbench.language))
	setAppearanceSource(workbench.appearance)
	mygo.App.SetName("DBL")
	mygo.Use(native.New(updater.Options{
		Interval:               updateInterval(workbench.updateIntervalHours),
		DisableAutomaticChecks: true,
		Icon:                   resources.MacOSIcon,
		Strings:                updateDialogStrings(),
	}))
	mygo.App.SetMenu(mygo.NewMenu(appMenuItems(workbench, runtime.GOOS)))
	mygo.App.WhenReady(func() {
		updater.OnChange(func() {
			mygo.RunOnMain(workbench.invalidateSettingsWindow)
		})
		workbench.window = mygo.NewWindow(mygo.WindowOptions{
			Title:         "DBL · 数据工作台",
			Width:         1280,
			Height:        820,
			Maximized:     workbench.interfaceSettings.StartupMode == "maximized",
			FullScreen:    workbench.interfaceSettings.StartupMode == "fullscreen",
			TitleBarStyle: mygo.TitleBarHiddenInset,
			Content:       ui.View(workbench.view),
		})
		workbench.window.OnClose(func(event *mygo.CloseEvent) {
			if workbench.interfaceSettings.CloseToTray && workbench.tray != nil && runtime.GOOS == "windows" && !workbench.quitting {
				if err := workbench.flushQueryDrafts(); err != nil {
					event.PreventDefault()
					workbench.reportError("保存 SQL 草稿失败", err)
					workbench.window.Invalidate()
					return
				}
				event.PreventDefault()
				workbench.window.Hide()
				return
			}
			if err := workbench.flushQueryDrafts(); err != nil {
				event.PreventDefault()
				workbench.reportError("保存 SQL 草稿失败", err)
				workbench.window.Invalidate()
				return
			}
			workbench.shuttingDown = true
		})
		workbench.window.OnClosed(workbench.shutdown)
		mygo.App.OnBeforeQuit(func(*mygo.QuitEvent) { workbench.quitting = true })
		mygo.App.OnQuitCanceled(func() { workbench.quitting = false })
		workbench.syncTray()
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}
