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
			app.postgresUsersTreeItem(c, app.openedConnectionID, depth)
		}
		return
	}
	for _, namespace := range app.sqlNamespaces {
		namespace := namespace
		open := app.connectionTreeState("namespace:"+app.openedConnectionID+":"+namespace, false)
		item := ui.TreeItem(c, treeIconPlaceholder+namespace, open, func() {
			app.sqlObjectTree(c, namespace, depth+1)
		}).Selected(namespace == app.sqlNamespace && app.selectedObject == "").ContextMenu(func(menu *ui.Menu) {
			app.schemaContextMenu(menu, namespace)
		})
		item = connectionTreeDecorations(c, item, "", "schema", depth, false, false)
		if item.Clicked() && (app.openedDriver == connection.DriverPostgreSQL || app.openedDriver == connection.DriverMySQL) {
			app.switchSQLNamespace(namespace)
		}
	}
	if app.openedDriver == connection.DriverPostgreSQL {
		app.postgresUsersTreeItem(c, app.openedConnectionID, depth)
	}
}

func (app *workbench) postgresUsersTreeItem(c *ui.Context, connectionID string, depth int) {
	item := ui.TreeItem(c, treeIconPlaceholder+app.localized("用户与权限"), nil, nil)
	item = connectionTreeDecorations(c, item, "", "users", depth, false, false)
	if !item.Clicked() {
		return
	}
	if connectionID != "" && connectionID != app.openedConnectionID {
		if !app.activateLiveConnectionForWorkspace(connectionID) {
			return
		}
		app.activateConnectionWorkspace(connectionID)
	}
	statement := "SELECT rolname, rolsuper, rolinherit, rolcreaterole, rolcreatedb, rolcanlogin FROM pg_catalog.pg_roles ORDER BY rolname;\n"
	if !app.openSQLQuery(statement) {
		app.status = app.localized("无法打开用户与权限查询工作区。 ")
		return
	}
	app.status = app.localized("已打开用户与权限查询草稿。")
}

func (app *workbench) sqlObjectTree(c *ui.Context, namespace string, depth int) {
	if app.openedDriver == connection.DriverPostgreSQL {
		app.postgresSchemaObjectTree(c, app.openedConnectionID, namespace, app.objects, depth)
		return
	}
	for _, object := range app.objects {
		if object.Type == "schema" {
			continue
		}
		if namespace != "" && object.Schema != namespace {
			continue
		}
		object := object
		app.sqlObjectTreeItem(c, app.openedConnectionID, object, depth)
	}
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
	{label: "扩展", typeName: "extension", icon: "extension"},
	{label: "事件触发器", typeName: "event_trigger", icon: "trigger"},
	{label: "查询", typeName: "query", icon: "query"},
}

func (app *workbench) postgresSchemaObjectTree(c *ui.Context, connectionID, namespace string, objects []connection.Object, depth int) {
	for _, category := range postgresSchemaCategories {
		category := category
		var categoryObjects []connection.Object
		for _, object := range objects {
			if object.Schema == namespace && object.Type == category.typeName {
				categoryObjects = append(categoryObjects, object)
			}
		}
		if len(categoryObjects) == 0 {
			continue
		}
		label := app.localized(category.label)
		open := app.connectionTreeState("category:"+connectionID+":"+namespace+":"+category.typeName, false)
		item := ui.TreeItem(c, treeIconPlaceholder+label, open, func() {
			for _, object := range categoryObjects {
				app.sqlObjectTreeItem(c, connectionID, object, depth+1)
			}
		})
		item = connectionTreeDecorations(c, item, "", category.icon, depth, false, false)
		item.Clicked()
	}
}

func (app *workbench) sqlObjectTreeItem(c *ui.Context, connectionID string, object connection.Object, depth int) {
	item := ui.TreeItem(c, treeIconPlaceholder+object.Name, nil, nil).Tooltip(object.Name).Selected(qualifiedObjectName(object) == app.selectedObject).ContextMenu(func(menu *ui.Menu) {
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
	case "function", "procedure":
		return "function"
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
