package app

import (
	"fmt"

	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/magalab/dbl/internal/connection"

	"github.com/magalab/dbl/internal/preferences"

	"github.com/magalab/dbl/internal/sqlite"
)

func (app *workbench) connectionTreeResources(c *ui.Context, rootDepth int) {
	switch {
	case app.store != nil:
		if app.openedDriver == connection.DriverPostgreSQL && app.hasOpenedSQLProfile {
			database := app.openedSQLProfile.Database
			if database == "" {
				if live, ok := app.liveConnections[app.openedConnectionID]; ok {
					database = live.tree.database
				}
			}
			if database != "" {
				open := app.connectionTreeState("pg-db:"+app.openedConnectionID, false)
				item := ui.TreeItem(c, treeIconPlaceholder+database, open, func() { app.sqlConnectionTree(c, rootDepth+2) }).Selected(app.selectedObject == "" && len(app.sqlNamespaces) == 0).ContextMenu(func(menu *ui.Menu) {
					app.databaseContextMenu(menu, database)
				})
				item = connectionTreeDecorations(c, item, "", "database", rootDepth+1, false, false)
				item.Clicked()
			} else {
				app.sqlConnectionTree(c, rootDepth+1)
			}
		} else {
			app.sqlConnectionTree(c, rootDepth+1)
		}
	case app.documentStore != nil:
		app.mongoConnectionTree(c, rootDepth+1)
	case app.keyStore != nil:
		currentDB := app.currentRedisDB()
		for database := 0; database < 16; database++ {
			database := database
			item := ui.TreeItem(c, treeIconPlaceholder+app.redisDatabaseLabel(database), nil, nil).Selected(!app.redisOverview && database == currentDB).ContextMenu(func(menu *ui.Menu) {
				app.redisDatabaseContextMenu(menu, database)
			})
			item = connectionTreeDecorations(c, item, "", "database", rootDepth+1, false, false)
			if item.Clicked() {
				app.switchRedisDatabase(database)
			}
		}
	case app.etcdStore != nil:
		app.etcdNavigation(c, rootDepth+1, "", preferences.ConnectionProfile{})
	}
}

func (app *workbench) etcdNavigation(c *ui.Context, depth int, id string, profile preferences.ConnectionProfile) {
	items := []struct{ section, label, icon string }{
		{section: "keys", label: "键", icon: "key"},
		{section: "auth", label: "用户与角色", icon: "users"},
		{section: "dashboard", label: "服务仪表盘", icon: "dashboard"},
	}
	for _, nav := range items {
		nav := nav
		item := ui.TreeItem(c, treeIconPlaceholder+app.localized(nav.label), nil, nil).
			Selected(app.etcdSidebarSection == nav.section)
		item = connectionTreeDecorations(c, item, "", nav.icon, depth, false, false)
		item.Disabled(app.busy || app.exporting || app.dialogOpen)
		if item.Clicked() {
			if profile.ID == "" || app.profileIsSelected(profile) {
				app.etcdSidebarSection = nav.section
				app.setEtcdSection(nav.section)
				continue
			}
			if app.profileIsActive(profile) {
				app.etcdSidebarSection = nav.section
				app.etcdPendingSection = nav.section
				if app.activateLiveConnectionForWorkspace(id) {
					app.activateConnectionWorkspace(id)
				}
				continue
			}
			app.etcdSidebarSection = nav.section
			app.etcdPendingSection = nav.section
			app.openSavedProfile(profile)
		}
	}
}

