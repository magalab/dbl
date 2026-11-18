package app

import (
	"errors"
	"testing"
)

func TestWelcomeConnectionFeedbackTracksFailureAndRetry(t *testing.T) {
	app := &workbench{}
	app.beginWelcomeConnectionAttempt("profile-1", "Remote DB")
	if !app.welcomeConnectionPending || app.welcomeConnectionProfileID != "profile-1" {
		t.Fatal("connection attempt was not marked pending")
	}

	app.finishWelcomeConnectionAttempt("another-profile", errors.New("ignored"))
	if !app.welcomeConnectionPending {
		t.Fatal("unrelated completion changed the pending attempt")
	}

	app.finishWelcomeConnectionAttempt("profile-1", errors.New("connection refused"))
	if app.welcomeConnectionPending || app.welcomeConnectionProfileID != "profile-1" {
		t.Fatal("failed connection should remain available for retry")
	}

	app.beginWelcomeConnectionAttempt("profile-1", "Remote DB")
	app.finishWelcomeConnectionAttempt("profile-1", nil)
	if app.welcomeConnectionProfileID != "" || app.welcomeConnectionPending {
		t.Fatal("successful connection feedback was not cleared")
	}
}
