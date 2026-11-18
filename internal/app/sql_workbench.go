package app

import (
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/magalab/dbl/internal/connection"
	mysqladapter "github.com/magalab/dbl/internal/mysql"
	"github.com/magalab/dbl/internal/postgresql"
	"github.com/magalab/dbl/internal/preferences"
	"github.com/magalab/dbl/internal/safeerror"

	"github.com/magalab/dbl/internal/sqlite"
	"github.com/magalab/dbl/internal/sqlstream"
)

func (app *workbench) openSQLite(access sqlite.AccessMode) {
	if app.busy || app.exporting || app.dialogOpen {
		return
	}
	path := strings.TrimSpace(app.path)
	if path == "" {
		app.status = app.localized("请输入 SQLite 文件路径。")
		return
	}
	app.stageActiveDataWorkspaceDraft()
	if app.hasStagedDataForConnections(map[string]bool{"sqlite:" + path: true}) {
		app.status = app.localized("请先保存或丢弃此连接数据面板中的修改，再重新打开连接。 ")
		return
	}
	openedEnvironment := ""
	openedProfileID := ""
	for _, profile := range app.connectionProfiles {
		if profile.ID == app.selectedProfileID && profile.Driver == preferences.DriverSQLite && profile.Path == path {
			openedEnvironment = profile.Environment
			openedProfileID = profile.ID
			break
		}
	}
	if openedProfileID == "" {
		for _, profile := range app.connectionProfiles {
			if profile.Driver == preferences.DriverSQLite && profile.Path == path {
				openedEnvironment = profile.Environment
				openedProfileID = profile.ID
				break
			}
		}
	}
	app.busy = true
	if app.etcdCancel != nil {
		app.etcdCancel()
		app.etcdCancel = nil
		app.etcdWatching = false
	}
	app.status = app.localized("正在后台打开 SQLite 文件…")
	oldCursor, oldCancel := app.cursor, app.cancelQuery
	if oldCursor != nil {
		app.updateHistory(app.activeQuery.activeHistoryID, time.Time{}, "已切换数据库，停止后续读取")
	}
	app.cursor = nil
	app.cancelQuery = nil
	app.queryContext = nil
	if oldCancel != nil {
		oldCancel()
	}
	var preferenceStore *preferences.Store
	var preferenceRevision uint64
	if app.preferences != nil {
		app.preferenceRevision++
		preferenceRevision = app.preferenceRevision
		preferenceStore = app.preferences
	}
	go func() {
		if oldCursor != nil {
			_ = oldCursor.Close()
		}
		ctx, cancel := app.withTaskTimeout(10 * time.Second)
		defer cancel()
		var (
			store      connection.SQL
			adapter    connection.Connection
			objectPage connection.ObjectPage
		)
		adapter, err := (sqlite.Connector{Path: path, Access: access}).Connect(ctx)
		if err == nil {
			var ok bool
			store, ok = adapter.(connection.SQL)
			if !ok {
				err = errors.New("SQLite adapter does not provide SQL capabilities")
				_ = adapter.Close()
			}
		}
		var preferenceErr error
		if err == nil {
			objectPage, err = store.Objects(ctx, "", "", "", sqliteObjectPageSize)
			if err != nil {
				_ = adapter.Close()
			} else if preferenceStore != nil {
				preferenceErr = preferenceStore.RememberSQLitePath(path, preferenceRevision)
			}
		}
		app.window.Update(func() {
			app.busy = false
			if err != nil {
				app.reportError("打开 SQLite 失败", err)
				return
			}
			profile := preferences.ConnectionProfile{ID: openedProfileID, Driver: preferences.DriverSQLite, Path: path, Name: path, Environment: openedEnvironment, ReadOnly: access == sqlite.ReadOnly}
			app.registerLiveConnection("sqlite:"+path, store, profile)
			app.store = store
			app.sqlNamespaces = []string{"main"}
			app.sqlNamespace, app.sqlNamespaceChoice = "main", "main"
			app.openedSQLProfile, app.hasOpenedSQLProfile = preferences.ConnectionProfile{}, false
			app.keyStore, app.documentStore, app.etcdStore = nil, nil, nil
			app.objects = objectPage.Objects
			app.objectCursorSchema = objectPage.NextSchema
			app.objectCursorType = objectPage.NextType
			app.objectCursorName = objectPage.NextName
			app.objectsHasMore = objectPage.HasMore
			app.objectList = ui.ListState{}
			app.selectedObject = ""
			app.selectedType = ""
			app.selectedSQL = ""
			app.objectColumns = nil
			app.objectColumnsLoading, app.objectColumnsError = false, ""
			app.openedPath = path
			app.openedDriver = connection.DriverSQLite
			app.openedConnectionID = "sqlite:" + path
			app.selectedProfileID = openedProfileID
			app.openedLabel = path
			app.openedEnvironment = openedEnvironment
			app.openedReadOnly = access == sqlite.ReadOnly
			if app.canBindActiveQuery(app.openedConnectionID) {
				app.activeQuery.queryConnectionID = app.openedConnectionID
				app.activeQuery.queryNamespace = "main"
			}
			if app.openedReadOnly {
				app.status = app.localized("SQLite 已以只读方式连接。")
			} else {
				app.status = app.localized("SQLite 已连接。")
			}
			if preferenceStore != nil && preferenceErr == nil {
				app.recentSQLitePaths = preferenceStore.Settings().RecentSQLitePaths
			}
			if preferenceErr != nil {
				app.reportError("已连接，但无法保存最近文件设置", preferenceErr)
			}
			app.activateConnectionWorkspace(app.openedConnectionID)
		})
	}()
}

