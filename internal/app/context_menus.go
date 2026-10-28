package app

import (
	"fmt"

	"github.com/egoist/mygo/ui"

	"github.com/magalab/dbl/internal/preferences"
)

func (app *workbench) connectionContextMenu(menu *ui.Menu, profile preferences.ConnectionProfile, connected bool) {
	id := app.profileLiveSessionID(profile)
	if connected {
		if !app.profileIsSelected(profile) && menu.Item("⇄  "+app.localized("切换到此连接")).Chosen() {
			app.openSavedProfile(profile)
		}
		if menu.Item("⏏  " + app.localized("关闭连接")).Disabled(app.busy || app.exporting || app.dialogOpen).Chosen() {
			app.closeProfileConnection(profile)
		}
	} else if menu.Item("⏵  " + app.localized("打开连接")).Disabled(app.busy || app.exporting || app.dialogOpen).Chosen() {
		app.openSavedProfile(profile)
	}
	menu.Separator()
	if menu.Item("＋  " + app.localized("新建查询")).Disabled(!connected || profile.Driver == preferences.DriverRedis || profile.Driver == preferences.DriverMongoDB || profile.Driver == preferences.DriverEtcd || app.busy || app.exporting || app.dialogOpen).Chosen() {
		if connected && !app.profileIsSelected(profile) {
			app.activateLiveConnection(id)
		}
		app.openSQLQuery("")
	}
	if menu.Item("▣  " + app.localized("打开示例查询")).Disabled(!connected || profile.Driver == preferences.DriverRedis || profile.Driver == preferences.DriverMongoDB || profile.Driver == preferences.DriverEtcd || app.busy || app.exporting || app.dialogOpen).Chosen() {
		if connected && !app.profileIsSelected(profile) {
			app.activateLiveConnection(id)
		}
		app.openSQLQuery("SELECT 1;\n")
	}
	menu.Separator()
	if menu.Item("↻  " + app.localized("刷新")).Disabled(!connected || app.busy || app.exporting || app.dialogOpen).Chosen() {
		if app.profileIsSelected(profile) {
			app.refreshCurrentConnection()
		} else {
			if !app.activateLiveConnection(id) {
				app.openSavedProfile(profile)
			}
		}
	}
	if menu.Item("ⓘ  " + app.localized("实例信息")).Disabled(!connected).Chosen() {
		app.status = fmt.Sprintf("%s · %s · %s", profile.Name, profile.Driver, profile.Host)
	}
	menu.Separator()
	menu.Submenu("☷  "+app.localized("连接管理"), func(submenu *ui.Menu) {
		if submenu.Item("✎  " + app.localized("编辑连接")).Disabled(app.busy || app.exporting || app.dialogOpen).Chosen() {
			app.loadConnectionProfile(profile)
			app.showConnectionEditor = true
		}
		if submenu.Item("⎘  " + app.localized("复制连接")).Disabled(app.busy || app.exporting || app.dialogOpen).Chosen() {
			app.loadConnectionProfile(profile)
			app.selectedProfileID = ""
			app.profileName = profile.Name + " 副本"
			app.showConnectionEditor = true
		}
		if submenu.Item("⌫  " + app.localized("删除连接…")).Disabled(app.busy || app.exporting || app.dialogOpen || app.preferences == nil).Chosen() {
			app.confirmDeleteConnectionProfile(profile)
		}
	})
}

func (app *workbench) confirmDeleteConnectionProfile(profile preferences.ConnectionProfile) {
	if app.busy || app.exporting || app.dialogOpen || app.preferences == nil {
		return
	}
	app.requestConfirmation(
		app.localized("删除连接"),
		fmt.Sprintf(app.localized("确定删除连接‘%s’吗？"), profile.Name),
		app.localized("此操作会从本机移除已保存的连接配置。"),
		[]string{"取消", "删除"}, 1,
		func(choice int) {
			if choice == 1 {
				app.deleteConnectionProfile(profile.ID)
			}
		},
	)
}

func (app *workbench) databaseContextMenu(menu *ui.Menu, database string) {
	if menu.Item("＋  " + app.localized("新建查询")).Disabled(app.busy || app.exporting || app.dialogOpen || app.queryWorkspaceRequested && app.workspaceCount() >= maxWorkspaces).Chosen() {
		app.openSQLQuery("")
	}
	if menu.Item("▣  " + app.localized("打开示例查询")).Disabled(app.busy || app.exporting || app.dialogOpen).Chosen() {
		app.openSQLQuery("SELECT 1;\n")
	}
	menu.Separator()
	if menu.Item("↻  " + app.localized("刷新")).Disabled(app.busy || app.exporting || app.dialogOpen).Chosen() {
		app.refreshCurrentConnection()
	}
	if menu.Item("ⓘ  " + app.localized("数据库信息")).Chosen() {
		app.status = fmt.Sprintf("%s · %s", app.openedLabel, database)
	}
}
