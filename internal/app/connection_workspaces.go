package app

import (
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/magalab/dbl/internal/connection"
)

func (app *workbench) openRedisWorkspace(connectionID string) {
	if connectionID == "" {
		return
	}
	app.cancelRedisDeleteConfirmation()
	index := -1
	for i, workspace := range app.workspaces {
		if workspace.redisView && workspace.redisConnectionID == connectionID {
			index = i
			break
		}
	}
	if index < 0 {
		workspace := newQueryWorkspace(app.nextWorkspaceID)
		app.nextWorkspaceID++
		workspace.redisView = true
		workspace.connectionView = true
		workspace.connectionDriver = connection.DriverRedis
		workspace.connectionID = connectionID
		workspace.redisConnectionID = connectionID
		workspace.title = "Redis"
		if live, ok := app.liveConnections[connectionID]; ok {
			workspace.title = app.redisDatabaseLabelForProfile(live.profile, live.profile.RedisDB)
		}
		app.workspaces = append(app.workspaces, workspace)
		index = len(app.workspaces) - 1
	}
	app.workspaces[index].connectionView = true
	app.workspaces[index].connectionDriver = connection.DriverRedis
	app.workspaces[index].connectionID = connectionID
	app.queryWorkspaceRequested = true
	app.activeWorkspace = index
	app.activeQuery = app.workspaces[index]
	if live, ok := app.liveConnections[connectionID]; ok {
		app.activeQuery.title = app.redisDatabaseLabelForProfile(live.profile, live.profile.RedisDB)
	}
	if app.activeQuery.redisStateLoaded {
		app.restoreRedisWorkspaceState(app.activeQuery)
	}
}

func connectionViewAvailable(app *workbench, workspace *queryWorkspace) bool {
	if app == nil || workspace == nil || !workspace.connectionView || workspace.connectionID != app.openedConnectionID {
		return false
	}
	switch workspace.connectionDriver {
	case connection.DriverRedis:
		return app.keyStore != nil
	case connection.DriverMongoDB:
		return app.documentStore != nil
	case connection.DriverEtcd:
		return app.etcdStore != nil
	default:
		return false
	}
}

func (app *workbench) openConnectionView(connectionID string) {
	live, ok := app.liveConnections[connectionID]
	if !ok || live.connection == nil {
		return
	}
	if app.activeQuery != nil && app.activeQuery.redisView {
		app.saveRedisWorkspaceState(app.activeQuery)
	}
	index := -1
	for i, workspace := range app.workspaces {
		if workspace.connectionView && workspace.connectionID == connectionID {
			index = i
			break
		}
	}
	if index < 0 {
		workspace := newQueryWorkspace(app.nextWorkspaceID)
		app.nextWorkspaceID++
		workspace.connectionView = true
		workspace.connectionDriver = live.connection.Driver()
		workspace.connectionID = connectionID
		workspace.title = live.profile.Name
		if workspace.title == "" {
			workspace.title = string(live.connection.Driver())
		}
		app.workspaces = append(app.workspaces, workspace)
		index = len(app.workspaces) - 1
	}
	app.queryWorkspaceRequested = true
	app.activeWorkspace = index
	app.activeQuery = app.workspaces[index]
}

func (app *workbench) saveRedisWorkspaceState(workspace *queryWorkspace) {
	if workspace == nil || !workspace.redisView {
		return
	}
	workspace.redisStateLoaded = true
	workspace.redisMatch = app.redisMatch
	workspace.redisGroupByPrefix = app.redisGroupByPrefix
	workspace.redisGroupDelimiter = app.redisGroupDelimiter
	workspace.redisGroupExpanded = cloneRedisGroupExpanded(app.redisGroupExpanded)
	workspace.redisKeyTTLDeadlines = cloneRedisKeyTTLDeadlines(app.redisKeyTTLDeadlines)
	workspace.redisKeys = append([]string(nil), app.redisKeys...)
	workspace.redisKeySelection = cloneRedisKeySelection(app.redisKeySelection)
	workspace.redisKeyMetadata = cloneRedisKeyMetadata(app.redisKeyMetadata)
	workspace.redisCursor = app.redisCursor
	workspace.redisHasMore = app.redisHasMore
	workspace.redisSelectedKey = app.redisSelectedKey
	workspace.redisInfo = app.redisInfo
	workspace.redisPage = app.redisPage
	workspace.redisPage.Entries = append([]connection.KeyValueEntry(nil), app.redisPage.Entries...)
	workspace.redisValueCursor = app.redisValueCursor
	workspace.redisValueDraft = app.redisValueDraft
	workspace.redisEntryField = app.redisEntryField
	workspace.redisEntryScore = app.redisEntryScore
	workspace.redisEntryFilter = app.redisEntryFilter
}

