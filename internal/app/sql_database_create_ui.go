package app

import "github.com/egoist/mygo/ui"

func (app *workbench) sqlDatabaseCreateModal(c *ui.Context) {
	if ui.Modal(c, &app.sqlDatabaseCreateOpen, func() {
		ui.Column(c).Width(440).MaxHeightPercent(80).Padding(14).Gap(12).Children(func() {
			ui.Row(c).AlignItems(ui.Center).Gap(8).Children(func() {
				ui.Text(c, app.localized("新建数据库")).Bold().Role(ui.RoleHeading).Level(2).Grow(1)
				close := iconButton(c, app.localized("关闭"), actionIcons["close"]).Size(30, 30).Padding(4)
				if close.Clicked() {
					app.closeSQLDatabaseCreator()
				}
			})
			ui.Text(c, app.localized("在当前连接中新建一个数据库。创建后可从连接树刷新并打开。 ")).TextColor(c.Theme().TextMuted)
			name := ui.TextInput(c, &app.sqlDatabaseCreateName).FillWidth().Placeholder(app.localized("数据库名称")).Label(app.localized("数据库名称"))
			name.Disabled(app.busy || app.exporting)
			ui.Row(c).Gap(8).Children(func() {
				ui.Spacer(c).Grow(1)
				cancel := ui.Button(c, app.localized("取消"))
				cancel.Disabled(app.busy || app.exporting)
				if cancel.Clicked() {
					app.closeSQLDatabaseCreator()
				}
				create := ui.PrimaryButton(c, app.localized("创建数据库"))
				create.Disabled(app.busy || app.exporting || app.openedReadOnly || app.sqlDatabaseCreateName == "")
				if create.Clicked() {
					app.confirmSQLDatabaseCreate()
				}
			})
		})
	}).Dismissed() {
		app.closeSQLDatabaseCreator()
	}
}
