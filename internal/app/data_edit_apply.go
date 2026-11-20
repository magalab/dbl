package app

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/magalab/dbl/internal/connection"

	"github.com/magalab/dbl/internal/sqlstream"
)

func (app *workbench) commitDataChanges(workspace *queryWorkspace, onSaved func()) {
	if workspace == nil || len(workspace.dataChanges) == 0 || app.busy || app.exporting || app.dialogOpen {
		return
	}
	if !app.canEditDataWorkspace(workspace) {
		app.status = app.localized("当前连接不支持写入此数据表，或连接已切换。 ")
		return
	}
	editor, ok := app.tableEditorForWorkspace(workspace)
	if !ok {
		app.status = app.localized("当前连接不支持表格数据编辑。 ")
		return
	}
	changes, err := app.connectionTableChanges(workspace)
	if err != nil {
		app.reportError("准备表格修改失败", err)
		return
	}
	connectionID := workspace.dataConnectionID
	if app.skipTableChangeConfirmation {
		app.applyDataChanges(workspace, editor, connectionID, changes, onSaved)
		return
	}
	connectionLabel, driver, environment := app.openedLabel, app.openedDriver, app.localized(environmentLabel(app.openedEnvironment))
	if live, exists := app.liveConnections[connectionID]; exists {
		connectionLabel = live.profile.Name
		driver = live.connection.Driver()
		environment = app.localized(environmentLabel(live.profile.Environment))
	}
	detail := app.dataChangesReview(workspace)
	app.requestConfirmation(
		app.localized("提交表格修改"),
		fmt.Sprintf(app.localized("即将在连接‘%s’中提交 %d 项表格修改。请核对目标和变更内容。"), connectionLabel, len(changes)),
		fmt.Sprintf(app.localized("环境：%s\n驱动：%s\n连接：%s\n对象：%s\n事务：同一事务提交；发生错误时整批回滚\n\n%s"), environment, welcomeDriverLabel(string(driver)), connectionLabel, qualifiedObjectName(workspace.dataObject), detail),
		[]string{"取消", "提交"}, 1,
		func(choice int) {
			if choice != 1 {
				app.status = app.localized("已取消表格修改。")
				return
			}
			app.applyDataChanges(workspace, editor, connectionID, changes, onSaved)
		},
	)
}

func (app *workbench) applyDataChanges(workspace *queryWorkspace, editor connection.TableEditor, connectionID string, changes []connection.TableChange, onSaved func()) {
	if workspace == nil || workspace.dataConnectionID != connectionID || !app.canEditDataWorkspace(workspace) {
		app.status = app.localized("连接已切换，表格修改没有提交。 ")
		return
	}
	app.busy = true
	app.status = app.localized("正在提交表格修改…")
	ctx, cancel := app.withTaskTimeout(25 * time.Second)
	var writeCursor *sqlstream.Cursor
	var stopReading func()
	if app.openedConnectionID == connectionID && app.cursor != nil {
		writeCursor = app.cursor
		stopReading = app.cancelQuery
		if app.activeQuery != nil {
			app.activeQuery.streamStopped = true
		}
		app.cursor, app.cancelQuery, app.queryContext = nil, nil, nil
	}
	go func() {
		defer cancel()
		if stopReading != nil {
			stopReading()
		}
		var applyErr error
		var cursorCloseErr error
		if writeCursor != nil {
			cursorCloseErr = writeCursor.Close()
			applyErr = cursorCloseErr
		}
		if applyErr == nil {
			applyErr = editor.ApplyTableChanges(ctx, workspace.dataObject, dataPrimaryKey(workspace), changes)
		}
		app.window.Update(func() {
			app.busy = false
			if applyErr != nil {
				if cursorCloseErr != nil {
					app.reportError("停止查询读取失败，未提交表格修改", applyErr)
				} else {
					app.reportError("提交表格修改失败", applyErr)
				}
				return
			}
			workspace.dataChanges = nil
			workspace.dataCellEditing = false
			workspace.dataInsertEditing = false
			workspace.dataEditOriginal = nil
			workspace.dataInsertDraft = nil
			workspace.dataInsertIncluded = nil
			workspace.dataInsertNull = nil
			workspace.pendingDataRefresh = true
			app.status = app.localized("表格修改已提交。")
			if onSaved != nil {
				onSaved()
				return
			}
			if app.activeQuery == workspace && app.openedConnectionID == connectionID {
				app.refreshDataWorkspace()
			}
		})
	}()
}

func (app *workbench) connectionTableChanges(workspace *queryWorkspace) ([]connection.TableChange, error) {
	changes := make([]connection.TableChange, 0, len(workspace.dataChanges))
	primaryKey := dataPrimaryKey(workspace)
	for _, staged := range workspace.dataChanges {
		change := connection.TableChange{Kind: staged.kind, Values: make(map[string]any), Original: make(map[string]any)}
		if staged.kind == connection.InsertRow {
			for name, value := range staged.values {
				change.Values[name] = value
			}
			changes = append(changes, change)
			continue
		}
		resultColumns := dataWorkspaceResultColumns(workspace)
		if len(staged.original) != len(resultColumns) {
			return nil, fmt.Errorf("row %d is no longer available in the result window", staged.sourceRow+1)
		}
		keySet := make(map[string]bool, len(primaryKey))
		for _, key := range primaryKey {
			keySet[key] = true
		}
		for index, column := range resultColumns {
			if keySet[column.Name] {
				change.Original[column.Name] = staged.original[index]
			}
		}
		if staged.kind == connection.UpdateRow {
			for name, value := range staged.values {
				change.Values[name] = value
			}
		}
		if staged.kind != connection.UpdateRow && staged.kind != connection.DeleteRow {
			return nil, fmt.Errorf("unsupported table change type %d", staged.kind)
		}
		if len(primaryKey) == 0 {
			return nil, fmt.Errorf("table %s has no primary key", qualifiedObjectName(workspace.dataObject))
		}
		changes = append(changes, change)
	}
	if len(changes) > connection.MaxTableChangeBatch {
		return nil, fmt.Errorf("at most %d row changes can be submitted together", connection.MaxTableChangeBatch)
	}
	return changes, nil
}

