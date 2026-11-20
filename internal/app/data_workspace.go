package app

import (
	"fmt"

	"github.com/egoist/mygo/ui"
	"github.com/magalab/dbl/internal/connection"
)

const dataWorkspacePageSize = 100

func resultPageSizeOptions() []string {
	return []string{"25", "50", "100", "200", "500"}
}

func resultWorkspacePageSize(workspace *queryWorkspace) int {
	if workspace == nil || workspace.resultPageSize <= 0 {
		return dataWorkspacePageSize
	}
	return workspace.resultPageSize
}

func resultPageCount(rowCount, pageSize int) int {
	if rowCount <= 0 || pageSize <= 0 {
		return 1
	}
	return (rowCount + pageSize - 1) / pageSize
}

func dataWorkspacePageStart(workspace *queryWorkspace) int {
	if workspace == nil || workspace.dataPage < 0 {
		return 0
	}
	return workspace.dataPage * resultWorkspacePageSize(workspace)
}

func dataWorkspacePageCount(rowCount int) int {
	return resultPageCount(rowCount, dataWorkspacePageSize)
}

func (app *workbench) openDataWorkspace(object connection.Object) {
	app.openDataWorkspaceMode(object, false)
}

func (app *workbench) openDataWorkspaceInNewTab(object connection.Object) {
	app.openDataWorkspaceMode(object, true)
}

func (app *workbench) openDataWorkspaceMode(object connection.Object, forceNew bool) {
	if app.busy || app.exporting || app.dialogOpen || app.store == nil || !isSQLTableOrView(object.Type) {
		return
	}
	if !forceNew {
		for i, workspace := range app.workspaces {
			if workspace.dataView && workspace.dataObject == object && workspace.dataConnectionID == app.openedConnectionID {
				if i != app.activeWorkspace || app.sqliteOverview {
					app.activateWorkspace(i)
				}
				return
			}
		}
	}
	if app.workspaceCount() >= maxWorkspaces {
		return
	}
	workspace := newQueryWorkspace(app.nextWorkspaceID)
	app.nextWorkspaceID++
	workspace.title = object.Name
	workspace.dataView = true
	workspace.dataObject = object
	workspace.dataConnectionID = app.openedConnectionID
	workspace.pendingDataRefresh = true
	workspace.dataColumnsFor = ""
	workspace.query.Buffer.Set(objectDataQuery(app, object))
	app.workspaces = append(app.workspaces, workspace)
	app.queryWorkspaceRequested = true
	app.activateWorkspace(len(app.workspaces) - 1)
	if app.activeQuery == workspace {
		app.refreshDataWorkspace()
	}
}

func (app *workbench) refreshDataWorkspace() {
	if app.busy || app.exporting || app.dialogOpen || app.activeQuery == nil || !app.activeQuery.dataView || app.store == nil {
		return
	}
	if len(app.activeQuery.dataChanges) > 0 {
		app.activeQuery.pendingDataRefresh = true
		app.status = app.localized("先提交或丢弃待处理的数据修改，再刷新。")
		return
	}
	if app.activeQuery.dataConnectionID != "" && app.activeQuery.dataConnectionID != app.openedConnectionID {
		app.activeQuery.pendingDataRefresh = true
		app.status = app.localized("请切换回此数据工作区所属的连接后再刷新。 ")
		return
	}
	// Refresh metadata along with rows so defaults and generated-column flags stay current.
	app.activeQuery.dataSelectedRow = -1
	app.activeQuery.dataPage = 0
	app.activeQuery.pageJumpInput = "1"
	if !app.activeQuery.dataColumnsLoading {
		app.activeQuery.dataColumnsFor = ""
	}
	app.loadDataWorkspaceColumns(app.activeQuery)
	app.activeQuery.query.Buffer.Set(objectDataQuery(app, app.activeQuery.dataObject))
	app.activeQuery.pendingDataRefresh = false
	app.executeSQL()
}

func (app *workbench) openDataSQLQuery() {
	if app.activeQuery == nil || !app.activeQuery.dataView || app.activeQuery.dataConnectionID != app.openedConnectionID {
		return
	}
	statement := app.activeQuery.query.Buffer.String()
	if app.openSQLQuery(statement) {
		app.executeSQL()
	}
}

