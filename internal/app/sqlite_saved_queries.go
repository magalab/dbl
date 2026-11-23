package app

import (
	"strings"
	"unicode/utf8"

	"github.com/magalab/dbl/internal/connection"
	"github.com/magalab/dbl/internal/preferences"
)

func (app *workbench) openSavedQueryEditor(connectionID, namespace string, query *preferences.SavedQuery) {
	if app.preferences == nil || app.busy || app.exporting || app.dialogOpen || app.savedQuerySaving {
		return
	}
	app.savedQueryDraft = preferences.SavedQuery{ConnectionID: connectionID, Namespace: namespace}
	app.savedQueryOriginalName = ""
	app.savedQueryTargetWorkspaceID = 0
	if query != nil {
		app.savedQueryDraft = *query
		app.savedQueryOriginalName = query.Name
	}
	app.savedQueryEditorOpen = true
	app.dialogOpen = true
}

func (app *workbench) closeSavedQueryEditor() {
	app.savedQueryEditorOpen = false
	app.dialogOpen = false
	app.savedQueryDraft = preferences.SavedQuery{}
	app.savedQueryOriginalName = ""
	app.savedQueryTargetWorkspaceID = 0
}

func (app *workbench) newSQLQueryWorkspace(connectionID, namespace string) {
	if app.busy || app.exporting || app.dialogOpen || app.queryWorkspaceRequested && app.workspaceCount() >= maxWorkspaces {
		if app.queryWorkspaceRequested && app.workspaceCount() >= maxWorkspaces {
			app.status = app.localized("工作区已达上限，请先关闭一个标签后再打开此连接的查询工作区。")
		}
		return
	}
	if connectionID != app.openedConnectionID {
		if !app.activateLiveConnectionForWorkspace(connectionID) || app.store == nil {
			return
		}
	}
	if app.store == nil || !isSupportedSavedQueryDriver(app.openedDriver) || !app.openSQLQuery("") || app.activeQuery == nil {
		return
	}
	app.activeQuery.queryConnectionID = connectionID
	app.activeQuery.queryNamespace = namespace
	if app.activeQuery.queryNamespace == "" {
		app.activeQuery.queryNamespace = app.sqlNamespace
		if app.activeQuery.queryNamespace == "" && app.openedDriver == connection.DriverSQLite {
			app.activeQuery.queryNamespace = "main"
		}
	}
	app.activeQuery.savedQueryName = ""
	app.status = app.localized("已打开新的查询工作区。")
	app.switchQueryWorkspaceNamespace(app.activeQuery)
}

func (app *workbench) saveActiveSQLQuery() {
	workspace := app.activeQuery
	if workspace == nil || !isSQLQueryWorkspace(workspace) || !isSavedQueryConnectionID(workspace.queryConnectionID) {
		return
	}
	query := preferences.SavedQuery{
		Name: workspace.savedQueryName, SQL: workspace.query.Buffer.String(),
		ConnectionID: workspace.queryConnectionID, Namespace: workspace.queryNamespace,
	}
	app.openSavedQueryEditor(query.ConnectionID, query.Namespace, &query)
	if app.savedQueryEditorOpen {
		app.savedQueryTargetWorkspaceID = workspace.id
	}
}

func (app *workbench) savedQueryValidationError() string {
	query := app.savedQueryDraft
	query.Name = strings.TrimSpace(query.Name)
	if query.Name == "" {
		return "查询名称不能为空。"
	}
	if utf8.RuneCountInString(query.Name) > 256 {
		return "查询名称不能超过 256 字符。"
	}
	if strings.TrimSpace(query.SQL) == "" {
		return "SQL 不能为空。"
	}
	if len(query.SQL) > preferences.MaxSavedQuerySQLBytes {
		return "SQL 查询不能超过 64 KiB。"
	}
	if len(query.ConnectionID) == 0 || len(query.ConnectionID) > 8192 || len(query.Namespace) > 1024 {
		return "查询名称不能为空。"
	}
	if app.preferences == nil {
		return "保存自定义查询失败"
	}
	for _, existing := range app.preferences.Settings().SavedQueries {
		if existing.ConnectionID != query.ConnectionID || !strings.EqualFold(strings.TrimSpace(existing.Name), query.Name) {
			continue
		}
		if existing.Name != app.savedQueryOriginalName {
			return "查询名称已存在。"
		}
	}
	if app.savedQueryOriginalName == "" && len(app.preferences.Settings().SavedQueries) >= preferences.MaxSavedQueries {
		return "自定义查询列表已达上限。"
	}
	return ""
}

