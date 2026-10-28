package app

import "github.com/egoist/mygo/ui"

func (app *workbench) requestConfirmation(title, message, detail string, buttons []string, primary int, onChoose func(int)) {
	if app.dialogOpen || len(buttons) == 0 {
		return
	}
	app.confirmation = &appConfirmation{
		title: title, message: message, detail: detail,
		buttons: append([]string(nil), buttons...), primary: primary, onChoose: onChoose,
	}
	app.confirmationOpen = true
	app.dialogOpen = true
}

func (app *workbench) resolveConfirmation(choice int) {
	request := app.confirmation
	app.confirmation = nil
	app.confirmationOpen = false
	app.dialogOpen = false
	if request != nil && request.onChoose != nil {
		request.onChoose(choice)
	}
}

func (app *workbench) confirmationModal(c *ui.Context) {
	if app.confirmation == nil {
		return
	}
	request := app.confirmation
	if ui.Modal(c, &app.confirmationOpen, func() {
		ui.Column(c).Width(600).MaxWidthPercent(90).Padding(18).Gap(12).
			Background(c.Theme().Surface).Border(1, c.Theme().Border).Radius(c.Theme().Radius + 4).
			Children(func() {
				ui.Text(c, app.localized(request.title)).Bold().Role(ui.RoleHeading).Level(2)
				ui.Text(c, app.localized(request.message))
				if request.detail != "" {
					ui.Scroll(c).Height(220).Children(func() {
						ui.Text(c, request.detail).Selectable()
					})
				}
				ui.Row(c).Gap(8).Children(func() {
					for index, label := range request.buttons {
						var button ui.Element
						if index == request.primary {
							button = ui.PrimaryButton(c, app.localized(label))
						} else {
							button = ui.Button(c, app.localized(label))
						}
						if button.Clicked() {
							app.resolveConfirmation(index)
						}
					}
				})
			})
	}).Dismissed() {
		app.resolveConfirmation(0)
	}
}