func (app *workbench) sqlConnectionTree(c *ui.Context, depth int) {
	if len(app.sqlNamespaces) == 0 {
		app.sqlObjectTree(c, "", depth)
		if app.openedDriver == connection.DriverPostgreSQL {
			app.sqlGlobalObjectTree(c, app.openedConnectionID, app.sqlNamespace, app.objects, depth)
		}
		if app.openedDriver == connection.DriverPostgreSQL || app.openedDriver == connection.DriverMySQL {
			app.sqlUsersTreeItem(c, app.openedConnectionID, app.openedDriver, app.sqlNamespace, depth)
		}
		return
	}
	for _, namespace := range app.sqlNamespaces {
		namespace := namespace
		open := app.connectionTreeState("namespace:"+app.openedConnectionID+":"+namespace, false)
		item := ui.TreeItem(c, treeIconPlaceholder+namespace, open, func() {
			app.sqlObjectTree(c, namespace, depth+1)
		}).Selected(namespace == app.sqlNamespace && app.selectedObject == "").ContextMenu(func(menu *ui.Menu) {
			app.schemaContextMenu(menu, app.openedConnectionID, app.openedDriver, namespace)
		})
		item = connectionTreeDecorations(c, item, "", "schema", depth, false, false)
		if item.Clicked() && (app.openedDriver == connection.DriverPostgreSQL || app.openedDriver == connection.DriverMySQL) {
			app.switchSQLNamespace(namespace)
		}
	}
	if app.openedDriver == connection.DriverPostgreSQL {
		app.sqlGlobalObjectTree(c, app.openedConnectionID, app.sqlNamespace, app.objects, depth)
	}
	if app.openedDriver == connection.DriverPostgreSQL || app.openedDriver == connection.DriverMySQL {
		app.sqlUsersTreeItem(c, app.openedConnectionID, app.openedDriver, app.sqlNamespace, depth)
	}
}

func (app *workbench) sqlUsersTreeItem(c *ui.Context, connectionID string, driver connection.Driver, namespace string, depth int) {
	if driver != connection.DriverPostgreSQL && driver != connection.DriverMySQL {
		return
	}
	item := ui.TreeItem(c, treeIconPlaceholder+app.localized("用户与权限"), nil, nil)
	item = connectionTreeDecorations(c, item, "", "users", depth, false, false)
	if !item.Clicked() {
		return
	}
	if driver == connection.DriverMySQL {
		app.openMySQLUserManager(connectionID, namespace)
		return
	}
	if driver == connection.DriverPostgreSQL {
		app.openPostgresRoleManager(connectionID, namespace)
	}
}

func (app *workbench) sqlObjectTree(c *ui.Context, namespace string, depth int) {
	if app.openedDriver == connection.DriverPostgreSQL {
		app.postgresSchemaObjectTree(c, app.openedConnectionID, namespace, app.objects, depth)
		return
	}
	if app.openedDriver == connection.DriverSQLite {
		app.sqliteObjectTree(c, app.openedConnectionID, namespace, app.objects, depth)
		return
	}
	if app.openedDriver == connection.DriverMySQL {
		app.mysqlObjectTree(c, app.openedConnectionID, namespace, app.objects, depth)
		return
	}
	for _, object := range app.objects {
		if !sqlObjectMatchesNamespace(string(app.openedDriver), namespace, object) {
			continue
		}
		object := object
		app.sqlObjectTreeItem(c, app.openedConnectionID, object, depth)
	}
}

func sqlObjectMatchesNamespace(driver, namespace string, object connection.Object) bool {
	if object.Type == "schema" {
		return false
	}
	if namespace == "" || object.Schema == namespace {
		return true
	}
	// SQLite objects come from sqlite_schema without a schema name, while the
	// tree exposes the primary database as the "main" namespace.
	return driver == string(connection.DriverSQLite) && namespace == "main" && object.Schema == ""
}

func (app *workbench) sqliteObjectTree(c *ui.Context, connectionID, namespace string, objects []connection.Object, depth int) {
	var tables, views, unownedTriggers []connection.Object
	childrenByOwner := make(map[string][]connection.Object)
	for _, object := range objects {
		if !sqlObjectMatchesNamespace(string(connection.DriverSQLite), namespace, object) {
			continue
		}
		switch object.Type {
		case "table":
			tables = append(tables, object)
		case "view":
			views = append(views, object)
		case "index", "trigger":
			if object.TableName != "" {
				childrenByOwner[object.TableName] = append(childrenByOwner[object.TableName], object)
			} else if object.Type == "trigger" {
				unownedTriggers = append(unownedTriggers, object)
			}
		}
	}

	app.sqliteObjectCategory(c, connectionID, namespace, "表", "table", tables, childrenByOwner, depth)
	app.sqliteObjectCategory(c, connectionID, namespace, "视图", "view", views, childrenByOwner, depth)
	if len(unownedTriggers) > 0 {
		app.sqliteObjectCategory(c, connectionID, namespace, "触发器", "trigger", unownedTriggers, nil, depth)
	}
	app.sqlSavedQueriesCategory(c, connectionID, namespace, depth, false)
}

