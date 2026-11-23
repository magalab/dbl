package app

import (
	"time"

	"github.com/magalab/dbl/internal/connection"
)

func (app *workbench) activateConnectionWorkspace(connectionID string) {
	if app.busy || app.exporting || app.dialogOpen {
		return
	}
	live, ok := app.liveConnections[connectionID]
	if !ok || live.connection == nil {
		return
	}
	app.sqliteOverview = false
	if live.connection.Driver() == connection.DriverRedis {
		app.openRedisWorkspace(connectionID)
		return
	}
	if live.connection.Driver() == connection.DriverMongoDB || live.connection.Driver() == connection.DriverEtcd {
		app.openConnectionView(connectionID)
		return
	}
	if live.connection.Driver() != connection.DriverPostgreSQL && live.connection.Driver() != connection.DriverMySQL && live.connection.Driver() != connection.DriverSQLite {
		return
	}
	if !app.queryWorkspaceRequested && app.activeQuery != nil && !app.activeQuery.connectionView {
		app.activeQuery.queryConnectionID = connectionID
		app.activeQuery.queryNamespace = app.sqlNamespace
		return
	}
	if isSQLQueryWorkspace(app.activeQuery) && app.activeQuery.queryConnectionID == connectionID {
		if app.sqlNamespace != "" {
			app.activeQuery.queryNamespace = app.sqlNamespace
		}
		return
	}
	index := -1
	for i, workspace := range app.workspaces {
		if !workspace.implicitQuery && !workspace.connectionView && workspaceConnectionID(workspace) == connectionID {
			index = i
			break
		}
	}
	if index < 0 {
		for i, workspace := range app.workspaces {
			if !workspace.implicitQuery && !workspace.connectionView && !workspace.dataView && !workspace.schemaView && workspace.queryConnectionID == "" {
				workspace.queryConnectionID = connectionID
				workspace.queryNamespace = app.sqlNamespace
				index = i
				break
			}
		}
	}
	if index < 0 {
		if app.workspaceCount() >= maxWorkspaces {
			app.detachActiveConnectionContext()
			app.status = app.localized("工作区已达上限，请先关闭一个标签后再打开此连接的查询工作区。")
			return
		}
		workspace := newQueryWorkspace(app.nextWorkspaceID)
		app.nextWorkspaceID++
		workspace.queryConnectionID = connectionID
		workspace.queryNamespace = app.sqlNamespace
		app.workspaces = append(app.workspaces, workspace)
		index = len(app.workspaces) - 1
	}
	app.queryWorkspaceRequested = true
	app.activateWorkspace(index)
}

func (app *workbench) addWorkspace() {
	index, implicit := app.implicitQueryWorkspace()
	if app.busy || app.exporting || app.dialogOpen || app.workspaceCount() >= maxWorkspaces || (app.hasActiveConnection() && app.store == nil) {
		return
	}
	app.sqliteOverview = false
	if implicit != nil {
		implicit.implicitQuery = false
		implicit.query.Buffer.Set("")
		implicit.queryConnectionID = app.openedConnectionID
		implicit.queryNamespace = app.sqlNamespace
		implicit.columns = nil
		implicit.resultSQL = ""
		implicit.activeHistoryID = 0
		implicit.showHistory = false
		app.queryWorkspaceRequested = true
		if index == app.activeWorkspace {
			app.activeQuery = implicit
			app.status = app.localized("已打开查询工作区。")
		} else {
			app.activateWorkspace(index)
		}
		return
	}
	workspace := newQueryWorkspace(app.nextWorkspaceID)
	app.nextWorkspaceID++
	workspace.queryConnectionID = app.openedConnectionID
	workspace.queryNamespace = app.sqlNamespace
	app.workspaces = append(app.workspaces, workspace)
	app.queryWorkspaceRequested = true
	app.activateWorkspace(len(app.workspaces) - 1)
}

