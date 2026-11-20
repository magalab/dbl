package app

import (
	"fmt"
	"sort"

	"strings"
	"time"

	"github.com/magalab/dbl/internal/connection"
	"github.com/magalab/dbl/internal/safeerror"
	"github.com/magalab/dbl/internal/sqlstream"
)

func (app *workbench) loadDataWorkspaceColumns(workspace *queryWorkspace) {
	if workspace == nil || app.store == nil || workspace.dataColumnsLoading || (workspace.dataColumnsFor == app.openedConnectionID && workspace.dataColumnsError == "") {
		return
	}
	store, connectionID := app.store, app.openedConnectionID
	workspace.dataColumnsFor = connectionID
	workspace.dataColumnsLoading = true
	workspace.dataColumnsError = ""
	object := workspace.dataObject
	go func() {
		ctx, cancel := app.withTaskTimeout(8 * time.Second)
		defer cancel()
		columns, err := store.Columns(ctx, object.Schema, object.Name)
		app.window.Update(func() {
			if workspace.dataColumnsFor != connectionID {
				return
			}
			workspace.dataColumnsLoading = false
			if app.store != store || app.openedConnectionID != connectionID {
				workspace.dataColumnsFor = ""
				return
			}
			if err != nil {
				workspace.dataColumnsError = safeerror.Display(err)
				return
			}
			workspace.dataColumns = columns
			workspace.dataColumnsError = ""
		})
	}()
}

func dataPrimaryKey(workspace *queryWorkspace) []string {
	if workspace == nil {
		return nil
	}
	keys := make([]string, 0)
	for _, column := range workspace.dataColumns {
		if column.PrimaryKey {
			keys = append(keys, column.Name)
		}
	}
	sort.Strings(keys)
	return keys
}

func dataColumnInfoForResult(workspace *queryWorkspace, resultColumn int) (connection.Column, bool) {
	columns := dataWorkspaceResultColumns(workspace)
	if workspace == nil || resultColumn < 0 || resultColumn >= len(columns) {
		return connection.Column{}, false
	}
	name := columns[resultColumn].Name
	for _, column := range workspace.dataColumns {
		if column.Name == name {
			return column, true
		}
	}
	return connection.Column{}, false
}

func resultColumnDatabaseType(workspace *queryWorkspace, resultColumn int) string {
	if workspace == nil || resultColumn < 0 {
		return ""
	}
	if workspace.dataView {
		if column, ok := dataColumnInfoForResult(workspace, resultColumn); ok && strings.TrimSpace(column.Type) != "" {
			return column.Type
		}
		columns := dataWorkspaceResultColumns(workspace)
		if resultColumn < len(columns) {
			return columns[resultColumn].DatabaseType
		}
		return ""
	}
	if resultColumn < len(workspace.columns) {
		return workspace.columns[resultColumn].DatabaseType
	}
	return ""
}

func dataColumnEditable(column connection.Column) bool {
	if column.Hidden != 0 {
		return false
	}
	typeName := strings.ToLower(column.Type)
	for _, binaryType := range []string{"bytea", "blob", "binary", "image"} {
		if strings.Contains(typeName, binaryType) {
			return false
		}
	}
	return true
}

func dataWorkspaceResultColumns(workspace *queryWorkspace) []sqlstream.Column {
	if workspace == nil {
		return nil
	}
	if len(workspace.dataResultColumns) > 0 {
		return workspace.dataResultColumns
	}
	return workspace.columns
}