func (app *workbench) sqliteObjectCategory(c *ui.Context, connectionID, namespace, label, typeName string, objects []connection.Object, childrenByOwner map[string][]connection.Object, depth int) {
	displayLabel := fmt.Sprintf("%s %d", app.localized(label), len(objects))
	open := app.connectionTreeState("sqlite-category:"+connectionID+":"+namespace+":"+typeName, false)
	item := ui.TreeItem(c, treeIconPlaceholder+displayLabel, open, func() {
		for _, object := range objects {
			object := object
			children := childrenByOwner[object.Name]
			if len(children) == 0 {
				app.sqlObjectTreeItem(c, connectionID, object, depth+1)
				continue
			}
			app.sqlObjectTreeItemWithChildren(c, connectionID, object, depth+1, children,
				"sqlite-object-children:"+connectionID+":"+namespace+":"+typeName+":"+object.Name)
		}
	})
	item = connectionTreeDecorations(c, item, "", typeName, depth, false, false)
	item.Clicked()
}

func (app *workbench) sqlSavedQueriesCategory(c *ui.Context, connectionID, namespace string, depth int, allNamespaces bool) {
	var queries []preferences.SavedQuery
	if app.preferences != nil {
		for _, query := range app.preferences.Settings().SavedQueries {
			if query.ConnectionID == connectionID && (allNamespaces || query.Namespace == namespace) {
				queries = append(queries, query)
			}
		}
	}
	scopeKey := namespace
	if allNamespaces {
		scopeKey = "all"
	}
	open := app.connectionTreeState("sql-saved-queries:"+connectionID+":"+scopeKey, false)
	item := ui.TreeItem(c, treeIconPlaceholder+app.localized("查询"), open, func() {
		for _, query := range queries {
			query := query
			queryItem := ui.TreeItem(c, treeIconPlaceholder+query.Name, nil, nil).Tooltip(query.Name).ContextMenu(func(menu *ui.Menu) {
				if menu.Item("✎  " + app.localized("编辑自定义查询")).Disabled(app.dialogOpen || app.savedQuerySaving).Chosen() {
					app.openSavedQueryEditor(query.ConnectionID, query.Namespace, &query)
				}
				if menu.Item("⌫  " + app.localized("删除自定义查询")).Disabled(app.dialogOpen || app.savedQuerySaving).Chosen() {
					app.confirmDeleteSavedQuery(query)
				}
			})
			queryItem = connectionTreeDecorations(c, queryItem, "", "query", depth+1, false, false)
			queryItem.Disabled(app.busy || app.exporting || app.dialogOpen || app.queryWorkspaceRequested && app.workspaceCount() >= maxWorkspaces)
			if queryItem.Clicked() {
				app.openSavedSQLQuery(query)
			}
		}
	}).ContextMenu(func(menu *ui.Menu) {
		if menu.Item("＋  " + app.localized("新建查询")).Disabled(app.busy || app.exporting || app.dialogOpen || app.savedQuerySaving).Chosen() {
			app.newSQLQueryWorkspace(connectionID, namespace)
		}
	})
	item = connectionTreeDecorations(c, item, "", "folder", depth, false, false)
	if item.Hovered() || item.FocusWithin() {
		item.Children(func() {
			ui.Spacer(c)
			add := iconButton(c, app.localized("新建查询"), actionIcons["add"]).Size(22, 22).Padding(3).BorderWidth(0).Background(ui.Transparent)
			add.Disabled(app.busy || app.exporting || app.dialogOpen || app.savedQuerySaving)
			if add.Clicked() {
				app.newSQLQueryWorkspace(connectionID, namespace)
			}
		})
	}
	item.Clicked()
}

type postgresTreeCategory struct {
	label    string
	typeName string
	icon     string
}

var postgresSchemaCategories = []postgresTreeCategory{
	{label: "表", typeName: "table", icon: "table"},
	{label: "视图", typeName: "view", icon: "view"},
	{label: "物化视图", typeName: "materialized_view", icon: "view"},
	{label: "存储过程", typeName: "procedure", icon: "query"},
	{label: "函数", typeName: "function", icon: "function"},
	{label: "序列", typeName: "sequence", icon: "sequence"},
	{label: "类型", typeName: "type", icon: "type"},
}