func (app *workbench) dataChangesReview(workspace *queryWorkspace) string {
	counts := map[connection.TableChangeKind]int{}
	for _, change := range workspace.dataChanges {
		counts[change.kind]++
	}
	lines := []string{
		fmt.Sprintf(app.localized("新增：%d 行；修改：%d 行；删除：%d 行"), counts[connection.InsertRow], counts[connection.UpdateRow], counts[connection.DeleteRow]),
		app.localized("变更预览："),
	}
	for index, change := range workspace.dataChanges {
		if index >= 12 {
			lines = append(lines, fmt.Sprintf(app.localized("… 其余 %d 项未展开"), len(workspace.dataChanges)-index))
			break
		}
		kind := app.localized(map[connection.TableChangeKind]string{connection.InsertRow: "新增", connection.UpdateRow: "修改", connection.DeleteRow: "删除"}[change.kind])
		parts := make([]string, 0, len(change.values))
		if change.kind == connection.DeleteRow {
			keySet := make(map[string]bool, len(dataPrimaryKey(workspace)))
			for _, key := range dataPrimaryKey(workspace) {
				keySet[key] = true
			}
			for index, column := range dataWorkspaceResultColumns(workspace) {
				if index < len(change.original) && keySet[column.Name] {
					parts = append(parts, column.Name+"="+truncateText(dataReviewValueForColumn(workspace, column.Name, change.original[index]), 120))
				}
			}
		} else if change.kind == connection.UpdateRow {
			keySet := make(map[string]bool, len(dataPrimaryKey(workspace)))
			for _, key := range dataPrimaryKey(workspace) {
				keySet[key] = true
			}
			keyParts := make([]string, 0, len(keySet))
			for columnIndex, column := range dataWorkspaceResultColumns(workspace) {
				if columnIndex < len(change.original) && keySet[column.Name] {
					keyParts = append(keyParts, column.Name+"="+truncateText(dataReviewValueForColumn(workspace, column.Name, change.original[columnIndex]), 80))
				}
			}
			sort.Strings(keyParts)
			parts = append(parts, "WHERE "+strings.Join(keyParts, " AND "))
			for name, value := range change.values {
				old := "?"
				for columnIndex, column := range dataWorkspaceResultColumns(workspace) {
					if column.Name == name && columnIndex < len(change.original) {
						old = dataReviewValueForColumn(workspace, name, change.original[columnIndex])
						break
					}
				}
				parts = append(parts, name+": "+truncateText(old, 80)+" → "+truncateText(dataReviewValueForColumn(workspace, name, value), 80))
			}
		} else {
			for name, value := range change.values {
				parts = append(parts, name+"="+truncateText(dataReviewValueForColumn(workspace, name, value), 120))
			}
		}
		sort.Strings(parts)
		if len(parts) == 0 {
			lines = append(lines, fmt.Sprintf(app.localized("%d. %s（使用数据库默认值）"), index+1, kind))
		} else {
			lines = append(lines, fmt.Sprintf(app.localized("%d. %s：%s"), index+1, kind, strings.Join(parts, ", ")))
		}
	}
	return truncateText(strings.Join(lines, "\n"), 6000)
}

func dataReviewValue(value any) string {
	if value == nil {
		return "NULL"
	}
	if text, ok := value.(string); ok {
		return strconv.Quote(text)
	}
	return fmt.Sprint(value)
}

func dataReviewValueForColumn(workspace *queryWorkspace, name string, value any) string {
	if _, ok := value.([]byte); !ok {
		return dataReviewValue(value)
	}
	for index, column := range dataWorkspaceResultColumns(workspace) {
		if column.Name != name {
			continue
		}
		formatted := formatCell(value, resultColumnDatabaseType(workspace, index))
		if strings.HasPrefix(formatted, "base64:") {
			return formatted
		}
		return strconv.Quote(formatted)
	}
	return dataReviewValue(value)
}

func (app *workbench) confirmCloseDataWorkspace(index int, workspace *queryWorkspace) {
	if app.dialogOpen || app.busy || app.exporting || index < 0 || index >= len(app.workspaces) || app.workspaces[index] != workspace {
		return
	}
	app.requestConfirmation(
		app.localized("关闭数据工作区"),
		fmt.Sprintf(app.localized("此数据工作区有 %d 项未提交修改。关闭前要怎样处理？"), len(workspace.dataChanges)),
		"",
		[]string{"继续编辑", "丢弃并关闭", "保存并关闭"}, 2,
		func(choice int) {
			if choice == 1 {
				workspace.dataChanges = nil
				workspace.dataCellEditing = false
				workspace.dataEditOriginal = nil
				workspace.dataInsertEditing = false
				workspace.dataInsertDraft = nil
				workspace.dataInsertIncluded = nil
				workspace.dataInsertNull = nil
				app.closeWorkspace(index)
				return
			}
			if choice == 2 {
				app.commitDataChanges(workspace, func() {
					if index >= 0 && index < len(app.workspaces) && app.workspaces[index] == workspace {
						app.closeWorkspace(index)
					}
				})
			}
		},
	)
}