func (app *workbench) saveSavedQuery() {
	if app.savedQuerySaving || app.preferences == nil || app.window == nil || app.savedQueryValidationError() != "" {
		return
	}
	query := app.savedQueryDraft
	query.Name = strings.TrimSpace(query.Name)
	app.savedQueryDraft.Name = query.Name
	settings := app.preferences.Settings()
	queries := make([]preferences.SavedQuery, 0, len(settings.SavedQueries)+1)
	replaced := false
	for _, existing := range settings.SavedQueries {
		if existing.ConnectionID == query.ConnectionID && existing.Name == app.savedQueryOriginalName && app.savedQueryOriginalName != "" {
			queries = append(queries, query)
			replaced = true
			continue
		}
		queries = append(queries, existing)
	}
	if !replaced {
		queries = append(queries, query)
	}
	app.persistSavedQueries(queries, "已保存自定义查询。", "保存自定义查询失败", true)
}

func (app *workbench) confirmDeleteSavedQuery(query preferences.SavedQuery) {
	if app.preferences == nil || app.dialogOpen || app.savedQuerySaving {
		return
	}
	app.requestConfirmation("删除自定义查询？", "确定删除自定义查询？", query.Name,
		[]string{"取消", "删除"}, 1, func(choice int) {
			if choice != 1 || app.preferences == nil {
				return
			}
			settings := app.preferences.Settings()
			queries := make([]preferences.SavedQuery, 0, len(settings.SavedQueries))
			for _, existing := range settings.SavedQueries {
				if existing.ConnectionID == query.ConnectionID && existing.Name == query.Name {
					continue
				}
				queries = append(queries, existing)
			}
			if len(queries) == len(settings.SavedQueries) {
				return
			}
			app.persistSavedQueries(queries, "已删除自定义查询。", "删除自定义查询失败", false)
		})
}

func (app *workbench) persistSavedQueries(queries []preferences.SavedQuery, successMessage, failureMessage string, closeEditor bool) {
	store := app.preferences
	if store == nil || app.window == nil || app.savedQuerySaving {
		return
	}
	app.savedQuerySaving = true
	app.preferenceRevision++
	revision := app.preferenceRevision
	savedName := app.savedQueryDraft.Name
	targetWorkspaceID := app.savedQueryTargetWorkspaceID
	go func() {
		err := store.SetSavedQueries(queries, revision)
		app.window.Update(func() {
			if app.preferences != store {
				return
			}
			app.savedQuerySaving = false
			if err != nil {
				app.reportError(failureMessage, err)
				return
			}
			app.status = app.localized(successMessage)
			if closeEditor {
				if targetWorkspaceID != 0 {
					if workspace := app.queryWorkspaceByID(targetWorkspaceID); workspace != nil {
						workspace.savedQueryName = savedName
						workspace.title = savedName
					}
				}
				app.closeSavedQueryEditor()
			}
		})
	}()
}

func (app *workbench) openSavedSQLQuery(query preferences.SavedQuery) {
	if app.busy || app.exporting || app.dialogOpen || app.queryWorkspaceRequested && app.workspaceCount() >= maxWorkspaces {
		return
	}
	if query.ConnectionID != app.openedConnectionID {
		if !app.activateLiveConnectionForWorkspace(query.ConnectionID) || app.store == nil {
			return
		}
	}
	if app.store == nil || !isSupportedSavedQueryDriver(app.openedDriver) || !app.openSQLQuery(query.SQL) || app.activeQuery == nil {
		return
	}
	app.activeQuery.queryConnectionID = query.ConnectionID
	app.activeQuery.queryNamespace = query.Namespace
	app.activeQuery.savedQueryName = query.Name
	app.activeQuery.title = query.Name
	if app.activeQuery.queryNamespace == "" && app.openedDriver == connection.DriverSQLite {
		app.activeQuery.queryNamespace = "main"
	}
	app.status = app.localized("已打开自定义查询。")
	if app.activeQuery.queryNamespace != "" && app.activeQuery.queryNamespace != app.sqlNamespace && app.hasOpenedSQLProfile {
		app.switchQueryWorkspaceNamespace(app.activeQuery)
	}
}

func (app *workbench) switchQueryWorkspaceNamespace(workspace *queryWorkspace) {
	if workspace == nil || workspace.queryNamespace == "" || workspace.queryNamespace == app.sqlNamespace || !app.hasOpenedSQLProfile {
		return
	}
	profile := app.openedSQLProfile
	switch app.openedDriver {
	case connection.DriverMySQL:
		profile.Database = workspace.queryNamespace
		app.openMySQL(profile)
	case connection.DriverPostgreSQL:
		app.openPostgreSQLSchema(profile, workspace.queryNamespace)
	}
}

func isSupportedSavedQueryDriver(driver connection.Driver) bool {
	return driver == connection.DriverSQLite || driver == connection.DriverPostgreSQL || driver == connection.DriverMySQL
}

func isSavedQueryConnectionID(connectionID string) bool {
	return strings.HasPrefix(connectionID, "sqlite:") || strings.HasPrefix(connectionID, "postgresql:") || strings.HasPrefix(connectionID, "mysql:")
}
