package app

func (app *workbench) setTableChangeConfirmationEnabled(enabled bool) {
	if app.preferences == nil || app.window == nil || app.tableChangeConfirmationSaving {
		return
	}
	store := app.preferences
	app.skipTableChangeConfirmation = !enabled
	app.tableChangeConfirmationSaving = true
	app.preferenceRevision++
	revision := app.preferenceRevision
	go func() {
		err := store.SetSkipTableChangeConfirmation(!enabled, revision)
		app.window.Update(func() {
			if app.preferences != store {
				return
			}
			app.tableChangeConfirmationSaving = false
			if err != nil {
				app.skipTableChangeConfirmation = enabled
				app.reportError("保存表格修改确认设置失败", err)
			} else {
				app.status = app.localized("表格修改确认设置已保存。")
			}
			app.invalidateSettingsWindow()
			app.window.Invalidate()
		})
	}()
}
