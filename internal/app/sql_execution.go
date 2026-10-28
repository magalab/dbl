package app

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/magalab/dbl/internal/connection"
	"github.com/magalab/dbl/internal/preferences"

	"github.com/magalab/dbl/internal/sqlformat"

	"github.com/magalab/dbl/internal/sqlstream"
)

func (app *workbench) formatSQL() {
	formatted, err := sqlformat.Format(app.activeQuery.query.Buffer.String())
	if err != nil {
		app.reportError("格式化 SQL 失败", err)
		return
	}
	app.activeQuery.query.Buffer.Set(formatted)
	app.errorSummary, app.errorDetails, app.showErrorDetails = "", "", false
	app.status = app.localized("SQL 已格式化。")
}

func (app *workbench) executeSQL() {
	if app.busy || app.exporting || app.dialogOpen || app.queryHistorySaving {
		return
	}
	if app.activeQuery == nil {
		app.status = app.localized("请先新建查询工作区。")
		return
	}
	if app.store == nil {
		app.status = app.localized("请先打开数据库文件或连接数据库。")
		return
	}
	app.activeQuery.implicitQuery = false
	if !app.queryWorkspaceRequested {
		app.queryWorkspaceRequested = true
	}
	statement := strings.TrimSpace(app.activeQuery.query.Buffer.String())
	if statement == "" {
		app.status = app.localized("SQL 语句为空。")
		return
	}
	if app.aiDraftPending {
		if statement != strings.TrimSpace(app.aiDraftStatement) {
			app.aiDraftStatement, app.aiDraftApproved = statement, false
			app.status = app.localized("AI 草稿已编辑，请先重新审阅目标、语句与风险。")
			return
		}
		if !app.aiDraftApproved {
			app.status = app.localized("请先审阅 AI 草稿并明确确认执行。")
			return
		}
		app.aiDraftPending, app.aiDraftApproved = false, false
	}
	if requiresSQLConfirmation(statement, app.openedReadOnly) {
		approval := app.openedConnectionID + "\n" + statement
		if app.sqlConfirmationApproved != approval {
			if app.sqlConfirmationPending == approval {
				return
			}
			app.sqlConfirmationPending = approval
			connectionLabel, driverLabel, environment, selectedObject := app.openedLabel, app.openedDriver, environmentLabel(app.openedEnvironment), app.selectedObject
			riskClass := sqlRiskSummary(statement)
			risk := app.localized(riskClass)
			title := "SQL 写操作确认"
			message := "此语句将在当前数据库连接执行。请核对目标对象和影响范围。"
			if strings.HasPrefix(riskClass, "未知") {
				title = "SQL 执行确认"
				message = "无法确认此语句为只读操作。请检查自定义函数或其他潜在副作用。"
			}
			if app.openedEnvironment == preferences.EnvironmentProduction {
				title = "生产环境 SQL 确认"
			}
			if selectedObject == "" {
				selectedObject = app.localized("无当前选中对象；请从完整 SQL 核对影响对象")
			}
			app.requestConfirmation(
				title,
				message,
				fmt.Sprintf(app.localized("环境：%s\n驱动：%s\n连接：%s\n当前选中对象：%s\n类型估计：%s\n\nSQL：\n%s"), app.localized(environment), welcomeDriverLabel(string(driverLabel)), connectionLabel, selectedObject, risk, statement),
				[]string{"取消", "执行"}, 1,
				func(choice int) {
					app.sqlConfirmationPending = ""
					if choice != 1 {
						app.status = app.localized("已取消 SQL 执行。")
						if app.activeQuery != nil && app.activeQuery.schemaApplyPending {
							app.completeSchemaWorkspaceApply(app.activeQuery, false)
						}
						return
					}
					app.sqlConfirmationApproved = approval
					app.executeSQL()
				},
			)
			return
		}
		app.sqlConfirmationApproved = ""
	}
	if app.activeQuery.schemaApplyPending {
		app.executeSchemaDDL(statement)
		return
	}
	app.cancelResultCountForWorkspace(app.activeQuery)

	oldCursor, oldCancel := app.cursor, app.cancelQuery
	if oldCursor != nil {
		app.updateHistory(app.activeQuery.activeHistoryID, time.Time{}, "新查询已启动，停止后续读取")
	}
	if oldCancel != nil {
		oldCancel()
	}
	ctx, cancel := app.withTaskTimeout(queryTimeout)
	app.cancelQuery = cancel
	app.queryContext = ctx
	app.cursor = nil
	app.busy = true
	app.activeQuery.columns = nil
	app.activeQuery.resultSQL = statement
	app.activeQuery.resultPath = app.openedPath
	app.activeQuery.resultConnectionID = app.openedConnectionID
	app.activeQuery.resultTotal = 0
	app.activeQuery.resultTotalKnown = false
	app.activeQuery.resultTotalCounted = false
	app.activeQuery.resultCountCancelled = false
	app.activeQuery.resultWindow = sqlstream.Window{MaxRows: maxDisplayedRows, MaxBytes: maxRetainedResultBytes}
	app.activeQuery.resultPage = 0
	app.activeQuery.pageJumpInput = "1"
	app.activeQuery.streamStopped = false
	app.resetResultOrder()
	app.activeQuery.showHistory = false
	app.activeQuery.queryConnectionID = app.openedConnectionID
	app.activeQuery.queryNamespace = app.sqlNamespace
	app.status = app.localized("正在执行查询…")
	initialPageSize := resultWorkspacePageSize(app.activeQuery)
	started := time.Now()
	app.nextQueryHistoryID++
	app.activeQuery.history = append(app.activeQuery.history, queryRecord{ID: app.nextQueryHistoryID, SQL: statement, Started: started, Outcome: "执行中"})
	if len(app.activeQuery.history) > maxHistoryEntries {
		app.activeQuery.history = app.activeQuery.history[1:]
	}
	historyID := app.nextQueryHistoryID
	app.activeQuery.activeHistoryID = historyID
	app.persistQueryHistory()
	go func() {
		if oldCursor != nil {
			_ = oldCursor.Close()
		}
		cursor, err := app.store.StartQuery(ctx, statement)
		if err != nil {
			timedOut := errors.Is(ctx.Err(), context.DeadlineExceeded)
			cancelled := errors.Is(ctx.Err(), context.Canceled)
			cancel()
			app.window.Update(func() {
				app.busy = false
				app.cancelQuery = nil
				app.queryContext = nil
				if timedOut {
					app.status = fmt.Sprintf("查询超过 %s，已停止。", queryTimeout)
				} else if cancelled {
					app.status = app.localized("查询已取消。")
				} else {
					app.reportError("查询失败", err)
				}
				app.updateHistory(historyID, started, app.status)
			})
			return
		}
		columns := cursor.Columns()
		rows, done, err := cursor.NextBatch(initialPageSize)
		timedOut := errors.Is(ctx.Err(), context.DeadlineExceeded)
		cancelled := errors.Is(ctx.Err(), context.Canceled)
		terminal := done || err != nil || cancelled || timedOut
		if terminal {
			_ = cursor.Close()
			cancel()
		}
		app.window.Update(func() {
			app.busy = false
			app.activeQuery.columns = columns
			if app.activeQuery.dataView {
				app.activeQuery.dataResultColumns = append([]sqlstream.Column(nil), columns...)
			}
			app.appendResultRows(rows)
			if terminal {
				app.cursor = nil
				app.cancelQuery = nil
				app.queryContext = nil
				app.activeQuery.streamStopped = !done || err != nil || cancelled || timedOut
				if done && err == nil && !cancelled && !timedOut {
					app.activeQuery.resultTotal = app.activeQuery.resultWindow.Total
					app.activeQuery.resultTotalKnown = true
					app.activeQuery.resultTotalCounted = false
				}
				if timedOut {
					app.status = fmt.Sprintf("查询超过 %s，已停止 · 已读取 %d 行", queryTimeout, app.activeQuery.resultWindow.Total)
				} else if cancelled {
					app.status = fmt.Sprintf("查询已取消 · 已读取 %d 行", app.activeQuery.resultWindow.Total)
				} else if err != nil {
					app.reportError("查询失败", err)
				} else if len(columns) == 0 {
					app.status = app.localized("SQL 执行完成 · 此语句没有返回结果集")
				} else {
					app.status = fmt.Sprintf("查询完成 · %d 行", app.activeQuery.resultWindow.Total)
				}
			} else {
				app.cursor = cursor
				app.status = app.localized("查询结果还有后续页。")
			}
			app.updateHistory(historyID, started, app.status)
		})
	}()
}

