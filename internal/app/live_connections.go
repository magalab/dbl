package app

import (
	"context"
	"strings"
	"time"

	"github.com/magalab/dbl/internal/connection"
	"github.com/magalab/dbl/internal/preferences"
	"github.com/magalab/dbl/internal/sqlstream"
)

func (app *workbench) registerLiveConnection(id string, conn connection.Connection, profile preferences.ConnectionProfile) {
	delete(app.treeConnectionErrors, profile.ID)
	if app.liveConnections == nil {
		app.liveConnections = make(map[string]liveConnection)
	}
	if previous, exists := app.liveConnections[id]; exists && previous.connection != conn {
		stopCount := app.cancelResultCountForConnection(id)
		go func() {
			if stopCount != nil {
				<-stopCount
			}
			_ = previous.connection.Close()
		}()
	}
	app.liveConnections[id] = liveConnection{connection: conn, profile: profile}
}

func (app *workbench) activateLiveConnection(id string) bool {
	return app.activateLiveConnectionMode(id, false, false)
}

func (app *workbench) activateLiveConnectionForWorkspace(id string) bool {
	return app.activateLiveConnectionMode(id, true, false)
}

func (app *workbench) activateLiveConnectionMode(id string, preserveWorkspace, redisOverview bool) bool {
	live, ok := app.liveConnections[id]
	if !ok || live.connection == nil {
		return false
	}
	if app.busy || app.exporting || app.dialogOpen {
		return false
	}
	if !app.redisOverview && app.activeQuery != nil && app.activeQuery.redisView && app.activeQuery.redisConnectionID == app.openedConnectionID {
		app.saveRedisWorkspaceState(app.activeQuery)
	}
	app.redisOverview = redisOverview
	if app.openedConnectionID == id && app.hasActiveConnection() {
		return true
	}
	previousConnectionID := app.openedConnectionID
	app.cancelRedisDeleteConfirmation()
	app.cancelRedisTTLRefresh()
	app.stageActiveDataWorkspaceDraft()
	if app.cancelQuery != nil {
		app.cancelQuery()
	}
	if app.cursor != nil {
		if app.activeQuery != nil {
			app.updateHistory(app.activeQuery.activeHistoryID, time.Time{}, "切换工作区，后续结果读取已停止")
			app.activeQuery.streamStopped = true
		}
		_ = app.cursor.Close()
	}
	if app.mongoCancel != nil {
		app.mongoCancel()
	}
	if app.etcdCancel != nil {
		app.etcdCancel()
	}
	if app.cancelRedis != nil {
		app.cancelRedis()
	}
	app.cursor, app.cancelQuery, app.queryContext = nil, nil, nil
	app.mongoCancel, app.etcdCancel, app.cancelRedis = nil, nil, nil
	app.etcdWatching = false
	app.store, app.keyStore, app.documentStore, app.etcdStore = nil, nil, nil, nil
	app.etcdAdmin = nil
	app.store, _ = live.connection.(connection.SQL)
	app.keyStore, _ = live.connection.(connection.KeyValue)
	app.documentStore, _ = live.connection.(connection.Documents)
	app.etcdStore, _ = live.connection.(connection.Configuration)
	app.etcdAdmin, _ = live.connection.(connection.EtcdAdmin)
	app.etcdDashboardLoaded, app.etcdAuthLoaded = false, false
	app.etcdDashboard, app.etcdAuthOverview = connection.EtcdDashboard{}, connection.EtcdAuthOverview{}
	app.etcdPreviousMetrics = connection.EtcdMetrics{}
	app.etcdMetricsRateAvailable = false
	app.openedConnectionID = id
	app.openedDriver = live.connection.Driver()
	app.openedPath = ""
	if live.profile.Driver == preferences.DriverSQLite {
		app.openedPath = live.profile.Path
		app.path = live.profile.Path
	}
	app.selectedProfileID = live.profile.ID
	app.openedSQLProfile, app.hasOpenedSQLProfile = live.profile, app.store != nil && live.profile.ID != ""
	app.openedLabel = live.profile.Name
	if app.openedLabel == "" {
		app.openedLabel = live.profile.Path
	}
	app.openedEnvironment = live.profile.Environment
	app.openedReadOnly = live.profile.ReadOnly
	app.readOnlyMode = live.profile.ReadOnly
	app.objects, app.sqlNamespaces = nil, nil
	app.sqlNamespace, app.sqlNamespaceChoice = "", ""
	app.objectsHasMore = false
	app.selectedObject, app.selectedType, app.selectedSQL = "", "", ""
	app.objectColumns = nil
	app.objectColumnsLoading, app.objectColumnsError = false, ""
	app.redisKeys, app.redisSelectedKey = nil, ""
	app.redisKeySelection.Clear()
	app.redisKeyMetadata = nil
	app.redisKeyTTLDeadlines = nil
	app.redisInfo, app.redisPage = connection.KeyInfo{}, connection.KeyValuePage{}
	app.redisCursor, app.redisHasMore, app.redisValueCursor = 0, false, ""
	app.mongoDatabases, app.mongoCollections, app.mongoIndexes = nil, nil, nil
	app.mongoDocuments, app.mongoSelectedDB, app.mongoSelectedColl = nil, "", ""
	app.mongoSelectedID, app.mongoOriginalJSON = "", ""
	app.etcdEntries, app.etcdSelectedKey = nil, ""
	app.etcdSelected = connection.EtcdEntry{}
	app.etcdPrefix, app.etcdUsePrefix, app.etcdStartKey = "", true, ""
	app.etcdKeysLoaded = false
	app.etcdDashboardLoading, app.etcdAuthLoading = false, false
	if !preserveWorkspace && app.activeQuery != nil {
		app.cancelResultCountForWorkspace(app.activeQuery)
		app.activeQuery.columns = nil
		app.activeQuery.resultSQL, app.activeQuery.resultPath, app.activeQuery.resultConnectionID = "", "", ""
		app.activeQuery.activeHistoryID = 0
		app.activeQuery.resultWindow = sqlstream.Window{MaxRows: maxDisplayedRows, MaxBytes: maxRetainedResultBytes}
		app.activeQuery.resultTotal = 0
		app.activeQuery.resultTotalKnown = false
		app.activeQuery.resultTotalCounted = false
		app.activeQuery.resultCountCancelled = false
		app.activeQuery.streamStopped = false
		app.resetResultOrder()
	}
	app.status = app.localized("已切换到连接：") + live.profile.Name
	app.loadLiveConnectionBasics(id, live.connection, redisOverview)
	if app.etcdStore != nil {
		app.resetEtcdAdminState()
		if app.etcdPendingSection != "" {
			app.etcdSection = app.etcdPendingSection
			app.etcdPendingSection = ""
		} else if previousConnectionID != id || app.etcdSection == "" {
			app.etcdSection = "overview"
		}
		app.etcdToolbarSection = etcdToolbarSectionFor(app.etcdSection)
		app.etcdSidebarSection = app.etcdSection
		app.etcdGlobalSearch = app.etcdSection == "global-search"
		app.setEtcdSection(app.etcdSection)
	}
	return true
}