func (app *workbench) openPostgreSQL(profile preferences.ConnectionProfile) {
	schema := ""
	if isSQLQueryWorkspace(app.activeQuery) && app.activeQuery.queryConnectionID == profileConnectionID(profile) {
		schema = app.activeQuery.queryNamespace
	}
	app.openPostgreSQLSchema(profile, schema)
}

func (app *workbench) openPostgreSQLSchema(profile preferences.ConnectionProfile, schema string) {
	app.openRemoteSQLProfile(profile, connection.DriverPostgreSQL, "PostgreSQL", postgresql.Connector{Profile: profile, Vault: app.vault, SearchPath: schema})
}

func (app *workbench) openMySQL(profile preferences.ConnectionProfile) {
	if isSQLQueryWorkspace(app.activeQuery) && app.activeQuery.queryConnectionID == profileConnectionID(profile) && app.activeQuery.queryNamespace != "" {
		profile.Database = app.activeQuery.queryNamespace
	}
	app.openRemoteSQLProfile(profile, connection.DriverMySQL, "MySQL", mysqladapter.Connector{Profile: profile, Vault: app.vault})
}

func (app *workbench) switchSQLNamespace(namespace string) {
	app.sqlNamespaceChoice = app.sqlNamespace
	if app.busy || app.exporting || app.dialogOpen || app.store == nil || namespace == "" || namespace == app.sqlNamespace || !slices.Contains(app.sqlNamespaces, namespace) {
		return
	}
	if !app.hasOpenedSQLProfile {
		app.reportError("无法切换数据库 / Schema", errors.New("当前连接没有可重连的远程 SQL profile"))
		return
	}
	if isSQLQueryWorkspace(app.activeQuery) {
		app.activeQuery.queryNamespace = namespace
	}
	profile := app.openedSQLProfile
	profile, schema, err := namespaceSwitchTarget(app.openedDriver, profile, namespace)
	if err != nil {
		app.reportError("无法切换数据库 / Schema", err)
		return
	}
	switch app.openedDriver {
	case connection.DriverPostgreSQL:
		app.openPostgreSQLSchema(profile, schema)
	case connection.DriverMySQL:
		app.openMySQL(profile)
	}
}

