package app

import (
	"context"
	"fmt"
	"log"
	"sort"
	"strings"
	"time"

	"github.com/magalab/dbl/internal/preferences"
)

func (app *workbench) updateHistory(id uint64, started time.Time, outcome string) {
	if app.activeQuery == nil {
		return
	}
	app.updateWorkspaceHistory(app.activeQuery, id, started, outcome)
}

func (app *workbench) updateWorkspaceHistory(workspace *queryWorkspace, id uint64, started time.Time, outcome string) {
	if workspace == nil || id == 0 {
		return
	}
	var entry *queryRecord
	for i := range workspace.history {
		if workspace.history[i].ID == id {
			entry = &workspace.history[i]
			break
		}
	}
	if entry == nil {
		return
	}
	entry.Rows = workspace.resultWindow.Total
	entry.Outcome = safeHistoryOutcome(outcome)
	if !started.IsZero() {
		entry.Duration = time.Since(started)
	}
	app.persistQueryHistory()
}

func safeHistoryOutcome(outcome string) string {
	switch {
	case strings.Contains(outcome, "失败"):
		return "失败"
	case strings.Contains(outcome, "取消"):
		return "已取消"
	case strings.Contains(outcome, "超时"), strings.Contains(outcome, "超过"):
		return "超时"
	case strings.Contains(outcome, "停止"):
		return "已停止"
	case strings.Contains(outcome, "执行中"):
		return "执行中"
	default:
		return "完成"
	}
}

func (app *workbench) queryHistorySnapshot() ([]preferences.QueryHistoryEntry, int) {
	entries := make([]preferences.QueryHistoryEntry, 0)
	skipped := 0
	for _, workspace := range app.workspaces {
		for _, record := range workspace.history {
			if len(record.SQL) > preferences.MaxQueryHistorySQLBytes {
				skipped++
				continue
			}
			entries = append(entries, preferences.QueryHistoryEntry{
				ID: record.ID, SQL: record.SQL, Started: record.Started,
				Duration: record.Duration, Rows: record.Rows, Outcome: record.Outcome,
			})
		}
	}
	sort.SliceStable(entries, func(i, j int) bool {
		if entries[i].Started.Equal(entries[j].Started) {
			return entries[i].ID < entries[j].ID
		}
		return entries[i].Started.Before(entries[j].Started)
	})
	if len(entries) > preferences.MaxQueryHistoryEntries {
		entries = entries[len(entries)-preferences.MaxQueryHistoryEntries:]
	}
	return entries, skipped
}

func (app *workbench) persistQueryHistory() {
	if !app.queryHistoryEnabled || app.preferences == nil || app.window == nil {
		return
	}
	store := app.preferences
	entries, skipped := app.queryHistorySnapshot()
	app.preferenceRevision++
	revision := app.preferenceRevision
	app.queryHistorySaveRev = revision
	go func() {
		err := store.SetQueryHistory(true, entries, revision)
		app.window.Update(func() {
			if app.preferences != store || app.queryHistorySaveRev != revision {
				return
			}
			if err != nil {
				app.reportError("保存查询历史失败", err)
			} else if skipped > 0 {
				app.status = fmt.Sprintf("%d 条 SQL 超过 64 KiB，未写入本机查询历史。", skipped)
			}
		})
	}()
}

func (app *workbench) setQueryHistoryEnabled(enabled bool) {
	if app.preferences == nil || app.queryHistorySaving || app.busy || app.exporting || app.dialogOpen || app.cursor != nil {
		return
	}
	store := app.preferences
	app.queryHistoryEnabled = enabled
	app.queryHistorySaving = true
	app.preferenceRevision++
	revision := app.preferenceRevision
	app.queryHistorySaveRev = revision
	entries := []preferences.QueryHistoryEntry(nil)
	skipped := 0
	if enabled {
		entries, skipped = app.queryHistorySnapshot()
	}
	go func() {
		err := store.SetQueryHistory(enabled, entries, revision)
		app.window.Update(func() {
			if app.preferences != store || app.queryHistorySaveRev != revision {
				return
			}
			app.queryHistorySaving = false
			app.invalidateSettingsWindow()
			if err != nil {
				app.queryHistoryEnabled = !enabled
				app.reportError("保存查询历史设置失败", err)
				return
			}
			if enabled {
				if skipped > 0 {
					app.status = fmt.Sprintf("查询历史已启用；%d 条超过 64 KiB 的 SQL 未保存。", skipped)
				} else {
					app.status = app.localized("查询历史将保存在本机设置文件中。")
				}
			} else {
				app.status = app.localized("查询历史仅保留到本次运行结束；磁盘上的历史已删除。")
			}
		})
	}()
}

func (app *workbench) shutdown() {
	if app.tray != nil {
		app.tray.Destroy()
		app.tray = nil
	}
	if !app.shuttingDown {
		if err := app.flushQueryDrafts(); err != nil {
			log.Printf("save SQL drafts on exit: %v", err)
		}
	}
	app.shuttingDown = true
	app.stopQueryDraftSaveTimer()
	app.cancelRedisTTLRefresh()
	if app.lifecycleCancel != nil {
		app.lifecycleCancel()
	}
	for _, cancel := range []context.CancelFunc{
		app.cancelQuery,
		app.cancelExport,
		app.cancelRedis,
		app.mongoCancel,
		app.etcdCancel,
		app.aiCancel,
	} {
		if cancel != nil {
			cancel()
		}
	}
	if app.layoutSaveTimer != nil {
		app.layoutSaveTimer.Stop()
	}
	if app.cursor != nil {
		_ = app.cursor.Close()
		app.cursor = nil
	}
	app.closeAllLiveConnections()
	_ = app.connectionManager.Disconnect()
}

func shortSQL(statement string) string {
	statement = strings.Join(strings.Fields(statement), " ")
	runes := []rune(statement)
	if len(runes) > 72 {
		statement = string(runes[:69]) + "…"
	}
	return statement
}