func (app *workbench) detachActiveConnectionContext() {
	app.redisOverview = false
	app.cancelRedisTTLRefresh()
	app.store, app.keyStore, app.documentStore, app.etcdStore = nil, nil, nil, nil
	app.etcdAdmin = nil
	app.openedSQLProfile, app.hasOpenedSQLProfile = preferences.ConnectionProfile{}, false
	app.openedDriver, app.openedConnectionID, app.openedPath = "", "", ""
	app.openedLabel, app.openedEnvironment = "", ""
	app.openedReadOnly = false
	app.objects, app.sqlNamespaces = nil, nil
	app.sqlNamespace, app.sqlNamespaceChoice = "", ""
	app.selectedObject, app.selectedType, app.selectedSQL = "", "", ""
	app.objectColumns = nil
	app.objectColumnsLoading, app.objectColumnsError = false, ""
	app.redisKeys, app.redisSelectedKey = nil, ""
	app.redisInfo, app.redisPage = connection.KeyInfo{}, connection.KeyValuePage{}
	app.redisCursor, app.redisHasMore, app.redisValueCursor = 0, false, ""
	app.redisKeySelection.Clear()
	app.redisKeyMetadata = nil
	app.redisKeyTTLDeadlines = nil
	app.redisEntryEditorOpen = false
	app.redisEntryFilter = ""
}