func (app *workbench) fetchNextResultPage() {
	if app.busy || app.exporting || app.dialogOpen || app.activeQuery == nil || app.cursor == nil || app.queryContext == nil {
		return
	}
	workspace := app.activeQuery
	cursor, ctx, historyID := app.cursor, app.queryContext, workspace.activeHistoryID
	pageSize := resultWorkspacePageSize(workspace)
	currentPage := workspace.resultPage
	if workspace.dataView {
		currentPage = workspace.dataPage
	}
	targetPage := currentPage + 1
	rowsToRead := (targetPage+1)*pageSize - workspace.resultWindow.Len()
	if rowsToRead <= 0 {
		rowsToRead = pageSize
	}
	app.busy = true
	app.status = app.localized("正在加载下一页…")
	go func() {
		rows, done, err := cursor.NextBatch(rowsToRead)
		timedOut := errors.Is(ctx.Err(), context.DeadlineExceeded)
		cancelled := errors.Is(ctx.Err(), context.Canceled)
		terminal := done || err != nil || cancelled || timedOut
		if terminal {
			_ = cursor.Close()
		}
		app.window.Update(func() {
			app.busy = false
			if len(rows) > 0 {
				workspace.resultWindow.Append(rows)
				workspace.resultOrderDirty = true
			}
			if len(rows) > 0 {
				lastLoadedPage := resultPageCount(workspace.resultWindow.Len(), pageSize) - 1
				pageToShow := min(targetPage, lastLoadedPage)
				if workspace.dataView {
					workspace.dataPage = pageToShow
					workspace.dataSelectedRow = -1
				} else {
					workspace.resultPage = pageToShow
				}
				workspace.pageJumpInput = strconv.Itoa(pageToShow + 1)
			}
			if terminal {
				app.cursor = nil
				if app.cancelQuery != nil {
					app.cancelQuery()
				}
				app.cancelQuery = nil
				app.queryContext = nil
				workspace.streamStopped = !done || err != nil || cancelled || timedOut
				if done && err == nil && !cancelled && !timedOut {
					workspace.resultTotal = workspace.resultWindow.Total
					workspace.resultTotalKnown = true
					workspace.resultTotalCounted = false
				}
				if timedOut {
					app.status = fmt.Sprintf("读取结果超过 %s，已停止。", queryTimeout)
				} else if cancelled {
					app.status = app.localized("后续结果读取已取消。")
				} else if err != nil {
					app.reportError("查询失败", err)
				} else {
					app.status = fmt.Sprintf("查询完成 · 共读取 %d 行", workspace.resultWindow.Total)
				}
			} else {
				app.status = app.localized("查询结果还有后续页。")
			}
			app.updateHistory(historyID, time.Time{}, app.status)
		})
	}()
}

