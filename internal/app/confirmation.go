package app

import (
	"fmt"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/transfer"
	"github.com/egoist/mygo/ui"
	"github.com/magalab/dbl/internal/credentials"
	"github.com/magalab/dbl/internal/databaseicons"
)

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
	if request != nil {
		clear(request.password)
		request.password = nil
		if request.onChoose != nil {
			request.onChoose(choice)
		}
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
				ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
					if icon, color, ok := databaseicons.Icon(request.driver); ok {
						driverLabel := welcomeDriverLabel(request.driver)
						ui.Icon(c, icon).Size(20, 20).Shrink(0).TextColor(color).Label(driverLabel).Tooltip(driverLabel)
					}
					ui.Text(c, app.localized(request.title)).Bold().Role(ui.RoleHeading).Level(2).Grow(1)
					if len(request.fields) > 0 && iconButton(c, app.localized("关闭"), actionIcons["close"]).BorderWidth(0).Clicked() {
						app.resolveConfirmation(0)
					}
				})
				ui.Text(c, app.localized(request.message))
				if len(request.fields) > 0 {
					ui.Scroll(c).MaxHeight(300).Children(func() {
						ui.Column(c).FillWidth().Gap(8).Children(func() {
							for _, field := range request.fields {
								if field.label == "DSN" && request.passwordRef != "" && dsnSupportsPassword(request.driver) {
									app.connectionDSNField(c, field, request)
								} else {
									app.connectionOverviewField(c, field)
								}
							}
							if request.passwordRef != "" && !dsnSupportsPassword(request.driver) {
								app.instancePasswordRow(c, request)
							}
						})
					})
				}
				if request.detail != "" {
					ui.Scroll(c).Height(220).Children(func() {
						ui.Text(c, request.detail).Selectable()
					})
				}
				if len(request.fields) == 0 {
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
				}
			})
	}).Dismissed() {
		app.resolveConfirmation(0)
	}
}

func (app *workbench) instancePasswordRow(c *ui.Context, request *appConfirmation) {
	label := app.localized("密码")
	row := ui.Row(c).FillWidth().Gap(10).AlignItems(ui.Center).Role(ui.RoleGroup).Label(label)
	row.Children(func() {
		ui.Icon(c, treeItemIcons["key"]).Size(18, 18).Shrink(0).TextColor(c.Theme().TextMuted).Tooltip(label)
		ui.Text(c, instancePasswordDisplay(request)).Grow(1).MinWidth(0)
		app.instancePasswordActions(c, request)
	})
	app.instancePasswordError(c, request)
}

func (app *workbench) connectionDSNField(c *ui.Context, field overviewField, request *appConfirmation) {
	label := app.localized(field.label)
	row := ui.Row(c).FillWidth().Gap(10).AlignItems(ui.Center).Role(ui.RoleGroup).Label(label)
	row.Children(func() {
		ui.Icon(c, actionIcons[field.icon]).Size(18, 18).Shrink(0).TextColor(c.Theme().TextMuted).Tooltip(label)
		password := []byte("********")
		if request.password != nil {
			password = request.password
		}
		ui.Text(c, dsnWithPassword(request.driver, field.value, password)).Grow(1).MinWidth(0).Selectable()
		app.instancePasswordVisibility(c, request)
		app.copyInstanceDSNButton(c, row, field, request)
	})
	app.instancePasswordError(c, request)
}

func instancePasswordDisplay(request *appConfirmation) string {
	if request.password == nil {
		return "********"
	}
	return string(request.password)
}

func (app *workbench) instancePasswordActions(c *ui.Context, request *appConfirmation) {
	app.instancePasswordVisibility(c, request)
	copyLabel, copyIcon := "复制%s", "copy"
	remaining := request.passwordCopiedUntil.Sub(c.Now())
	if remaining > 0 {
		copyLabel, copyIcon = "已复制%s", "check"
		c.After(remaining)
	}
	copyButton := iconButton(c, fmt.Sprintf(app.localized(copyLabel), app.localized("密码")), actionIcons[copyIcon]).BorderWidth(0)
	copyButton.Disabled(request.passwordLoading)
	if remaining > 0 {
		copyButton.TextColor(c.Theme().Success)
	}
	if copyButton.Clicked() {
		app.readInstancePassword(request, true)
	}
}

func (app *workbench) instancePasswordVisibility(c *ui.Context, request *appConfirmation) {
	visibility, visibilityIcon := "显示密码", "eye"
	if request.password != nil {
		visibility, visibilityIcon = "隐藏密码", "eyeOff"
	}
	toggle := iconButton(c, app.localized(visibility), actionIcons[visibilityIcon]).BorderWidth(0)
	toggle.Disabled(request.passwordLoading)
	if toggle.Clicked() {
		if request.password != nil {
			clear(request.password)
			request.password = nil
		} else {
			app.readInstancePassword(request, false)
		}
	}
}