func (app *workbench) loadLiveConnectionBasics(id string, conn connection.Connection, redisOverview bool) {
	needVersion := app.liveConnections[id].sqlVersion == ""
	ctx, cancel := app.withTaskTimeout(10 * time.Second)
	redisMatch := redisMatchPattern(app.redisMatch)
	redisType := ""
	go func() {
		defer cancel()
		var objects []connection.Object
		var page connection.ObjectPage
		var namespaces []string
		var namespace string
		var database string
		var mongoDBs []string
		var redisPage connection.KeyPage
		var redisMetadata map[string]connection.KeyInfo
		var err error
		switch store := conn.(type) {
		case connection.SQL:
			page, err = store.Objects(ctx, "", "", "", sqliteObjectPageSize)
			objects = page.Objects
			if err == nil {
				namespaces, _ = store.Namespaces(ctx)
				namespace, _ = store.CurrentNamespace(ctx)
				if currentDatabase, ok := store.(connection.CurrentDatabase); ok {
					database, _ = currentDatabase.CurrentDatabase(ctx)
				}
			}
		case connection.Documents:
			mongoDBs, err = store.Databases(ctx)
		case connection.KeyValue:
			if !redisOverview {
				redisPage, err = store.ScanKeysByType(ctx, redisMatch, redisType, 0, 100)
				if err == nil {
					redisMetadata = inspectRedisKeyMetadata(ctx, store, redisPage.Keys)
				}
			}
		}
		app.window.Update(func() {
			live, ok := app.liveConnections[id]
			if !ok || live.connection != conn || app.openedConnectionID != id {
				return
			}
			if err != nil {
				app.reportError("读取连接资源失败", err)
				return
			}
			app.objects, app.objectCursorSchema, app.objectCursorType, app.objectCursorName = objects, page.NextSchema, page.NextType, page.NextName
			app.objectsHasMore = page.HasMore
			live.tree.database = database
			live.tree.objects = objects
			live.tree.namespaces = namespaces
			live.tree.namespace = namespace
			live.treeLoaded = true
			app.liveConnections[id] = live
			app.sqlNamespaces, app.sqlNamespace, app.sqlNamespaceChoice = namespaces, namespace, namespace
			if app.openedDriver == connection.DriverSQLite && len(namespaces) == 0 {
				app.sqlNamespaces = []string{"main"}
				app.sqlNamespace, app.sqlNamespaceChoice = "main", "main"
			}
			if app.activeQuery != nil && app.activeQuery.connectionView && app.activeQuery.connectionID == id &&
				(app.activeQuery.connectionDriver == connection.DriverPostgreSQL || app.activeQuery.connectionDriver == connection.DriverMySQL) &&
				app.activeQuery.connectionViewNamespace == "" {
				app.activeQuery.connectionViewNamespace = app.sqlNamespace
			}
			if isSQLQueryWorkspace(app.activeQuery) && app.activeQuery.queryConnectionID == id && app.activeQuery.queryNamespace == "" {
				app.activeQuery.queryNamespace = app.sqlNamespace
			}
			// Collection lists belong to the database the user selected. Loading the
			// first database here can race with selectMongoDatabase and put another
			// database's collections under the selected tree node.
			app.mongoDatabases = mongoDBs
			if !redisOverview && !app.redisOverview && (app.activeQuery == nil || !app.activeQuery.redisView || app.activeQuery.redisConnectionID != id || !app.activeQuery.redisStateLoaded) {
				app.redisKeys, app.redisCursor, app.redisHasMore = redisPage.Keys, redisPage.NextCursor, redisPage.NextCursor != 0
				app.replaceRedisKeyMetadata(redisMetadata)
				app.redisKeySelection.Clear()
				if app.activeQuery != nil && app.activeQuery.redisView && app.activeQuery.redisConnectionID == id {
					app.saveRedisWorkspaceState(app.activeQuery)
				}
			}
		})
		if source, ok := conn.(connection.SQLVersion); needVersion && ok && err == nil {
			versionCtx, stop := context.WithTimeout(ctx, 2*time.Second)
			version, versionErr := source.SQLVersion(versionCtx)
			stop()
			if versionErr == nil && strings.TrimSpace(version) != "" {
				app.window.Update(func() {
					live, exists := app.liveConnections[id]
					if exists && live.connection == conn {
						live.sqlVersion = strings.TrimSpace(version)
						app.liveConnections[id] = live
					}
				})
			}
		}
	}()
}