func (app *workbench) countRemainingResultRows() {
	if app.activeQuery == nil {
		return
	}
	workspace := app.activeQuery
	if app.resultCountWorkspace == workspace {
		app.cancelResultCountForWorkspace(workspace)
		return
	}
	if app.busy || app.exporting || app.dialogOpen || app.store == nil || app.resultCountWorkspace != nil ||
		workspace.resultConnectionID == "" || workspace.resultConnectionID != app.openedConnectionID ||
		workspace.resultTotalKnown || len(workspace.columns) == 0 || (workspace.dataView && (len(workspace.dataChanges) > 0 || workspace.dataCellEditing || workspace.dataInsertEditing)) {
		return
	}
	countStatement, ok := resultCountStatement(workspace.resultSQL)
	if !ok {
		app.status = app.localized("此查询不支持安全的总数统计。")
		return
	}
	store := app.store
	cancelable, supportsServerCancel := store.(connection.QueryCancellation)
	connectionID := workspace.resultConnectionID
	countedSQL := workspace.resultSQL
	historyID := workspace.activeHistoryID
	ctx, cancel := app.withTaskTimeout(queryTimeout)
	app.resultCountGeneration++
	generation := app.resultCountGeneration
	app.resultCountWorkspace = workspace
	app.cancelResultCount = cancel
	app.cancelResultCountServer = nil
	app.resultCountCancelRequested = false
	app.resultCountServerCancelReady = supportsServerCancel
	app.resultCountServerCancelSent = false
	app.resultCountCancelError = ""
	cancelSignal := &atomic.Bool{}
	app.resultCountCancelSignal = cancelSignal
	workspace.resultCountCancelled = false
	if app.activeQuery == workspace {
		app.status = app.localized("正在数据库中统计总数…")
	}
	go func() {
		defer cancel()
		var total int64
		registerServerCancel := func(serverCancel connection.QueryCancel) {
			app.window.Update(func() {
				if app.resultCountGeneration != generation || app.resultCountWorkspace != workspace {
					return
				}
				app.cancelResultCountServer = serverCancel
				if app.resultCountCancelRequested {
					app.cancelResultCountOnServer(workspace)
				}
			})
		}
		var cursor *sqlstream.Cursor
		var err error
		if supportsServerCancel {
			cursor, err = cancelable.StartQueryWithCancellation(ctx, countStatement, registerServerCancel)
		} else {
			cursor, err = store.StartQuery(ctx, countStatement)
		}
		if cursor == nil && err == nil {
			err = fmt.Errorf("count query returned no cursor")
		}
		if cursor != nil {
			if err == nil {
				var rows [][]any
				rows, _, err = cursor.NextBatch(1)
				if err == nil {
					if len(rows) != 1 || len(rows[0]) != 1 {
						err = fmt.Errorf("count query returned an unexpected result")
					} else {
						total, err = resultCountValue(rows[0][0])
					}
				}
				if err == nil {
					var extra [][]any
					var done bool
					extra, done, err = cursor.NextBatch(1)
					if err == nil && (len(extra) > 0 || !done) {
						err = fmt.Errorf("count query returned more than one row")
					}
				}
			}
			if closeErr := cursor.Close(); err == nil && closeErr != nil {
				err = closeErr
			}
		}
		timedOut := errors.Is(ctx.Err(), context.DeadlineExceeded)
		cancelled := errors.Is(ctx.Err(), context.Canceled) || cancelSignal.Load()
		app.window.Update(func() {
			if app.resultCountGeneration != generation || app.resultCountWorkspace != workspace {
				return
			}
			app.resultCountWorkspace = nil
			app.cancelResultCount = nil
			app.cancelResultCountServer = nil
			app.resultCountCancelRequested = false
			app.resultCountServerCancelReady = false
			app.resultCountServerCancelSent = false
			app.resultCountCancelSignal = nil
			resultStillCurrent := workspace.resultConnectionID == connectionID && workspace.resultSQL == countedSQL && workspace.activeHistoryID == historyID
			switch {
			case timedOut:
				if resultStillCurrent && app.activeQuery == workspace && !app.busy {
					app.status = fmt.Sprintf(app.localized("总数统计超过 %s，已停止。"), queryTimeout)
				}
			case cancelled:
				workspace.resultCountCancelled = true
				if resultStillCurrent && app.activeQuery == workspace && !app.busy {
					if app.resultCountCancelError != "" {
						app.status = fmt.Sprintf(app.localized("数据库端取消请求失败：%s"), app.resultCountCancelError)
					} else {
						app.status = app.localized("总数统计已取消。")
					}
				}
			case err != nil:
				if resultStillCurrent && app.activeQuery == workspace && app.openedConnectionID == connectionID && !app.busy {
					app.reportError("统计总数失败", err)
				}
			default:
				if !resultStillCurrent {
					return
				}
				workspace.resultTotal = total
				workspace.resultTotalKnown = true
				workspace.resultTotalCounted = true
				if app.activeQuery == workspace {
					app.status = fmt.Sprintf(app.localized("总数统计完成 · 共 %d 行"), total)
				}
			}
		})
	}()
}

