package app

import "github.com/egoist/mygo/ui"

func (app *workbench) savedQueryEditorModal(c *ui.Context) {
	if ui.Modal(c, &app.savedQueryEditorOpen, func() {
		ui.Column(c).Width(720).MaxWidthPercent(90).MaxHeightPercent(90).Padding(18).Gap(12).
			Background(c.Theme().Surface).Border(1, c.Theme().Border).Radius(c.Theme().Radius + 4).
			Children(func() {
				ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
					title := "新建自定义查询"
					if app.savedQueryOriginalName != "" {
						title = "编辑自定义查询"
					}
					ui.Text(c, app.localized(title)).Bold().Role(ui.RoleHeading).Level(2).Grow(1)
					close := iconButton(c, app.localized("关闭"), actionIcons["close"]).Size(32, 32).Padding(5)
					close.Disabled(app.savedQuerySaving)
					if close.Clicked() {
						app.closeSavedQueryEditor()
					}
				})
				name := ui.TextInput(c, &app.savedQueryDraft.Name).Placeholder(app.localized("输入查询名称")).Label(app.localized("查询名称"))
				name.Disabled(app.savedQuerySaving)
				statement := ui.TextArea(c, &app.savedQueryDraft.SQL).Lines(12, 24).FillWidth().Placeholder(app.localized("输入 SQL 语句")).Label(app.localized("SQL 查询"))
				statement.Disabled(app.savedQuerySaving)
				if validation := app.savedQueryValidationError(); validation != "" {
					ui.Text(c, app.localized(validation)).TextColor(c.Theme().Danger)
				}
				ui.Row(c).Gap(8).Children(func() {
					ui.Spacer(c).Grow(1)
					cancel := ui.Button(c, app.localized("取消"))
					cancel.Disabled(app.savedQuerySaving)
					if cancel.Clicked() {
						app.closeSavedQueryEditor()
					}
					save := ui.PrimaryButton(c, app.localized("保存查询"))
					save.Disabled(app.savedQuerySaving || app.savedQueryValidationError() != "")
					if save.Clicked() {
						app.saveSavedQuery()
					}
				})
			})
	}).Dismissed() {
		if !app.savedQuerySaving {
			app.closeSavedQueryEditor()
		}
	}
}