func (app *workbench) closeLiveConnection(id string) error {
	live, ok := app.liveConnections[id]
	if !ok {
		return nil
	}
	stopCount := app.cancelResultCountForConnection(id)
	delete(app.liveConnections, id)
	if stopCount != nil {
		<-stopCount
	}
	return live.connection.Close()
}

func (app *workbench) closeAllLiveConnections() {
	for id := range app.liveConnections {
		_ = app.closeLiveConnection(id)
	}
}

func (app *workbench) closeProfileConnection(profile preferences.ConnectionProfile) {
	if app.busy || app.exporting || app.dialogOpen {
		return
	}
	ids := []string{profileConnectionID(profile)}
	if profile.Driver == preferences.DriverRedis {
		ids = ids[:0]
		prefix := "redis:" + profile.ID + ":db:"
		for id := range app.liveConnections {
			if strings.HasPrefix(id, prefix) {
				ids = append(ids, id)
			}
		}
	}
	active := app.profileIsSelected(profile)
	closing := make(map[string]bool, len(ids)+1)
	for _, id := range ids {
		closing[id] = true
	}
	if active {
		closing[app.openedConnectionID] = true
	}
	if app.hasStagedDataForConnections(closing) {
		app.status = app.localized("请先保存或丢弃此连接数据面板中的修改，再关闭连接。 ")
		return
	}
	if app.hasStagedSchemaForConnections(closing) {
		app.status = app.localized("请先应用或丢弃此连接结构编辑器中的修改，再关闭连接。 ")
		return
	}
	app.collapseConnectionTree(profile.ID, profileConnectionID(profile))
	var inactive []connection.Connection
	var countStops []<-chan struct{}
	for _, id := range ids {
		if active && id == app.openedConnectionID {
			continue
		}
		if live, ok := app.liveConnections[id]; ok {
			if stopCount := app.cancelResultCountForConnection(id); stopCount != nil {
				countStops = append(countStops, stopCount)
			}
			delete(app.liveConnections, id)
			inactive = append(inactive, live.connection)
		}
	}
	if active {
		app.closeConnection()
	}
	if len(inactive) > 0 {
		go func() {
			var firstErr error
			for _, stopCount := range countStops {
				<-stopCount
			}
			for _, conn := range inactive {
				if err := conn.Close(); err != nil && firstErr == nil {
					firstErr = err
				}
			}
			app.window.Update(func() {
				if firstErr != nil {
					app.reportError("关闭数据库连接失败", firstErr)
				} else if !active {
					app.status = app.localized("数据库连接已关闭。")
				}
			})
		}()
	}
}