func (app *workbench) cancelResultCountForWorkspace(workspace *queryWorkspace) {
	if workspace == nil || app.resultCountWorkspace != workspace {
		return
	}
	if app.resultCountCancelRequested {
		return
	}
	app.resultCountCancelRequested = true
	if app.resultCountCancelSignal != nil {
		app.resultCountCancelSignal.Store(true)
	}
	if !app.resultCountServerCancelReady && app.cancelResultCount != nil {
		app.cancelResultCount()
	}
	app.cancelResultCountOnServer(workspace)
	if app.activeQuery == workspace {
		app.status = app.localized("正在停止总数统计…")
	}
}

func (app *workbench) cancelResultCountOnServer(workspace *queryWorkspace) {
	if workspace == nil || app.resultCountWorkspace != workspace || !app.resultCountServerCancelReady || app.cancelResultCountServer == nil || app.resultCountServerCancelSent {
		return
	}
	serverCancel := app.cancelResultCountServer
	cancelQuery := app.cancelResultCount
	generation := app.resultCountGeneration
	app.resultCountServerCancelSent = true
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		err := serverCancel(ctx)
		cancel()
		if err == nil {
			// The server has acknowledged KILL QUERY. Only now cancel the local
			// context, so the driver cannot make the UI look finished while the
			// database is still working on the aggregate.
			if cancelQuery != nil {
				cancelQuery()
			}
			return
		}
		app.window.Update(func() {
			if app.resultCountGeneration != generation || app.resultCountWorkspace != workspace {
				return
			}
			app.resultCountCancelError = err.Error()
			app.resultCountServerCancelSent = false
			if app.activeQuery == workspace && !app.busy {
				app.status = fmt.Sprintf(app.localized("数据库端取消请求失败：%s"), app.resultCountCancelError)
			}
		})
	}()
}

