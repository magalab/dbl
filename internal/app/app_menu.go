package app

import (
	"strings"

	"github.com/egoist/mygo"
)

func aboutPanelOptions(version string) mygo.AboutPanelOptions {
	version = strings.TrimSpace(version)
	if version == "" || version == "devel" {
		version = "开发版"
	}
	return mygo.AboutPanelOptions{
		ApplicationName:    "DBL · 数据工作台",
		ApplicationVersion: version,
		Copyright:          "Copyright © 2026 Magalab",
		Credits:            "使用 MyGo " + mygo.Version + " 构建",
	}
}

func showAboutPanel() {
	mygo.App.ShowAboutPanel(aboutPanelOptions(mygo.App.Version()))
}

func appMenuItems(app *workbench, goos string) []*mygo.MenuItem {
	menuItems := []*mygo.MenuItem{
		{Role: mygo.RoleAppMenu, Submenu: []*mygo.MenuItem{
			{Label: app.localized("关于 DBL"), Click: func(*mygo.MenuItem, *mygo.Window) {
				showAboutPanel()
			}},
			{Label: app.localized("设置…"), Accelerator: "CmdOrCtrl+,", Click: func(*mygo.MenuItem, *mygo.Window) {
				app.openSettings()
			}},
			mygo.Separator(),
			{Role: mygo.RoleServices},
			mygo.Separator(),
			{Role: mygo.RoleHide},
			{Role: mygo.RoleHideOthers},
			{Role: mygo.RoleUnhide},
			mygo.Separator(),
			{Role: mygo.RoleQuit},
		}},
		{Label: app.localized("数据库"), Submenu: []*mygo.MenuItem{
			{Label: app.localized("新建连接…"), Accelerator: "CmdOrCtrl+N", Click: func(*mygo.MenuItem, *mygo.Window) {
				app.beginNewConnection()
			}},
			{Label: app.localized("打开 SQLite 文件…"), Accelerator: "CmdOrCtrl+O", Click: func(*mygo.MenuItem, *mygo.Window) {
				app.chooseSQLiteFile()
			}},
			{Label: app.localized("新建 SQLite 文件…"), Click: func(*mygo.MenuItem, *mygo.Window) {
				app.chooseNewSQLiteFile()
			}},
			{Label: app.localized("关闭连接"), Click: func(*mygo.MenuItem, *mygo.Window) {
				app.closeConnection()
			}},
		}},
		{Label: app.localized("查询"), Submenu: []*mygo.MenuItem{
			{Label: app.localized("新建查询工作区"), Accelerator: "CmdOrCtrl+T", Click: func(*mygo.MenuItem, *mygo.Window) {
				app.addWorkspace()
			}},
			{Label: app.localized("执行 SQL"), Accelerator: "CmdOrCtrl+Return", Click: func(*mygo.MenuItem, *mygo.Window) {
				app.executeSQL()
			}},
		}},
		{Label: app.localized("面板"), Submenu: []*mygo.MenuItem{
			{Label: app.localized("左侧边栏"), Accelerator: "CmdOrCtrl+B", Click: func(*mygo.MenuItem, *mygo.Window) {
				app.toggleLeftPanel()
			}},
			{Label: app.localized("AI 助手"), Accelerator: "CmdOrCtrl+Shift+B", Click: func(*mygo.MenuItem, *mygo.Window) {
				app.toggleAIPanel()
			}},
		}},
		{Role: mygo.RoleEditMenu},
		{Role: mygo.RoleWindowMenu},
	}
	if goos != "darwin" {
		menuItems = append(menuItems,
			&mygo.MenuItem{Label: app.localized("设置"), Submenu: []*mygo.MenuItem{
				{Label: app.localized("打开设置…"), Accelerator: "CmdOrCtrl+,", Click: func(*mygo.MenuItem, *mygo.Window) {
					app.openSettings()
				}},
			}},
			&mygo.MenuItem{Label: app.localized("帮助"), Submenu: []*mygo.MenuItem{
				{Label: app.localized("关于 DBL"), Click: func(*mygo.MenuItem, *mygo.Window) {
					showAboutPanel()
				}},
			}},
		)
	}
	return menuItems
}