func (app *workbench) activateWorkspace(index int) {
	if index < 0 || index >= len(app.workspaces) || app.busy || app.exporting || app.dialogOpen {
		return
	}
	app.sqliteOverview = false
	workspace := app.workspaces[index]
	if app.activeQuery != nil && app.activeQuery.mysqlUserManager && app.activeQuery != workspace {
		clearMySQLUserWorkspaceSecrets(app.activeQuery)
	}
	if app.activeQuery != nil && app.activeQuery.postgresRoleManager && app.activeQuery != workspace {
		clearPostgresRoleWorkspaceSecrets(app.activeQuery)
	}
	targetConnectionID := workspaceConnectionID(workspace)
	connectionAlreadyActive := targetConnectionID == app.openedConnectionID
	reusedRedisOverview := app.redisOverview && connectionAlreadyActive && workspace.redisView
	if isSQLQueryWorkspace(app.activeQuery) && app.activeQuery == workspace && app.store != nil && app.sqlNamespace != "" {
		app.activeQuery.queryNamespace = app.sqlNamespace
	}
	targetNamespace := ""
	if isSQLQueryWorkspace(workspace) {
		targetNamespace = workspace.queryNamespace
	}
	if !app.redisOverview && index == app.activeWorkspace && targetConnectionID == app.openedConnectionID && (targetNamespace == "" || targetNamespace == app.sqlNamespace) {
		return
	}
	if app.cursor != nil && index != app.activeWorkspace {
		app.stopCursorForWorkspaceChange(index, -1)
		return
	}
	if !app.redisOverview && app.activeQuery != nil && app.activeQuery.redisView && app.activeWorkspace != index {
		app.saveRedisWorkspaceState(app.activeQuery)
	}
	if targetConnectionID != "" && targetConnectionID != app.openedConnectionID {
		if targetNamespace != "" {
			if live, ok := app.liveConnections[targetConnectionID]; ok && live.connection != nil {
				switch live.connection.Driver() {
				case connection.DriverMySQL:
					if live.profile.Database != targetNamespace {
						profile := live.profile
						profile.Database = targetNamespace
						app.activeWorkspace = index
						app.activeQuery = workspace
						app.openMySQL(profile)
						return
					}
				case connection.DriverPostgreSQL:
					if !live.treeLoaded || live.tree.namespace != targetNamespace {
						app.activeWorkspace = index
						app.activeQuery = workspace
						app.openPostgreSQLSchema(live.profile, targetNamespace)
						return
					}
				}
			}
		}
		if !app.activateLiveConnectionForWorkspace(targetConnectionID) {
			if workspace.connectionView || isSQLQueryWorkspace(workspace) {
				app.detachActiveConnectionContext()
				app.activeWorkspace = index
				app.activeQuery = workspace
				app.status = app.localized("连接已关闭")
				return
			}
			app.status = app.localized("此工作区所属的连接已关闭。 ")
			return
		}
	} else if targetConnectionID == "" && app.hasActiveConnection() && !workspace.connectionView {
		app.detachActiveConnectionContext()
	}
	app.redisOverview = false
	app.activeWorkspace = index
	app.activeQuery = workspace
	if connectionAlreadyActive && isSQLQueryWorkspace(workspace) && targetNamespace != "" && app.store != nil && app.sqlNamespace != targetNamespace {
		app.switchSQLNamespace(targetNamespace)
		return
	}
	if workspace.redisView && workspace.redisStateLoaded {
		app.restoreRedisWorkspaceState(workspace)
	} else if reusedRedisOverview {
		app.scanRedis(true)
		return
	}
	app.status = app.localized("已切换到 ") + workspace.title + "。"
	if workspace.redisView {
		if live, ok := app.liveConnections[workspace.redisConnectionID]; ok {
			app.status = app.localized("已切换到 ") + app.redisDatabaseLabelForProfile(live.profile, live.profile.RedisDB) + "。"
		}
	} else if workspace.streamStopped {
		app.status += " 当前结果只包含已读取部分。"
	}
	app.errorSummary = ""
	app.errorDetails = ""
	if workspace.dataView && workspace.pendingDataRefresh {
		app.refreshDataWorkspace()
	}
}

func isSQLQueryWorkspace(workspace *queryWorkspace) bool {
	return workspace != nil && !workspace.connectionView && !workspace.dataView && !workspace.schemaView && !workspace.redisView
}

func (app *workbench) userManagerWorkspaceIndex(connectionID string, driver connection.Driver) int {
	for index, workspace := range app.workspaces {
		if workspace.queryConnectionID != connectionID {
			continue
		}
		if (driver == connection.DriverMySQL && workspace.mysqlUserManager) || (driver == connection.DriverPostgreSQL && workspace.postgresRoleManager) {
			return index
		}
	}
	return -1
}