func (app *workbench) cancelResultCountForConnection(connectionID string) {
	if app.resultCountWorkspace != nil && app.resultCountWorkspace.resultConnectionID == connectionID {
		app.cancelResultCountForWorkspace(app.resultCountWorkspace)
	}
}

// resultCountStatement wraps only a single SELECT already classified as a
// side-effect-free read. The original query is re-executed server-side, so
// unknown or mutating statements must never reach this path.
func resultCountStatement(statement string) (string, bool) {
	statement = strings.TrimSpace(statement)
	if statement == "" || !strings.HasPrefix(sqlRiskSummary(statement), "读取类") {
		return "", false
	}
	code := strings.TrimSpace(sqlRiskCode(statement))
	fields := strings.Fields(code)
	if len(fields) == 0 || !strings.EqualFold(fields[0], "SELECT") {
		return "", false
	}
	trimmedEnd := len(code) - 1
	if trimmedEnd >= 0 && code[trimmedEnd] == ';' {
		statement = statement[:trimmedEnd] + statement[trimmedEnd+1:]
		code = strings.TrimSpace(code[:trimmedEnd])
	}
	if strings.Contains(code, ";") {
		return "", false
	}
	return "SELECT COUNT(*) FROM (\n" + statement + "\n) AS dbl_result_count", true
}

func resultCountValue(value any) (int64, error) {
	var text string
	switch typed := value.(type) {
	case int:
		if typed < 0 {
			return 0, fmt.Errorf("count value is negative")
		}
		return int64(typed), nil
	case int32:
		if typed < 0 {
			return 0, fmt.Errorf("count value is negative")
		}
		return int64(typed), nil
	case int64:
		if typed < 0 {
			return 0, fmt.Errorf("count value is negative")
		}
		return typed, nil
	case uint:
		if uint64(typed) > uint64(^uint64(0)>>1) {
			return 0, fmt.Errorf("count value exceeds int64")
		}
		return int64(typed), nil
	case uint32:
		return int64(typed), nil
	case uint64:
		if typed > uint64(^uint64(0)>>1) {
			return 0, fmt.Errorf("count value exceeds int64")
		}
		return int64(typed), nil
	case []byte:
		text = string(typed)
	case string:
		text = typed
	default:
		text = fmt.Sprint(value)
	}
	total, err := strconv.ParseInt(strings.TrimSpace(text), 10, 64)
	if err != nil || total < 0 {
		if err == nil {
			err = fmt.Errorf("count value is negative")
		}
		return 0, fmt.Errorf("invalid count value: %w", err)
	}
	return total, nil
}

func (app *workbench) appendResultRows(rows [][]any) {
	if len(rows) == 0 {
		return
	}
	app.activeQuery.resultWindow.Append(rows)
	app.activeQuery.resultOrderDirty = true
}

func (app *workbench) resetResultOrder() {
	app.activeQuery.resultSort = ui.SortOrder{}
	app.activeQuery.resultOrder = nil
	app.activeQuery.resultOrderDirty = true
	app.activeQuery.lastSortColumn = ""
	app.activeQuery.lastSortDesc = false
}
