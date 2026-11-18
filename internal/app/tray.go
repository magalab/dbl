package app

import (
	"runtime"

	"github.com/egoist/mygo"
	"github.com/magalab/dbl/resources"
)

func (app *workbench) syncTray() {
	if runtime.GOOS != "windows" {
		return
	}
	if !app.interfaceSettings.CloseToTray {
		if app.tray != nil {
			app.tray.Destroy()
			app.tray = nil
		}
		return
	}
	if app.tray != nil {
		return
	}
	menu := mygo.NewMenu([]*mygo.MenuItem{
		{Label: app.localized("显示 DBL"), Click: func(*mygo.MenuItem, *mygo.Window) {
			app.showMainWindow()
		}},
		mygo.Separator(),
		{Label: app.localized("退出"), Click: func(*mygo.MenuItem, *mygo.Window) {
			mygo.App.Quit()
		}},
	})
	tray, err := mygo.NewTray(mygo.TrayOptions{
		ID:      "com.magalab.dbl",
		Icon:    resources.MacOSIcon,
		ToolTip: app.localized("DBL 数据工作台"),
		Menu:    menu,
	})
	if err != nil {
		app.reportError("创建系统托盘失败", err)
		return
	}
	app.tray = tray
}

func (app *workbench) showMainWindow() {
	if app.window == nil {
		return
	}
	app.window.Show()
	app.window.Focus()
}