func (app *workbench) restoreRedisWorkspaceState(workspace *queryWorkspace) {
	if workspace == nil || !workspace.redisView || !workspace.redisStateLoaded {
		return
	}
	app.redisMatch = workspace.redisMatch
	app.redisGroupByPrefix = workspace.redisGroupByPrefix
	app.redisGroupDelimiter = workspace.redisGroupDelimiter
	app.redisGroupExpanded = cloneRedisGroupExpanded(workspace.redisGroupExpanded)
	app.redisKeyTTLDeadlines = cloneRedisKeyTTLDeadlines(workspace.redisKeyTTLDeadlines)
	app.redisKeys = append([]string(nil), workspace.redisKeys...)
	app.redisKeySelection = cloneRedisKeySelection(workspace.redisKeySelection)
	app.redisKeyMetadata = cloneRedisKeyMetadata(workspace.redisKeyMetadata)
	if len(app.redisKeyTTLDeadlines) == 0 && len(app.redisKeyMetadata) > 0 {
		app.redisKeyTTLDeadlines = redisKeyTTLDeadlines(app.redisKeyMetadata, time.Now())
	}
	app.redisCursor = workspace.redisCursor
	app.redisHasMore = workspace.redisHasMore
	app.redisSelectedKey = workspace.redisSelectedKey
	app.redisInfo = workspace.redisInfo
	app.redisPage = workspace.redisPage
	app.redisPage.Entries = append([]connection.KeyValueEntry(nil), workspace.redisPage.Entries...)
	app.redisValueCursor = workspace.redisValueCursor
	app.redisValueDraft = workspace.redisValueDraft
	app.redisEntryField = workspace.redisEntryField
	app.redisEntryScore = workspace.redisEntryScore
	app.redisEntryFilter = workspace.redisEntryFilter
}

func cloneRedisKeySelection(source ui.Selection[string]) ui.Selection[string] {
	var result ui.Selection[string]
	for key := range source.All() {
		result.Add(key)
	}
	return result
}

func cloneRedisKeyMetadata(source map[string]connection.KeyInfo) map[string]connection.KeyInfo {
	if len(source) == 0 {
		return nil
	}
	result := make(map[string]connection.KeyInfo, len(source))
	for key, info := range source {
		result[key] = info
	}
	return result
}

func cloneRedisGroupExpanded(source map[string]bool) map[string]bool {
	if len(source) == 0 {
		return nil
	}
	result := make(map[string]bool, len(source))
	for path, expanded := range source {
		result[path] = expanded
	}
	return result
}

func cloneRedisKeyTTLDeadlines(source map[string]time.Time) map[string]time.Time {
	if len(source) == 0 {
		return nil
	}
	result := make(map[string]time.Time, len(source))
	for key, deadline := range source {
		result[key] = deadline
	}
	return result
}

func workspaceConnectionID(workspace *queryWorkspace) string {
	if workspace == nil {
		return ""
	}
	switch {
	case workspace.connectionView:
		return workspace.connectionID
	case workspace.dataView:
		return workspace.dataConnectionID
	case workspace.schemaView:
		return workspace.schemaConnectionID
	case workspace.queryConnectionID != "":
		return workspace.queryConnectionID
	default:
		return workspace.resultConnectionID
	}
}