func (app *workbench) dataWorkspace(c *ui.Context) {
	object := app.activeQuery.dataObject
	c.OnShortcut(ui.Cmd, ui.KeyS, func() {
		app.saveDataWorkspaceChanges(app.activeQuery)
	})
	if app.activeQuery.dataCellEditing && !app.dialogOpen {
		workspace := app.activeQuery
		c.OnShortcut(0, ui.KeyEscape, func() {
			app.cancelDataCellEdit(workspace)
		})
	}
	if app.activeQuery.dataConnectionID == app.openedConnectionID && app.activeQuery.dataColumnsFor != app.openedConnectionID && app.store != nil {
		app.loadDataWorkspaceColumns(app.activeQuery)
	}
	ui.Row(c).FillWidth().Padding(10, 14).Gap(10).AlignItems(ui.Center).Children(func() {
		if icon := treeItemIcons["table"]; icon != nil {
			ui.Icon(c, icon).Size(18, 18)
		}
		ui.Text(c, app.localized("数据")).Bold().Role(ui.RoleHeading).Level(1)
		breadcrumb := app.dataWorkspaceConnectionLabel(app.activeQuery)
		if object.Schema != "" {
			breadcrumb += " / " + object.Schema
		}
		breadcrumb += " / " + object.Name
		ui.Text(c, breadcrumb).TextColor(c.Theme().TextMuted).SingleLine()
		if len(app.activeQuery.dataColumns) > 0 {
			ui.Text(c, fmt.Sprintf("%d %s", len(app.activeQuery.dataColumns), app.localized("字段"))).TextColor(c.Theme().TextMuted).SingleLine()
		}
		ui.Spacer(c)
		refresh := iconButton(c, app.localized("刷新数据"), actionIcons["refresh"])
		refresh.Size(32, 32).Padding(5)
		refresh.Disabled(app.busy || app.exporting || app.dialogOpen || app.store == nil || app.activeQuery.dataConnectionID != app.openedConnectionID || len(app.activeQuery.dataChanges) > 0)
		if refresh.Clicked() {
			app.refreshDataWorkspace()
		}
		query := iconButton(c, app.localized("在查询中打开"), actionIcons["query"])
		query.Size(32, 32).Padding(5)
		query.Disabled(app.busy || app.exporting || app.dialogOpen || app.store == nil || app.activeQuery.dataConnectionID != app.openedConnectionID)
		if query.Clicked() {
			app.openDataSQLQuery()
		}
	})
	if object.Type == "table" {
		ui.Column(c).FillWidth().Padding(4, 12).Gap(4).Children(func() {
			ui.Row(c).FillWidth().Gap(8).Children(func() {
				app.dataEditToolbar(c, app.activeQuery)
			})
			app.dataInsertToolbar(c, app.activeQuery)
		})
	}
	if app.activeQuery.dataConnectionID != app.openedConnectionID {
		ui.Text(c, app.localized("当前数据结果属于另一连接；切换回来源连接后才能刷新或打开查询。 ")).TextColor(c.Theme().Warning).Padding(8, 12)
	}
	if app.activeQuery.dataColumnsLoading {
		ui.Text(c, app.localized("正在读取表结构…")).TextColor(c.Theme().TextMuted).Padding(8, 12)
	} else if app.activeQuery.dataColumnsError != "" {
		ui.Text(c, app.localized("读取表结构失败：")+app.activeQuery.dataColumnsError).TextColor(c.Theme().Danger).Padding(8, 12)
	} else if object.Type == "table" && !app.busy && !app.activeQuery.dataColumnsLoading && app.activeQuery.dataColumnsFor == app.activeQuery.dataConnectionID && len(dataWorkspaceResultColumns(app.activeQuery)) > 0 && !dataHasVisiblePrimaryKey(app.activeQuery) {
		ui.Text(c, app.localized("此表没有可用的主键列；可以新增行，但不能安全地原地修改或删除现有行。 ")).TextColor(c.Theme().TextMuted).Padding(8, 12)
	}
	app.dataChangesPanel(c, app.activeQuery)
	app.resultTable(c)
}

func (app *workbench) dataWorkspaceConnectionLabel(workspace *queryWorkspace) string {
	if workspace == nil {
		return app.openedLabel
	}
	if workspace.dataConnectionID == app.openedConnectionID {
		return app.openedLabel
	}
	if live, ok := app.liveConnections[workspace.dataConnectionID]; ok {
		if live.profile.Name != "" {
			return live.profile.Name
		}
		return live.profile.Path
	}
	return app.localized("连接已关闭")
}
