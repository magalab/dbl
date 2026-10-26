package connection_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/magalab/dbl/internal/connection"
	"github.com/magalab/dbl/internal/connection/mock"
)

func TestManagerConnectHealthAndDisconnectTransitions(t *testing.T) {
	var manager connection.Manager
	if got := manager.Snapshot().State; got != connection.Disconnected {
		t.Fatalf("initial state = %v, want disconnected", got)
	}
	if err := manager.Connect(context.Background(), mock.Connector{}); err != nil {
		t.Fatal(err)
	}
	if snapshot := manager.Snapshot(); snapshot.State != connection.Connected || snapshot.Driver != "mock" || snapshot.Err != nil {
		t.Fatalf("connected snapshot = %#v", snapshot)
	}
	if err := manager.HealthCheck(context.Background()); err != nil {
		t.Fatalf("health check failed: %v", err)
	}
	if err := manager.Disconnect(); err != nil {
		t.Fatal(err)
	}
	if got := manager.Snapshot().State; got != connection.Disconnected {
		t.Fatalf("state after disconnect = %v, want disconnected", got)
	}
	if err := manager.HealthCheck(context.Background()); err == nil {
		t.Fatal("health check succeeded without an active connection")
	}
}

func TestManagerReportsConnectionFailureAndCancellation(t *testing.T) {
	var manager connection.Manager
	wantErr := errors.New("mock connection refused")
	if err := manager.Connect(context.Background(), mock.Connector{ConnectErr: wantErr}); !errors.Is(err, wantErr) {
		t.Fatalf("Connect error = %v, want %v", err, wantErr)
	}
	if snapshot := manager.Snapshot(); snapshot.State != connection.ConnectionFailed || !errors.Is(snapshot.Err, wantErr) {
		t.Fatalf("failed snapshot = %#v", snapshot)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
	defer cancel()
	err := manager.Connect(ctx, mock.Connector{Delay: time.Second})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("canceled Connect error = %v, want deadline exceeded", err)
	}
	if snapshot := manager.Snapshot(); snapshot.State != connection.ConnectionFailed || !errors.Is(snapshot.Err, context.DeadlineExceeded) {
		t.Fatalf("canceled snapshot = %#v", snapshot)
	}
}

func TestManagerReplaceReturnsPreviousConnectionForWorkspaceSwitch(t *testing.T) {
	var manager connection.Manager
	if err := manager.Connect(context.Background(), mock.Connector{}); err != nil {
		t.Fatal(err)
	}
	old := manager.Replace(&mock.Connection{})
	if old == nil {
		t.Fatal("Replace did not return the previous connection")
	}
	if err := manager.HealthCheck(context.Background()); err != nil {
		t.Fatalf("replacement connection is unhealthy: %v", err)
	}
	if err := old.HealthCheck(context.Background()); err != nil {
		t.Fatalf("manager closed previous connection before caller switched workspace: %v", err)
	}
	if err := old.Close(); err != nil {
		t.Fatal(err)
	}
	if err := manager.Disconnect(); err != nil {
		t.Fatal(err)
	}
}

func TestMockHealthFailureAndErrorClassification(t *testing.T) {
	var manager connection.Manager
	healthErr := errors.New("mock health failure")
	if err := manager.Connect(context.Background(), mock.Connector{HealthErr: healthErr}); err != nil {
		t.Fatal(err)
	}
	if err := manager.HealthCheck(context.Background()); !errors.Is(err, healthErr) {
		t.Fatalf("health error = %v, want %v", err, healthErr)
	}
	if got := connection.ClassifyCommonError(context.DeadlineExceeded); got != connection.ErrorTimeout {
		t.Fatalf("deadline classification = %v, want timeout", got)
	}
	if got := connection.ClassifyCommonError(context.Canceled); got != connection.ErrorCanceled {
		t.Fatalf("cancel classification = %v, want canceled", got)
	}
	_ = manager.Disconnect()
}