func (app *workbench) canBindActiveQuery(connectionID string) bool {
	return isSQLQueryWorkspace(app.activeQuery) && (app.activeQuery.queryConnectionID == "" || app.activeQuery.queryConnectionID == connectionID)
}

func (app *workbench) closeWorkspace(index int) {
	if index < 0 || index >= len(app.workspaces) || app.workspaces[index].implicitQuery || app.busy || app.exporting || app.dialogOpen {
		return
	}
	workspace := app.workspaces[index]
	if workspace.mysqlUserManager {
		clearMySQLUserWorkspaceSecrets(workspace)
	}
	if workspace.postgresRoleManager {
		clearPostgresRoleWorkspaceSecrets(workspace)
	}
	if workspace.dataView {
		if workspace.dataCellEditing {
			app.applyDataCellEdit(workspace)
		}
		if workspace.dataInsertEditing {
			if app.dataInsertHasDraft(workspace) {
				app.stageDataInsert(workspace)
			} else {
				workspace.dataInsertEditing = false
			}
		}
	}
	if workspace.schemaView && schemaWorkspaceDirty(workspace) {
		app.confirmCloseSchemaWorkspace(index, workspace)
		return
	}
	if workspace.dataView && (len(workspace.dataChanges) > 0 || workspace.dataCellEditing || app.dataInsertHasDraft(workspace)) {
		app.confirmCloseDataWorkspace(index, workspace)
		return
	}
	if index == app.activeWorkspace && app.cursor != nil {
		app.stopCursorForWorkspaceChange(-1, index)
		return
	}
	app.removeWorkspace(index)
}

// Stop an inactive result cursor so tabs cannot hold every connection in the
// SQLite pool open while their remaining rows are not being consumed.

func (app *workbench) stopCursorForWorkspaceChange(switchTo, closeIndex int) {
	cursor, cancel := app.cursor, app.cancelQuery
	app.updateHistory(app.activeQuery.activeHistoryID, time.Time{}, "工作区切换，后续结果读取已停止")
	app.activeQuery.streamStopped = true
	app.cursor = nil
	app.cancelQuery = nil
	app.queryContext = nil
	if cancel != nil {
		cancel()
	}
	app.busy = true
	app.status = app.localized("正在停止当前工作区的后续结果读取…")
	go func() {
		closeErr := cursor.Close()
		app.window.Update(func() {
			app.busy = false
			if closeErr != nil {
				app.reportError("关闭查询游标失败", closeErr)
				return
			}
			if closeIndex >= 0 {
				app.removeWorkspace(closeIndex)
				return
			}
			app.activateWorkspace(switchTo)
			app.status = app.localized("已切换工作区；前一查询已读取部分仍保留，未读取部分已停止。")
		})
	}()
}

func (app *workbench) removeWorkspace(index int) {
	if index < 0 || index >= len(app.workspaces) {
		return
	}
	if index >= 0 && index < len(app.workspaces) {
		app.cancelResultCountForWorkspace(app.workspaces[index])
	}
	wasActive := index == app.activeWorkspace
	app.workspaces = append(app.workspaces[:index], app.workspaces[index+1:]...)
	if app.workspaceCount() == 0 {
		if implicitIndex, implicit := app.implicitQueryWorkspace(); implicit != nil {
			app.activeWorkspace = implicitIndex
			app.activeQuery = implicit
		} else {
			app.activeWorkspace = -1
			app.activeQuery = nil
		}
		app.queryWorkspaceRequested = false
		app.status = app.localized("已关闭工作区。")
		app.errorSummary = ""
		app.errorDetails = ""
		return
	}
	if !wasActive && index < app.activeWorkspace {
		app.activeWorkspace--
	}
	if wasActive {
		next := min(index, len(app.workspaces)-1)
		app.activeWorkspace = -1
		app.activeQuery = nil
		app.activateWorkspace(next)
	} else {
		app.activeQuery = app.workspaces[app.activeWorkspace]
	}
	app.status = app.localized("已关闭工作区。")
	app.errorSummary = ""
	app.errorDetails = ""
}
