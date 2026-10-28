package app

import "github.com/egoist/mygo/ui"

func (app *workbench) etcdMaintenanceWorkspace(c *ui.Context) {
	ui.Column(c).Fill().MinHeight(0).Children(func() {
		ui.Row(c).FillWidth().Padding(10, 16).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Column(c).Grow(1).Gap(3).Children(func() {
				ui.Text(c, app.localized("维护")).Bold().Role(ui.RoleHeading).Level(2)
				ui.Text(c, app.localized("整理成员后端数据库并查看或解除集群告警。 ")).TextColor(c.Theme().TextMuted)
			})
			refresh := iconButton(c, app.localized("刷新维护状态"), actionIcons["refresh"])
			refresh.Disabled(app.busy || app.dialogOpen || app.etcdDashboardLoading)
			if refresh.Clicked() {
				app.refreshEtcdDashboard()
			}
		})
		ui.Scroll(c).Fill().Grow(1).MinHeight(0).Children(func() {
			ui.Column(c).FillWidth().Padding(16).Gap(12).Children(func() {
				if app.etcdDashboardLoading {
					ui.Text(c, app.localized("正在读取集群状态…")).TextColor(c.Theme().TextMuted)
					return
				}
				if app.etcdDashboardError != "" {
					ui.Text(c, app.localized("读取仪表盘失败：")+app.etcdDashboardError).TextColor(c.Theme().Danger)
					return
				}
				app.etcdDashboardMembers(c)
				app.etcdDashboardAlarms(c)
			})
		})
	})
}