func namespaceSwitchTarget(driver connection.Driver, profile preferences.ConnectionProfile, namespace string) (preferences.ConnectionProfile, string, error) {
	if namespace == "" {
		return preferences.ConnectionProfile{}, "", errors.New("database or schema is empty")
	}
	switch driver {
	case connection.DriverPostgreSQL:
		return profile, namespace, nil
	case connection.DriverMySQL:
		profile.Database = namespace
		return profile, "", nil
	default:
		return preferences.ConnectionProfile{}, "", errors.New("SQL driver does not support namespace switching")
	}
}

func namespaceSelectorDisabled(current string, namespaces []string) bool {
	return len(namespaces) == 0 || (len(namespaces) == 1 && current != "")
}

func (app *workbench) openRemoteSQLProfile(profile preferences.ConnectionProfile, driver connection.Driver, label string, connector connection.Connector) {
	if app.busy || app.exporting || app.dialogOpen || connection.Driver(profile.Driver) != driver {
		return
	}
	app.stageActiveDataWorkspaceDraft()
	connectionID := string(driver) + ":" + profile.ID
	if app.hasStagedDataForConnections(map[string]bool{connectionID: true}) {
		app.status = app.localized("请先保存或丢弃此连接数据面板中的修改，再重新打开连接。 ")
		return
	}
	app.busy = true
	if app.etcdCancel != nil {
		app.etcdCancel()
		app.etcdCancel = nil
		app.etcdWatching = false
	}
	app.status = app.localized("正在连接 ") + label + "…"
	oldCursor, oldCancel := app.cursor, app.cancelQuery
	if oldCursor != nil {
		app.updateHistory(app.activeQuery.activeHistoryID, time.Time{}, "已切换数据库，停止后续读取")
	}
	app.cursor = nil
	app.cancelQuery = nil
	app.queryContext = nil
	if oldCancel != nil {
		oldCancel()
	}
	go func() {
		if oldCursor != nil {
			_ = oldCursor.Close()
		}
		ctx, cancel := app.withTaskTimeout(20 * time.Second)
		defer cancel()
		adapter, err := connector.Connect(ctx)
		var store connection.SQL
		var objectPage connection.ObjectPage
		var namespaces []string
		namespace := ""
		if err == nil {
			var ok bool
			store, ok = adapter.(connection.SQL)
			if !ok {
				err = errors.New(label + " adapter does not provide SQL capabilities")
			}
		}
		if err == nil {
			objectPage, err = store.Objects(ctx, "", "", "", sqliteObjectPageSize)
		}
		if err == nil {
			namespaces, _ = store.Namespaces(ctx)
			namespace, _ = store.CurrentNamespace(ctx)
			if namespace == "" && driver == connection.DriverMySQL {
				namespace = profile.Database
			}
			if namespace != "" && !slices.Contains(namespaces, namespace) {
				namespaces = append(namespaces, namespace)
				sort.Strings(namespaces)
			}
		}
		if err != nil && adapter != nil {
			_ = adapter.Close()
		}
		app.window.Update(func() {
			app.busy = false
			if err != nil {
				app.sqlNamespaceChoice = app.sqlNamespace
				app.reportError("连接 "+label+" 失败", err)
				return
			}
			app.registerLiveConnection(string(driver)+":"+profile.ID, store, profile)
			app.store = store
			app.keyStore, app.documentStore, app.etcdStore = nil, nil, nil
			app.objects = objectPage.Objects
			app.sqlNamespaces = namespaces
			app.sqlNamespace, app.sqlNamespaceChoice = namespace, namespace
			app.openedSQLProfile, app.hasOpenedSQLProfile = profile, true
			app.objectCursorSchema = objectPage.NextSchema
			app.objectCursorType = objectPage.NextType
			app.objectCursorName = objectPage.NextName
			app.objectsHasMore = objectPage.HasMore
			app.objectList = ui.ListState{}
			app.selectedObject = ""
			app.selectedType = ""
			app.selectedSQL = ""
			app.objectColumns = nil
			app.objectColumnsLoading, app.objectColumnsError = false, ""
			app.openedPath = ""
			app.openedDriver = driver
			app.openedConnectionID = string(driver) + ":" + profile.ID
			app.openedLabel = profile.Name + " · " + profile.Host + "/" + profile.Database
			app.openedEnvironment = profile.Environment
			app.openedReadOnly = profile.ReadOnly
			if app.canBindActiveQuery(app.openedConnectionID) {
				app.activeQuery.queryConnectionID = app.openedConnectionID
				app.activeQuery.queryNamespace = namespace
			}
			if profile.ReadOnly {
				app.status = label + " 已以只读事务模式连接。"
			} else {
				app.status = label + " 已连接。"
			}
			app.activateConnectionWorkspace(app.openedConnectionID)
		})
	}()
}