func (app *workbench) postgresSchemaObjectTree(c *ui.Context, connectionID, namespace string, objects []connection.Object, depth int) {
	triggersByTable := make(map[string][]connection.Object)
	for _, object := range objects {
		if object.Schema == namespace && object.Type == "trigger" && object.TableName != "" {
			triggersByTable[object.TableName] = append(triggersByTable[object.TableName], object)
		}
	}
	for _, category := range postgresSchemaCategories {
		category := category
		var categoryObjects []connection.Object
		for _, object := range objects {
			if object.Schema == namespace && object.Type == category.typeName {
				categoryObjects = append(categoryObjects, object)
			}
		}
		children := map[string][]connection.Object(nil)
		if category.typeName == "table" || category.typeName == "view" {
			children = triggersByTable
		}
		app.sqlObjectCategory(c, connectionID, namespace, "postgres-schema", category.label, category.typeName, category.icon, categoryObjects, children, depth)
	}
}

var mysqlSchemaCategories = []postgresTreeCategory{
	{label: "表", typeName: "table", icon: "table"},
	{label: "视图", typeName: "view", icon: "view"},
	{label: "存储过程", typeName: "procedure", icon: "query"},
	{label: "函数", typeName: "function", icon: "function"},
	{label: "触发器", typeName: "trigger", icon: "trigger"},
	{label: "事件", typeName: "event", icon: "event"},
}

func (app *workbench) mysqlObjectTree(c *ui.Context, connectionID, namespace string, objects []connection.Object, depth int) {
	for _, category := range mysqlSchemaCategories {
		category := category
		var categoryObjects []connection.Object
		for _, object := range objects {
			if object.Schema == namespace && object.Type == category.typeName {
				categoryObjects = append(categoryObjects, object)
			}
		}
		app.sqlObjectCategory(c, connectionID, namespace, "mysql-schema", category.label, category.typeName, category.icon, categoryObjects, nil, depth)
	}
	app.sqlSavedQueriesCategory(c, connectionID, namespace, depth, false)
}

func (app *workbench) sqlGlobalObjectTree(c *ui.Context, connectionID, defaultNamespace string, objects []connection.Object, depth int) {
	if app.openedDriver != connection.DriverPostgreSQL {
		return
	}
	app.postgresGlobalObjectTree(c, connectionID, defaultNamespace, objects, depth)
}

func (app *workbench) postgresGlobalObjectTree(c *ui.Context, connectionID, defaultNamespace string, objects []connection.Object, depth int) {
	for _, category := range []postgresTreeCategory{
		{label: "扩展", typeName: "extension", icon: "extension"},
		{label: "事件触发器", typeName: "event_trigger", icon: "trigger"},
	} {
		category := category
		var categoryObjects []connection.Object
		for _, object := range objects {
			if object.Type == category.typeName {
				categoryObjects = append(categoryObjects, object)
			}
		}
		app.sqlObjectCategory(c, connectionID, "global", "postgres-global", category.label, category.typeName, category.icon, categoryObjects, nil, depth)
	}
	app.sqlSavedQueriesCategory(c, connectionID, defaultNamespace, depth, true)
}

func (app *workbench) sqlObjectCategory(c *ui.Context, connectionID, namespace, prefix, label, typeName, icon string, objects []connection.Object, childrenByOwner map[string][]connection.Object, depth int) {
	displayLabel := fmt.Sprintf("%s %d", app.localized(label), len(objects))
	open := app.connectionTreeState(prefix+":"+connectionID+":"+namespace+":"+typeName, false)
	item := ui.TreeItem(c, treeIconPlaceholder+displayLabel, open, func() {
		for _, object := range objects {
			object := object
			children := childrenByOwner[object.Name]
			if len(children) == 0 {
				app.sqlObjectTreeItem(c, connectionID, object, depth+1)
				continue
			}
			app.sqlObjectTreeItemWithChildren(c, connectionID, object, depth+1, children, prefix+":"+connectionID+":"+namespace+":"+typeName+":"+object.Name)
		}
	})
	item = connectionTreeDecorations(c, item, "", icon, depth, false, false)
	item.Clicked()
}

func (app *workbench) sqlObjectTreeItem(c *ui.Context, connectionID string, object connection.Object, depth int) {
	app.sqlObjectTreeItemWithChildren(c, connectionID, object, depth, nil, "")
}