func (app *workbench) copyInstanceDSNButton(c *ui.Context, row ui.Element, field overviewField, request *appConfirmation) {
	type feedback struct {
		value string
		until time.Time
	}
	state := ui.Local(row, "copyDSNFeedback", func() feedback { return feedback{} })
	if state.value != field.value {
		*state = feedback{value: field.value}
	}
	remaining := state.until.Sub(c.Now())
	label, icon := "复制%s", "copy"
	if remaining > 0 {
		label, icon = "已复制%s", "check"
		c.After(remaining)
	}
	button := iconButton(c, fmt.Sprintf(app.localized(label), app.localized(field.label)), actionIcons[icon]).BorderWidth(0)
	button.Disabled(request.passwordLoading)
	if remaining > 0 {
		button.TextColor(c.Theme().Success)
	}
	if !button.Clicked() {
		return
	}
	if request.password != nil {
		if err := writeClipboardText(dsnWithPassword(request.driver, field.value, request.password)); err != nil {
			request.passwordError = "复制失败"
			state.until = time.Time{}
			return
		}
		state.until = c.Now().Add(1500 * time.Millisecond)
		c.After(1500 * time.Millisecond)
		return
	}
	vault, window := app.vault, app.window
	if vault == nil || window == nil {
		request.passwordError = "读取已保存密码失败"
		return
	}
	request.passwordError = ""
	request.passwordLoading = true
	ref := credentials.Reference(request.passwordRef)
	go func() {
		secret, err := vault.Load(ref)
		window.Update(func() {
			defer clear(secret)
			if app.confirmation != request || app.shuttingDown {
				return
			}
			request.passwordLoading = false
			if err != nil {
				request.passwordError = "读取已保存密码失败"
				return
			}
			if err := writeClipboardText(dsnWithPassword(request.driver, field.value, secret)); err != nil {
				request.passwordError = "复制失败"
				return
			}
			state.until = time.Now().Add(1500 * time.Millisecond)
		})
	}()
}

func (app *workbench) instancePasswordError(c *ui.Context, request *appConfirmation) {
	if request.passwordError != "" {
		ui.Text(c, app.localized(request.passwordError)).TextColor(c.Theme().Danger).Role(ui.RoleStatus)
	}
}

func (app *workbench) readInstancePassword(request *appConfirmation, copyPassword bool) {
	request.passwordError = ""
	if copyPassword {
		request.passwordCopiedUntil = time.Time{}
	}
	vault, window := app.vault, app.window
	if vault == nil || window == nil {
		request.passwordError = "读取已保存密码失败"
		return
	}
	ref := credentials.Reference(request.passwordRef)
	request.passwordLoading = true
	go func() {
		secret, err := vault.Load(ref)
		window.Update(func() {
			defer clear(secret)
			if app.confirmation != request || app.shuttingDown {
				return
			}
			request.passwordLoading = false
			if err != nil {
				request.passwordError = "读取已保存密码失败"
				return
			}
			if !copyPassword {
				request.password = append([]byte{}, secret...)
				return
			}
			if err := writeClipboardText(string(secret)); err != nil {
				request.passwordError = "复制失败"
				return
			}
			request.passwordCopiedUntil = time.Now().Add(1500 * time.Millisecond)
		})
	}()
}

func writeClipboardText(value string) error {
	return mygo.Clipboard.Write(transfer.TextData(value))
}

func (app *workbench) copyFieldButton(c *ui.Context, row ui.Element, label, value string, write func(string) error) {
	const feedbackDuration = 1500 * time.Millisecond
	type feedback struct {
		label, value string
		until        time.Time
	}
	state := ui.Local(row, "copyFeedback", func() feedback { return feedback{} })
	if state.label != label || state.value != value {
		*state = feedback{label: label, value: value}
	}
	remaining := state.until.Sub(c.Now())
	action, icon := app.localized("复制%s"), "copy"
	if remaining > 0 {
		action, icon = app.localized("已复制%s"), "check"
		c.After(remaining)
	}
	button := iconButton(c, fmt.Sprintf(action, label), actionIcons[icon]).BorderWidth(0)
	if remaining > 0 {
		button.TextColor(c.Theme().Success)
	}
	if button.Clicked() {
		if err := write(value); err != nil {
			state.until = time.Time{}
			c.Toast(app.localized("复制失败"))
			return
		}
		state.until = c.Now().Add(feedbackDuration)
		c.After(feedbackDuration)
	}
}