func (app *workbench) closeConnection() {
	if app.busy || app.exporting || app.dialogOpen || (app.store == nil && app.keyStore == nil && app.documentStore == nil && app.etcdStore == nil) {
		return
	}
	connectionID := app.openedConnectionID
	if app.hasStagedDataForConnections(map[string]bool{connectionID: true}) {
		app.status = app.localized("请先保存或丢弃此连接数据面板中的修改，再关闭连接。 ")
		return
	}
	if app.hasStagedSchemaForConnections(map[string]bool{connectionID: true}) {
		app.status = app.localized("请先应用或丢弃此连接结构编辑器中的修改，再关闭连接。 ")
		return
	}
	stopCount := app.cancelResultCountForConnection(connectionID)
	app.collapseActiveConnectionTree(connectionID)
	live := app.liveConnections[connectionID]
	delete(app.liveConnections, connectionID)
	cursor, cancel := app.cursor, app.cancelQuery
	if cursor != nil {
		app.updateHistory(app.activeQuery.activeHistoryID, time.Time{}, "已关闭连接，停止后续读取")
	}
	if cancel != nil {
		cancel()
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
	app.store = nil
	app.sqlNamespaces = nil
	app.sqlNamespace, app.sqlNamespaceChoice = "", ""
	app.openedSQLProfile, app.hasOpenedSQLProfile = preferences.ConnectionProfile{}, false
	app.keyStore = nil
	app.documentStore = nil
	app.etcdStore = nil
	app.mongoCancel = nil
	app.etcdCancel = nil
	app.cancelRedis = nil
	app.etcdWatching = false
	app.mongoDatabases = nil
	app.mongoCollections = nil
	app.mongoIndexes = nil
	app.mongoIndexError = ""
	app.mongoIndexesLoading = false
	app.mongoDocuments = nil
	app.mongoEditor = nil
	app.mongoSelectedDB, app.mongoSelectedColl, app.mongoSelectedID = "", "", ""
	app.cursor = nil
	app.queryContext = nil
	app.cancelQuery = nil
	app.objects = nil
	app.objectCursorSchema = ""
	app.objectCursorType = ""
	app.objectCursorName = ""
	app.objectsHasMore = false
	app.objectList = ui.ListState{}
	app.selectedObject = ""
	app.selectedType = ""
	app.selectedSQL = ""
	app.objectColumns = nil
	app.objectColumnsLoading, app.objectColumnsError = false, ""
	app.openedPath = ""
	app.openedDriver = ""
	app.openedConnectionID = ""
	app.openedLabel = ""
	app.openedReadOnly = false
	app.openedEnvironment = ""
	app.redisKeys = nil
	app.redisSelectedKey = ""
	app.redisInfo = connection.KeyInfo{}
	app.redisPage = connection.KeyValuePage{}
	app.redisCursor = 0
	app.redisHasMore = false
	app.redisValueCursor = ""
	if app.activeQuery != nil {
		app.activeQuery.columns = nil
		app.activeQuery.resultSQL = ""
		app.activeQuery.resultPath = ""
		app.activeQuery.resultConnectionID = ""
		app.activeQuery.activeHistoryID = 0
		app.activeQuery.resultWindow = sqlstream.Window{MaxRows: maxDisplayedRows, MaxBytes: maxRetainedResultBytes}
		app.activeQuery.resultTotal = 0
		app.activeQuery.resultTotalKnown = false
		app.activeQuery.resultTotalCounted = false
		app.activeQuery.resultCountCancelled = false
		app.activeQuery.streamStopped = false
		app.resetResultOrder()
	}
	app.busy = true
	app.status = app.localized("正在关闭数据库连接…")
	go func() {
		if stopCount != nil {
			<-stopCount
		}
		if cursor != nil {
			_ = cursor.Close()
		}
		var err error
		if live.connection != nil {
			err = live.connection.Close()
		}
		app.window.Update(func() {
			app.busy = false
			if err != nil {
				app.reportError("关闭数据库连接失败", err)
			} else {
				app.status = app.localized("数据库连接已关闭。")
			}
		})
	}()
}

func (app *workbench) loadMoreObjects() {
	if app.busy || app.exporting || app.dialogOpen || app.store == nil || !app.objectsHasMore {
		return
	}
	store := app.store
	afterSchema, afterType, afterName := app.objectCursorSchema, app.objectCursorType, app.objectCursorName
	app.busy = true
	app.status = app.localized("正在加载更多数据库对象…")
	go func() {
		ctx, cancel := app.withTaskTimeout(5 * time.Second)
		defer cancel()
		page, err := store.Objects(ctx, afterSchema, afterType, afterName, sqliteObjectPageSize)
		app.window.Update(func() {
			app.busy = false
			if app.store != store {
				return
			}
			if err != nil {
				app.reportError("加载数据库对象失败", err)
				return
			}
			app.objects = append(app.objects, page.Objects...)
			app.objectCursorSchema = page.NextSchema
			app.objectCursorType = page.NextType
			app.objectCursorName = page.NextName
			app.objectsHasMore = page.HasMore
			app.status = fmt.Sprintf("已加载 %d 个数据库对象", len(app.objects))
		})
	}()
}

func (app *workbench) inspectObject(object connection.Object) {
	if app.busy || app.exporting || app.dialogOpen || app.store == nil {
		return
	}
	app.selectedObject = qualifiedObjectName(object)
	app.selectedType = object.Type
	app.selectedSQL = object.SQL
	app.objectColumns = nil
	app.objectColumnsError = ""
	app.objectColumnsLoading = false
	if !isSQLTableOrView(object.Type) {
		app.status = fmt.Sprintf("已选择 %s %s", object.Type, object.Name)
		return
	}
	app.objectColumnsLoading = true
	app.status = app.localized("正在读取表结构…")
	app.inspectTableColumns(object)
}

func qualifiedObjectName(object connection.Object) string {
	if object.Schema == "" {
		return object.Name
	}
	return object.Schema + "." + object.Name
}

func (app *workbench) inspectTableColumns(object connection.Object) {
	if app.busy || app.dialogOpen || app.store == nil {
		return
	}
	tableIdentity := qualifiedObjectName(object)
	store := app.store
	go func() {
		ctx, cancel := app.withTaskTimeout(5 * time.Second)
		defer cancel()
		columns, err := store.Columns(ctx, object.Schema, object.Name)
		app.window.Update(func() {
			if app.store != store || app.selectedObject != tableIdentity {
				return
			}
			app.objectColumnsLoading = false
			if err != nil {
				app.objectColumnsError = safeerror.Display(err)
				app.reportError("读取表结构失败", err)
				return
			}
			app.objectColumnsError = ""
			app.objectColumns = columns
			app.status = app.localized("表结构已加载。")
		})
	}()
}