func (app *workbench) sqlObjectTreeItemWithChildren(c *ui.Context, connectionID string, object connection.Object, depth int, children []connection.Object, expansionKey string) {
	var item ui.Element
	if len(children) == 0 {
		item = ui.TreeItem(c, treeIconPlaceholder+object.Name, nil, nil)
	} else {
		open := app.connectionTreeState(expansionKey, false)
		item = ui.TreeItem(c, treeIconPlaceholder+object.Name, open, func() {
			for _, child := range children {
				child := child
				app.sqlObjectTreeItem(c, connectionID, child, depth+1)
			}
		})
	}
	item = item.Tooltip(object.Name).Selected(qualifiedObjectName(object) == app.selectedObject).ContextMenu(func(menu *ui.Menu) {
		app.sqlObjectContextMenuForConnection(menu, connectionID, object)
	})
	item = connectionTreeDecorations(c, item, "", sqlObjectIcon(object.Type), depth, false, false)
	if !item.Clicked() {
		return
	}
	if connectionID != "" && connectionID != app.openedConnectionID {
		if !app.activateLiveConnectionForWorkspace(connectionID) {
			return
		}
		app.openConnectionView(connectionID)
	}
	app.inspectObject(object)
	if isSQLTableOrView(object.Type) {
		app.browseObjectData(object)
	}
}

func (app *workbench) openRedisDatabaseForProfile(profile preferences.ConnectionProfile, database int) {
	if app.busy || app.exporting || app.dialogOpen || profile.Driver != preferences.DriverRedis {
		return
	}
	if app.profileIsSelected(profile) && app.openedDriver == connection.DriverRedis {
		app.switchRedisDatabase(database)
		return
	}
	profile.RedisDB = database
	app.loadConnectionProfile(profile)
	connectionID := redisDatabaseConnectionID(profile, database)
	if app.activateLiveConnectionForWorkspace(connectionID) {
		app.openRedisWorkspace(connectionID)
		return
	}
	app.openRedis(profile, true)
}

func (app *workbench) currentRedisDB() int {
	if live, ok := app.liveConnections[app.openedConnectionID]; ok {
		return live.profile.RedisDB
	}
	return 0
}

func (app *workbench) redisDatabaseLabel(database int) string {
	if live, ok := app.liveConnections[app.openedConnectionID]; ok {
		if alias := strings.TrimSpace(live.profile.RedisDatabaseAliases[database]); alias != "" {
			return alias
		}
	}
	return fmt.Sprintf("db%d", database)
}

func sqlObjectIcon(kind string) string {
	switch kind {
	case "table":
		return "table"
	case "view", "materialized_view":
		return "view"
	case "index":
		return "index"
	case "trigger":
		return "trigger"
	case "procedure":
		return "query"
	case "function":
		return "function"
	case "event":
		return "event"
	case "sequence":
		return "sequence"
	case "type":
		return "type"
	case "extension":
		return "extension"
	default:
		return "database"
	}
}

func isSQLTableOrView(kind string) bool {
	return kind == "table" || kind == "view" || kind == "materialized_view"
}

func (app *workbench) mongoConnectionTree(c *ui.Context, depth int) {
	for _, database := range app.mongoDatabases {
		database := database
		open := app.connectionTreeState("mongo-db:"+app.openedConnectionID+":"+database, false)
		item := ui.TreeItem(c, treeIconPlaceholder+database, open, func() {
			if database != app.mongoSelectedDB {
				return
			}
			for _, collection := range app.mongoCollections {
				collection := collection
				child := ui.TreeItem(c, treeIconPlaceholder+collection, nil, nil).Tooltip(collection).Selected(collection == app.mongoSelectedColl)
				child = connectionTreeDecorations(c, child, "", "table", depth+1, false, false)
				if child.Clicked() {
					app.selectMongoCollection(collection)
				}
			}
		}).Selected(database == app.mongoSelectedDB && app.mongoSelectedColl == "")
		item = connectionTreeDecorations(c, item, "", "database", depth, false, false)
		if item.Clicked() && database != app.mongoSelectedDB {
			*open = true
			app.selectMongoDatabase(database)
		}
	}
}

func sqliteAccess(readOnly bool) sqlite.AccessMode {
	if readOnly {
		return sqlite.ReadOnly
	}
	return sqlite.ReadWriteExisting
}
