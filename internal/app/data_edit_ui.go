package app

import (
	"fmt"
	"sort"

	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/magalab/dbl/internal/connection"
)

func (app *workbench) dataEditToolbar(c *ui.Context, workspace *queryWorkspace) {
	editable := app.canEditDataWorkspace(workspace)
	if workspace.dataCellEditing {
		isNull := workspace.dataEditNull
		null := ui.Checkbox(c, &isNull, app.localized("NULL"))
		null.Disabled(!editable)
		if null.Changed() {
			workspace.dataEditNull = isNull
		}
		apply := iconPrimaryButton(c, app.localized("应用单元格修改"), actionIcons["check"])
		apply.Size(32, 32).Padding(5)
		apply.Disabled(!editable)
		if apply.Clicked() {
			app.applyDataCellEdit(workspace)
		}
		cancel := iconButton(c, app.localized("取消单元格修改"), actionIcons["close"])
		cancel.Size(32, 32).Padding(5).Tooltip(app.localized("按 Esc 取消单元格编辑"))
		if cancel.Clicked() {
			app.cancelDataCellEdit(workspace)
		}
	}
	add := iconButton(c, app.localized("新增行"), actionIcons["add"])
	add.Size(32, 32).Padding(5)
	add.Disabled(!editable || !dataWorkspaceColumnsReady(workspace) || workspace.dataInsertEditing || len(workspace.dataChanges) >= connection.MaxTableChangeBatch)
	if add.Clicked() {
		workspace.dataInsertEditing = true
		workspace.dataInsertDraft = make(map[int]string)
		workspace.dataInsertIncluded = make(map[int]bool)
		workspace.dataInsertNull = make(map[int]bool)
		workspace.dataPage = workspace.resultWindow.Len() / resultWorkspacePageSize(workspace)
		workspace.dataSelectedRow = -1
	}
	selectedSourceRow, selectedValues, hasSelection := app.selectedDataRow(workspace)
	selectedChange := (*dataRowChange)(nil)
	if hasSelection {
		selectedChange = findDataRowChange(workspace, selectedSourceRow)
	}
	deleteLabel := "删除行"
	deleteIcon := actionIcons["trash"]
	if selectedChange != nil && selectedChange.kind == connection.DeleteRow {
		deleteLabel = "取消标记删除"
		deleteIcon = actionIcons["undo"]
	}
	deleteRow := iconButton(c, app.localized(deleteLabel), deleteIcon)
	deleteRow.Size(32, 32).Padding(5).TextColor(c.Theme().Danger)
	deleteRow.Tooltip(app.localized(deleteLabel))
	deleteRow.Disabled(!editable || !hasSelection || !dataRowHasPrimaryKeyValues(workspace, selectedValues) || (len(workspace.dataChanges) >= connection.MaxTableChangeBatch && selectedChange == nil))
	if deleteRow.Clicked() {
		if selectedChange != nil && selectedChange.kind == connection.DeleteRow {
			app.unstageDeleteDataRow(workspace, selectedSourceRow)
		} else {
			app.stageDeleteDataRow(workspace, selectedSourceRow)
		}
	}
	if count := len(workspace.dataChanges); count > 0 {
		ui.Spacer(c)
		saveLabel := fmt.Sprintf(app.localized("提交修改 (%d)"), count)
		save := iconPrimaryButton(c, saveLabel, actionIcons["save"])
		save.Size(32, 32).Padding(5)
		save.Disabled(!editable)
		if save.Clicked() {
			app.saveDataWorkspaceChanges(workspace)
		}
		ui.Text(c, dataSaveShortcutLabel()).TextColor(c.Theme().TextMuted).Tooltip(app.localized("快捷键保存"))
		discard := iconButton(c, app.localized("回滚"), actionIcons["undo"])
		discard.Size(32, 32).Padding(5).Tooltip(app.localized("撤销所有尚未提交的修改"))
		discard.Disabled(app.busy || app.dialogOpen || app.exporting)
		if discard.Clicked() {
			workspace.dataChanges = nil
			workspace.dataCellEditing = false
			workspace.dataEditOriginal = nil
			workspace.dataInsertEditing = false
			workspace.dataInsertDraft = nil
			workspace.dataInsertIncluded = nil
			workspace.dataInsertNull = nil
			app.status = app.localized("已丢弃待提交的数据修改。")
		}
	}
}

func (app *workbench) dataInsertToolbar(c *ui.Context, workspace *queryWorkspace) {
	if workspace == nil || !workspace.dataInsertEditing {
		return
	}
	ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
		ui.Spacer(c)
		stage := iconPrimaryButton(c, app.localized("加入待提交修改"), actionIcons["check"])
		stage.Size(32, 32).Padding(5)
		stage.Disabled(!app.canEditDataWorkspace(workspace) || len(workspace.dataChanges) >= connection.MaxTableChangeBatch)
		if stage.Clicked() {
			app.stageDataInsert(workspace)
		}
		ui.Text(c, dataSaveShortcutLabel()).TextColor(c.Theme().TextMuted).Tooltip(app.localized("快捷键保存"))
		cancel := iconButton(c, app.localized("取消新增行"), actionIcons["close"])
		cancel.Size(32, 32).Padding(5)
		if cancel.Clicked() {
			workspace.dataInsertEditing = false
			workspace.dataInsertDraft = nil
			workspace.dataInsertIncluded = nil
			workspace.dataInsertNull = nil
		}
	})
}

func dataSaveShortcutLabel() string {
	if ui.Cmd == ui.Super {
		return "⌘S"
	}
	return "Ctrl+S"
}

