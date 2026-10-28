package app

import (
	"context"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/magalab/dbl/internal/sqlstream"
)

func TestCancelResultCountKeepsStoppingStateUntilQueryReturns(t *testing.T) {
	app := newTestWorkbench()
	workspace := app.activeQuery
	ctx, cancel := context.WithCancel(context.Background())
	app.resultCountWorkspace = workspace
	app.cancelResultCount = cancel

	app.cancelResultCountForWorkspace(workspace)
	if ctx.Err() != context.Canceled {
		t.Fatalf("count context error = %v, want context.Canceled", ctx.Err())
	}
	if app.resultCountWorkspace != workspace {
		t.Fatal("count workspace was cleared before the query returned")
	}
	if app.cancelResultCount == nil {
		t.Fatal("cancel handle was cleared before the query returned")
	}
	if !app.resultCountCancelRequested {
		t.Fatal("cancel request state was not retained")
	}

	app.cancelResultCountForWorkspace(workspace)
	if app.resultCountWorkspace != workspace || !app.resultCountCancelRequested {
		t.Fatal("a repeated cancel request cleared the in-flight count state")
	}
}

func TestCanceledResultCountKeepsRetryControlVisible(t *testing.T) {
	app := newTestWorkbench()
	app.activeQuery.resultSQL = "SELECT 1 AS value"
	app.activeQuery.columns = []sqlstream.Column{{Name: "value"}}
	app.activeQuery.resultCountCancelled = true
	tester := ui.NewTester(app.view, 1200, 900)
	if !tester.HasText("统计已取消 · 重试") {
		t.Fatal("canceled count state did not leave a visible retry control")
	}
}
