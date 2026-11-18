package app

import (
	"context"
	"errors"
)

func (app *workbench) beginWelcomeConnectionAttempt(profileID, name string) {
	app.welcomeConnectionProfileID = profileID
	app.welcomeConnectionProfileName = name
	app.welcomeConnectionPending = true
	app.errorSummary, app.errorDetails = "", ""
	app.showErrorDetails = false
}

func (app *workbench) finishWelcomeConnectionAttempt(profileID string, err error) {
	if profileID == "" || app.welcomeConnectionProfileID != profileID {
		return
	}
	app.welcomeConnectionPending = false
	if err == nil || errorsIsCanceled(err) {
		app.welcomeConnectionProfileID = ""
		app.welcomeConnectionProfileName = ""
	}
}

func (app *workbench) retryWelcomeConnection() bool {
	for _, profile := range app.connectionProfiles {
		if profile.ID == app.welcomeConnectionProfileID {
			app.openSavedProfile(profile)
			return true
		}
	}
	return false
}

func errorsIsCanceled(err error) bool {
	return errors.Is(err, context.Canceled)
}

func (app *workbench) dismissWelcomeConnectionFeedback() {
	app.welcomeConnectionProfileID = ""
	app.welcomeConnectionProfileName = ""
	app.welcomeConnectionPending = false
	if app.errorSummary == app.status {
		app.status = ""
		app.errorSummary, app.errorDetails = "", ""
		app.showErrorDetails = false
	}
	app.window.Invalidate()
}