func (app *workbench) dataInsertCell(c *ui.Context, workspace *queryWorkspace, tableColumn int) {
	if tableColumn == 0 {
		ui.Text(c, app.localized("新行")).SingleLine().TextColor(c.Theme().TextMuted)
		return
	}
	columnIndex := tableColumn - 1
	if columnIndex < 0 || columnIndex >= len(dataWorkspaceResultColumns(workspace)) {
		ui.Text(c, "—").SingleLine()
		return
	}
	column, ok := dataColumnInfoForResult(workspace, columnIndex)
	if !ok || !dataColumnEditable(column) {
		ui.Text(c, app.localized("自动生成")).SingleLine().TextColor(c.Theme().TextMuted)
		return
	}
	draftIndex := -1
	for index, dataColumn := range workspace.dataColumns {
		if dataColumn.Name == column.Name {
			draftIndex = index
			break
		}
	}
	if draftIndex < 0 {
		ui.Text(c, app.localized("不可编辑")).SingleLine().TextColor(c.Theme().TextMuted)
		return
	}
	if workspace.dataInsertNull[draftIndex] {
		setValue := ui.Button(c, "NULL").Tooltip(app.localized("点击改回默认值；右键可选择单元格状态"))
		setValue.Disabled(!app.canEditDataWorkspace(workspace))
		if setValue.Clicked() {
			workspace.dataInsertNull[draftIndex] = false
			workspace.dataInsertIncluded[draftIndex] = false
		}
		setValue.ContextMenu(func(menu *ui.Menu) {
			if menu.Item(app.localized("使用数据库默认值")).Chosen() {
				workspace.dataInsertNull[draftIndex] = false
				workspace.dataInsertIncluded[draftIndex] = false
			}
		})
		return
	}
	value := workspace.dataInsertDraft[draftIndex]
	input := ui.TextInput(c, &value).Placeholder(app.localized("默认值")).Label(column.Name)
	input.Disabled(!app.canEditDataWorkspace(workspace))
	if input.Changed() {
		workspace.dataInsertDraft[draftIndex] = value
		workspace.dataInsertIncluded[draftIndex] = true
		workspace.dataInsertNull[draftIndex] = false
	}
	input.ContextMenu(func(menu *ui.Menu) {
		if menu.Item(app.localized("写入 NULL")).Disabled(!app.canEditDataWorkspace(workspace)).Chosen() {
			workspace.dataInsertDraft[draftIndex] = ""
			workspace.dataInsertIncluded[draftIndex] = true
			workspace.dataInsertNull[draftIndex] = true
		}
		if menu.Item(app.localized("使用数据库默认值")).Chosen() {
			workspace.dataInsertDraft[draftIndex] = ""
			workspace.dataInsertIncluded[draftIndex] = false
			workspace.dataInsertNull[draftIndex] = false
		}
	})
}

func (app *workbench) dataChangesPanel(c *ui.Context, workspace *queryWorkspace) {
	if workspace == nil || len(workspace.dataChanges) == 0 {
		return
	}
	ui.Column(c).FillWidth().Padding(8, 12).Gap(4).Background(c.Theme().Surface).Border(1, c.Theme().Border).Radius(c.Theme().Radius).Children(func() {
		ui.Text(c, app.localized("待提交的行修改")).Bold()
		visible := workspace.dataChanges
		if len(visible) > 8 {
			visible = visible[:8]
		}
		for index, change := range visible {
			index, change := index, change
			label := app.localized(map[connection.TableChangeKind]string{
				connection.InsertRow: "新增",
				connection.UpdateRow: "修改",
				connection.DeleteRow: "删除",
			}[change.kind])
			if change.kind != connection.InsertRow {
				label += fmt.Sprintf(app.localized(" · 源行 %d"), change.sourceRow+1)
			}
			parts := make([]string, 0, len(change.values))
			for name, value := range change.values {
				parts = append(parts, name+"="+truncateText(dataReviewValue(value), 80))
			}
			if change.kind == connection.DeleteRow {
				for columnIndex, column := range dataWorkspaceResultColumns(workspace) {
					if columnIndex < len(change.original) {
						for _, key := range dataPrimaryKey(workspace) {
							if column.Name == key {
								parts = append(parts, key+"="+truncateText(dataReviewValue(change.original[columnIndex]), 80))
							}
						}
					}
				}
			}
			sort.Strings(parts)
			detail := strings.Join(parts, " · ")
			if detail == "" && change.kind == connection.InsertRow {
				detail = app.localized("使用数据库默认值")
			}
			ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Text(c, label).SingleLine()
				ui.Text(c, detail).TextColor(c.Theme().TextMuted).SingleLine().Grow(1)
				undo := ui.Button(c, app.localized("取消本项修改"))
				undo.Disabled(app.busy || app.exporting || app.dialogOpen)
				if undo.Clicked() {
					if change.kind == connection.DeleteRow && len(change.deletedRowValues) > 0 {
						app.unstageDeleteDataRow(workspace, change.sourceRow)
					} else {
						workspace.dataChanges = append(workspace.dataChanges[:index], workspace.dataChanges[index+1:]...)
						app.status = app.localized("已取消一项待提交修改。")
					}
				}
			})
		}
		if remaining := len(workspace.dataChanges) - len(visible); remaining > 0 {
			ui.Text(c, fmt.Sprintf(app.localized("另有 %d 项修改未展开。"), remaining)).TextColor(c.Theme().TextMuted)
		}
	})
}