func dataHasVisiblePrimaryKey(workspace *queryWorkspace) bool {
	keys := dataPrimaryKey(workspace)
	if len(keys) == 0 {
		return false
	}
	for _, key := range keys {
		found := false
		for _, column := range dataWorkspaceResultColumns(workspace) {
			if column.Name == key {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func dataRowHasPrimaryKeyValues(workspace *queryWorkspace, row []any) bool {
	if !dataHasVisiblePrimaryKey(workspace) {
		return false
	}
	columns := dataWorkspaceResultColumns(workspace)
	for _, key := range dataPrimaryKey(workspace) {
		found := false
		for index, column := range columns {
			if column.Name == key {
				if index >= len(row) || row[index] == nil {
					return false
				}
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func (app *workbench) tableEditorForWorkspace(workspace *queryWorkspace) (connection.TableEditor, bool) {
	if workspace == nil || workspace.dataConnectionID == "" {
		return nil, false
	}
	if workspace.dataConnectionID == app.openedConnectionID && app.store != nil {
		if editor, ok := app.store.(connection.TableEditor); ok {
			return editor, true
		}
	}
	if live, ok := app.liveConnections[workspace.dataConnectionID]; ok && live.connection != nil {
		if editor, ok := live.connection.(connection.TableEditor); ok {
			return editor, true
		}
	}
	return nil, false
}

func (app *workbench) dataWorkspaceReadOnly(workspace *queryWorkspace) bool {
	if workspace == nil || workspace.dataConnectionID == "" {
		return true
	}
	if workspace.dataConnectionID == app.openedConnectionID {
		return app.openedReadOnly
	}
	if live, ok := app.liveConnections[workspace.dataConnectionID]; ok {
		return live.profile.ReadOnly
	}
	return true
}

func (app *workbench) canEditDataWorkspace(workspace *queryWorkspace) bool {
	if workspace == nil || workspace.dataObject.Type != "table" || app.busy || app.exporting || app.dialogOpen || app.dataWorkspaceReadOnly(workspace) {
		return false
	}
	_, ok := app.tableEditorForWorkspace(workspace)
	return ok
}

func dataWorkspaceColumnsReady(workspace *queryWorkspace) bool {
	return workspace != nil && !workspace.dataColumnsLoading && workspace.dataColumnsError == "" && workspace.dataColumnsFor == workspace.dataConnectionID && len(workspace.dataColumns) > 0
}

func (app *workbench) selectedDataRow(workspace *queryWorkspace) (int64, []any, bool) {
	if workspace == nil || workspace.dataSelectedRow < 0 {
		return 0, nil, false
	}
	if app.activeQuery == workspace {
		app.updateResultOrder()
	}
	pageStart := dataWorkspacePageStart(workspace)
	pageEnd := min(pageStart+resultWorkspacePageSize(workspace), workspace.resultWindow.Len())
	if pageStart >= pageEnd || workspace.dataSelectedRow >= pageEnd-pageStart {
		return 0, nil, false
	}
	sourceIndex := pageStart + workspace.dataSelectedRow
	if len(workspace.resultOrder) > 0 {
		if sourceIndex >= len(workspace.resultOrder) {
			return 0, nil, false
		}
		sourceIndex = workspace.resultOrder[sourceIndex]
	}
	if sourceIndex < 0 || sourceIndex >= workspace.resultWindow.Len() {
		return 0, nil, false
	}
	return workspace.resultWindow.SourceRow(sourceIndex), workspace.resultWindow.Row(sourceIndex), true
}

func (app *workbench) saveDataWorkspaceChanges(workspace *queryWorkspace) {
	if workspace == nil || !workspace.dataView || app.busy || app.exporting || app.dialogOpen {
		return
	}
	if workspace.dataCellEditing {
		app.applyDataCellEdit(workspace)
		if workspace.dataCellEditing {
			return
		}
	}
	if workspace.dataInsertEditing {
		app.stageDataInsert(workspace)
		if workspace.dataInsertEditing {
			return
		}
	}
	if len(workspace.dataChanges) == 0 {
		app.status = app.localized("没有待提交的数据修改。")
		return
	}
	app.commitDataChanges(workspace, nil)
}

func (app *workbench) displayedDataCell(workspace *queryWorkspace, sourceRow int64, column int, original any) any {
	if change := findDataRowChange(workspace, sourceRow); change != nil && change.kind == connection.UpdateRow {
		columns := dataWorkspaceResultColumns(workspace)
		if column >= 0 && column < len(columns) {
			if value, ok := change.values[columns[column].Name]; ok {
				return value
			}
		}
	}
	return original
}

func findDataRowChange(workspace *queryWorkspace, sourceRow int64) *dataRowChange {
	for index := range workspace.dataChanges {
		change := &workspace.dataChanges[index]
		if change.kind != connection.InsertRow && change.sourceRow == sourceRow {
			return change
		}
	}
	return nil
}

func (app *workbench) beginDataCellEdit(workspace *queryWorkspace, sourceRow int64, column int, original any) {
	if !app.canEditDataWorkspace(workspace) {
		return
	}
	if change := findDataRowChange(workspace, sourceRow); change != nil && change.kind == connection.DeleteRow {
		return
	}
	value := app.displayedDataCell(workspace, sourceRow, column, original)
	sourceIndex := app.sourceIndexForRow(workspace, sourceRow)
	if sourceIndex < 0 || !dataRowHasPrimaryKeyValues(workspace, workspace.resultWindow.Row(sourceIndex)) {
		return
	}
	workspace.dataCellEditing = true
	workspace.dataEditSourceRow = sourceRow
	workspace.dataEditColumn = column
	workspace.dataEditDraft = formatCell(value, resultColumnDatabaseType(workspace, column))
	workspace.dataEditNull = value == nil
	workspace.dataEditOriginal = nil
	if sourceIndex >= 0 {
		workspace.dataEditOriginal = append([]any(nil), workspace.resultWindow.Row(sourceIndex)...)
	}
}

func (app *workbench) cancelDataCellEdit(workspace *queryWorkspace) {
	if workspace == nil || !workspace.dataCellEditing {
		return
	}
	workspace.dataCellEditing = false
	workspace.dataEditOriginal = nil
}

func (app *workbench) applyDataCellEdit(workspace *queryWorkspace) {
	if workspace == nil || !workspace.dataCellEditing {
		return
	}
	if findDataRowChange(workspace, workspace.dataEditSourceRow) == nil && len(workspace.dataChanges) >= connection.MaxTableChangeBatch {
		app.status = fmt.Sprintf(app.localized("每批最多暂存 %d 项修改。"), connection.MaxTableChangeBatch)
		return
	}
	sourceIndex := app.sourceIndexForRow(workspace, workspace.dataEditSourceRow)
	row := workspace.dataEditOriginal
	if sourceIndex >= 0 {
		row = workspace.resultWindow.Row(sourceIndex)
	}
	if len(row) == 0 {
		workspace.dataCellEditing = false
		return
	}
	columns := dataWorkspaceResultColumns(workspace)
	if workspace.dataEditColumn < 0 || workspace.dataEditColumn >= len(row) || workspace.dataEditColumn >= len(columns) {
		workspace.dataCellEditing = false
		return
	}
	change := findDataRowChange(workspace, workspace.dataEditSourceRow)
	if change == nil {
		change = &dataRowChange{kind: connection.UpdateRow, sourceRow: workspace.dataEditSourceRow, original: append([]any(nil), row...), values: make(map[string]any)}
		workspace.dataChanges = append(workspace.dataChanges, *change)
		change = &workspace.dataChanges[len(workspace.dataChanges)-1]
	}
	if change.kind != connection.UpdateRow {
		workspace.dataCellEditing = false
		return
	}
	original := row[workspace.dataEditColumn]
	var value any = workspace.dataEditDraft
	if workspace.dataEditNull {
		value = nil
	}
	if sameDataValue(original, value, resultColumnDatabaseType(workspace, workspace.dataEditColumn)) {
		delete(change.values, columns[workspace.dataEditColumn].Name)
	} else {
		change.values[columns[workspace.dataEditColumn].Name] = value
	}
	if len(change.values) == 0 {
		removeDataRowChange(workspace, workspace.dataEditSourceRow)
	}
	workspace.dataCellEditing = false
	workspace.dataEditOriginal = nil
	if len(workspace.dataChanges) > connection.MaxTableChangeBatch {
		workspace.dataChanges = workspace.dataChanges[:connection.MaxTableChangeBatch]
	}
	app.status = app.localized("单元格修改已暂存；提交前不会写入数据库。")
}

func sameDataValue(original, edited any, databaseType string) bool {
	if original == nil || edited == nil {
		return original == nil && edited == nil
	}
	return formatCell(original, databaseType) == formatCell(edited, databaseType)
}

func removeDataRowChange(workspace *queryWorkspace, sourceRow int64) {
	for index := range workspace.dataChanges {
		if workspace.dataChanges[index].kind != connection.InsertRow && workspace.dataChanges[index].sourceRow == sourceRow {
			workspace.dataChanges = append(workspace.dataChanges[:index], workspace.dataChanges[index+1:]...)
			return
		}
	}
}

func (app *workbench) sourceIndexForRow(workspace *queryWorkspace, sourceRow int64) int {
	for index := 0; index < workspace.resultWindow.Len(); index++ {
		if workspace.resultWindow.SourceRow(index) == sourceRow {
			return index
		}
	}
	return -1
}

func (app *workbench) stageDataInsert(workspace *queryWorkspace) {
	if workspace == nil || !workspace.dataInsertEditing {
		return
	}
	values := make(map[string]any)
	for index, included := range workspace.dataInsertIncluded {
		if !included || index < 0 || index >= len(workspace.dataColumns) {
			continue
		}
		name := workspace.dataColumns[index].Name
		if workspace.dataInsertNull[index] {
			values[name] = nil
		} else {
			values[name] = workspace.dataInsertDraft[index]
		}
	}
	if len(workspace.dataChanges) >= connection.MaxTableChangeBatch {
		app.status = fmt.Sprintf(app.localized("每批最多暂存 %d 项修改。"), connection.MaxTableChangeBatch)
		return
	}
	workspace.dataChanges = append(workspace.dataChanges, dataRowChange{kind: connection.InsertRow, values: values})
	workspace.dataInsertEditing = false
	workspace.dataInsertDraft = nil
	workspace.dataInsertIncluded = nil
	workspace.dataInsertNull = nil
	app.status = app.localized("新增行已加入待提交修改。")
}

func (app *workbench) dataInsertHasDraft(workspace *queryWorkspace) bool {
	if workspace == nil || !workspace.dataInsertEditing {
		return false
	}
	for _, included := range workspace.dataInsertIncluded {
		if included {
			return true
		}
	}
	return false
}

func (app *workbench) stageDeleteDataRow(workspace *queryWorkspace, sourceRow int64) {
	if !app.canEditDataWorkspace(workspace) {
		return
	}
	sourceIndex := app.sourceIndexForRow(workspace, sourceRow)
	if sourceIndex < 0 {
		return
	}
	row := workspace.resultWindow.Row(sourceIndex)
	if !dataRowHasPrimaryKeyValues(workspace, row) {
		return
	}
	change := findDataRowChange(workspace, sourceRow)
	if change == nil {
		if len(workspace.dataChanges) >= connection.MaxTableChangeBatch {
			app.status = fmt.Sprintf(app.localized("每批最多暂存 %d 项修改。"), connection.MaxTableChangeBatch)
			return
		}
		workspace.dataChanges = append(workspace.dataChanges, dataRowChange{kind: connection.DeleteRow, sourceRow: sourceRow, original: append([]any(nil), row...)})
	} else {
		if change.kind == connection.UpdateRow {
			change.deletedRowValues = cloneDataValues(change.values)
		}
		change.kind, change.values = connection.DeleteRow, nil
	}
	app.status = app.localized("删除行已加入待提交修改。")
}

func (app *workbench) unstageDeleteDataRow(workspace *queryWorkspace, sourceRow int64) {
	if workspace == nil {
		return
	}
	change := findDataRowChange(workspace, sourceRow)
	if change == nil || change.kind != connection.DeleteRow {
		return
	}
	if len(change.deletedRowValues) > 0 {
		change.kind = connection.UpdateRow
		change.values = change.deletedRowValues
		change.deletedRowValues = nil
	} else {
		removeDataRowChange(workspace, sourceRow)
	}
	app.status = app.localized("已取消待删除行。")
}

func cloneDataValues(values map[string]any) map[string]any {
	if len(values) == 0 {
		return nil
	}
	cloned := make(map[string]any, len(values))
	for name, value := range values {
		cloned[name] = value
	}
	return cloned
}

func (app *workbench) stageActiveDataWorkspaceDraft() {
	workspace := app.activeQuery
	if workspace == nil || !workspace.dataView {
		return
	}
	if workspace.dataCellEditing {
		app.applyDataCellEdit(workspace)
	}
	if workspace.dataInsertEditing && app.dataInsertHasDraft(workspace) {
		app.stageDataInsert(workspace)
	}
}

func (app *workbench) hasStagedDataForConnections(connectionIDs map[string]bool) bool {
	for _, workspace := range app.workspaces {
		if workspace.dataView && connectionIDs[workspace.dataConnectionID] && (len(workspace.dataChanges) > 0 || workspace.dataCellEditing || app.dataInsertHasDraft(workspace)) {
			return true
		}
	}
	return false
}
