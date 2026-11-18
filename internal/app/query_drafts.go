package app

import (
	"fmt"
	"slices"
	"time"

	"github.com/egoist/mygo"
	"github.com/magalab/dbl/internal/preferences"
)

type queryDraftObservation struct {
	id, version                    uint64
	title, connectionID, namespace string
	active                         bool
}

func (app *workbench) queryDraftObservation() []queryDraftObservation {
	var result []queryDraftObservation
	for _, workspace := range app.workspaces {
		if !isSQLQueryWorkspace(workspace) || workspace.implicitQuery {
			continue
		}
		result = append(result, queryDraftObservation{
			id: workspace.id, version: workspace.query.Buffer.Version(),
			title: workspace.title, connectionID: workspace.queryConnectionID,
			namespace: workspace.queryNamespace, active: workspace == app.activeQuery,
		})
	}
	return result
}

func (app *workbench) queryDraftSnapshot() (drafts []preferences.QueryDraft, active, skipped int) {
	for _, workspace := range app.workspaces {
		if !isSQLQueryWorkspace(workspace) || workspace.implicitQuery {
			continue
		}
		if workspace.query.Buffer.Len() > preferences.MaxQueryDraftSQLBytes || len(drafts) >= preferences.MaxQueryDrafts {
			skipped++
			continue
		}
		if workspace == app.activeQuery {
			active = len(drafts)
		}
		drafts = append(drafts, preferences.QueryDraft{
			Title: workspace.title, SQL: workspace.query.Buffer.String(),
			ConnectionID: workspace.queryConnectionID, Namespace: workspace.queryNamespace,
		})
	}
	return
}

func (app *workbench) restoreQueryDrafts(drafts []preferences.QueryDraft, active int) {
	if !app.queryDraftsEnabled || len(drafts) == 0 {
		return
	}
	for i, draft := range drafts {
		if i >= preferences.MaxQueryDrafts {
			break
		}
		workspace := newQueryWorkspace(app.nextWorkspaceID)
		app.nextWorkspaceID++
		if draft.Title != "" {
			workspace.title = draft.Title
		}
		workspace.query.Buffer.Set(draft.SQL)
		workspace.queryConnectionID, workspace.queryNamespace = draft.ConnectionID, draft.Namespace
		app.workspaces = append(app.workspaces, workspace)
		if i == active || i == 0 {
			app.activeWorkspace, app.activeQuery = len(app.workspaces)-1, workspace
		}
	}
	app.queryWorkspaceRequested = true
	app.observedQueryDrafts = app.queryDraftObservation()
}

func (app *workbench) stopQueryDraftSaveTimer() {
	app.queryDraftSaveGeneration++
	if app.queryDraftSaveTimer != nil {
		app.queryDraftSaveTimer.Stop()
		app.queryDraftSaveTimer = nil
	}
}

// Observe buffer versions instead of copying every SQL string on every frame.
// Both observation and snapshotting run on the UI thread.
func (app *workbench) scheduleQueryDraftSave() {
	if !app.queryDraftsEnabled || app.queryDraftsSaving || app.preferences == nil || app.window == nil || app.shuttingDown {
		return
	}
	observed := app.queryDraftObservation()
	if slices.Equal(observed, app.observedQueryDrafts) {
		return
	}
	app.observedQueryDrafts = observed
	app.stopQueryDraftSaveTimer()
	generation := app.queryDraftSaveGeneration
	app.queryDraftSaveTimer = time.AfterFunc(750*time.Millisecond, func() {
		mygo.RunOnMain(func() {
			if app.shuttingDown || generation != app.queryDraftSaveGeneration || !app.queryDraftsEnabled {
				return
			}
			app.queryDraftSaveTimer = nil
			app.saveQueryDrafts(true)
		})
	})
}

func (app *workbench) setQueryDraftsEnabled(enabled bool) {
	if app.preferences == nil || app.window == nil || app.queryDraftsSaving {
		return
	}
	app.stopQueryDraftSaveTimer()
	app.queryDraftsEnabled, app.queryDraftsSaving = enabled, true
	app.observedQueryDrafts = app.queryDraftObservation()
	app.saveQueryDrafts(false)
}

func (app *workbench) saveQueryDrafts(automatic bool) {
	store, enabled := app.preferences, app.queryDraftsEnabled
	drafts, active, skipped := app.queryDraftSnapshot()
	app.preferenceRevision++
	revision := app.preferenceRevision
	app.queryDraftSaveRevision = revision
	go func() {
		err := store.SetQueryDrafts(enabled, drafts, active, revision)
		mygo.RunOnMain(func() {
			if app.shuttingDown || app.preferences != store || app.queryDraftSaveRevision != revision {
				return
			}
			if !automatic {
				app.queryDraftsSaving = false
			}
			if err != nil {
				if !automatic {
					app.queryDraftsEnabled = !enabled
				}
				app.reportError("保存 SQL 草稿失败", err)
			} else if skipped > 0 && enabled {
				app.status = fmt.Sprintf(app.localized("%d 个 SQL 草稿超过 256 KiB，未保存。"), skipped)
			} else if !automatic {
				if enabled {
					app.status = app.localized("SQL 草稿自动保存已开启。")
				} else {
					app.status = app.localized("SQL 草稿自动保存已关闭，本机草稿已清除。")
				}
			}
			app.invalidateSettingsWindow()
			app.window.Invalidate()
		})
	}()
}

func (app *workbench) flushQueryDrafts() error {
	app.stopQueryDraftSaveTimer()
	if app.preferences == nil || !app.queryDraftsEnabled && !app.queryDraftsSaving {
		return nil
	}
	drafts, active, _ := app.queryDraftSnapshot()
	app.preferenceRevision++
	return app.preferences.SetQueryDrafts(app.queryDraftsEnabled, drafts, active, app.preferenceRevision)
}
