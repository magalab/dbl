package app

import (
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/magalab/dbl/internal/connection"
)

func (app *workbench) schemaContextMenu(menu *ui.Menu, connectionID string, driver connection.Driver, schema string) {
	activate := func() bool {
		return connectionID == app.openedConnectionID || app.activateLiveConnectionForWorkspace(connectionID)
	}
	store, readOnly := app.sqlConnectionForID(connectionID)
	canUseWorkspace := store != nil && !app.busy && !app.exporting && !app.dialogOpen
	canAddWorkspace := canUseWorkspace && (!app.queryWorkspaceRequested || app.workspaceCount() < maxWorkspaces)
	if menu.Item("＋  " + app.localized("新建表…")).Disabled(!canAddWorkspace || driver != connection.DriverSQLite && driver != connection.DriverMySQL && driver != connection.DriverPostgreSQL || readOnly).Chosen() {
		if activate() {
			app.openNewTableStructureEditor(schema)
		}
	}
	if menu.Item("▤  " + app.localized("新建查询")).Disabled(!canAddWorkspace).Chosen() {
		if activate() {
			app.openObjectSQLQuery(connection.Object{Schema: schema}, "")
		}
	}
	menu.Separator()
	exportLabel := "导出 Schema"
	if driver == connection.DriverMySQL || driver == connection.DriverSQLite {
		exportLabel = "导出数据库"
	}
	menu.Submenu("⇩  "+app.localized(exportLabel), func(submenu *ui.Menu) {
		addSQLDatabaseExportOptionsForConnection(app, submenu, connectionID, schema)
	})
	if menu.Item("⇧  " + app.localized("导入数据…")).Disabled(!canUseWorkspace || readOnly || app.tableImporterForSQLConnection(connectionID) == nil).Chosen() {
		if activate() {
			app.importCSVIntoNamespace(connectionID, driver, schema)
		}
	}
	menu.Separator()
	if menu.Item("↻  " + app.localized("刷新")).Disabled(!canUseWorkspace).Chosen() {
		if activate() {
			app.refreshCurrentConnection()
		}
	}
}

func addSQLDatabaseExportOptions(app *workbench, menu *ui.Menu, scope string) {
	for _, option := range []struct {
		label       string
		includeData bool
	}{{"仅结构 SQL…", false}, {"结构和数据 SQL…", true}} {
		option := option
		if menu.Item(app.localized(option.label)).Disabled(app.busy || app.exporting || app.dialogOpen || app.store == nil).Chosen() {
			app.exportSQLDatabase(scope, option.includeData)
		}
	}
}

func addSQLDatabaseExportOptionsForConnection(app *workbench, menu *ui.Menu, connectionID, scope string) {
	store, _ := app.sqlConnectionForID(connectionID)
	for _, option := range []struct {
		label       string
		includeData bool
	}{{"仅结构 SQL…", false}, {"结构和数据 SQL…", true}} {
		option := option
		if menu.Item(app.localized(option.label)).Disabled(app.busy || app.exporting || app.dialogOpen || store == nil).Chosen() {
			if connectionID != app.openedConnectionID && !app.activateLiveConnectionForWorkspace(connectionID) {
				return
			}
			app.exportSQLDatabase(scope, option.includeData)
		}
	}
}

func (app *workbench) sqlConnectionForID(connectionID string) (connection.SQL, bool) {
	if connectionID == "" || connectionID == app.openedConnectionID {
		return app.store, app.openedReadOnly
	}
	live, ok := app.liveConnections[connectionID]
	if !ok || live.connection == nil {
		return nil, true
	}
	store, _ := live.connection.(connection.SQL)
	return store, live.profile.ReadOnly
}

func (app *workbench) tableImporterForSQLConnection(connectionID string) connection.TableImporter {
	if connectionID == "" || connectionID == app.openedConnectionID {
		importer, _ := app.store.(connection.TableImporter)
		if app.openedReadOnly {
			return nil
		}
		return importer
	}
	live, ok := app.liveConnections[connectionID]
	if !ok || live.connection == nil || live.profile.ReadOnly {
		return nil
	}
	importer, _ := live.connection.(connection.TableImporter)
	return importer
}

func (app *workbench) sqlObjectsForConnection(connectionID string) []connection.Object {
	var objects []connection.Object
	if connectionID == "" || connectionID == app.openedConnectionID {
		objects = app.objects
	} else if live, ok := app.liveConnections[connectionID]; ok {
		objects = live.tree.objects
	}
	return append([]connection.Object(nil), objects...)
}

func (app *workbench) sqlObjectContextMenu(menu *ui.Menu, object connection.Object) {
	app.sqlObjectContextMenuForConnection(menu, "", object)
}

func (app *workbench) sqlObjectContextMenuForConnection(menu *ui.Menu, connectionID string, object connection.Object) {
	activate := func() bool {
		return connectionID == "" || connectionID == app.openedConnectionID || app.activateLiveConnectionForWorkspace(connectionID)
	}
	if menu.Item("⎘  " + app.localized("复制名称")).Chosen() {
		mygo.Clipboard.WriteText(object.Name)
	}
	menu.Submenu("⌘  "+app.localized("生成 SQL"), func(submenu *ui.Menu) {
		for _, action := range []struct{ label, kind string }{
			{"SELECT 模板", "select"}, {"INSERT 模板", "insert"}, {"UPDATE 模板", "update"}, {"DELETE 模板", "delete"}, {"重命名模板", "rename"},
		} {
			action := action
			if submenu.Item(app.localized(action.label)).Disabled(app.busy || app.exporting || app.dialogOpen || (action.kind == "select" && !isSQLTableOrView(object.Type)) || (action.kind != "select" && object.Type != "table") || (app.queryWorkspaceRequested && app.workspaceCount() >= maxWorkspaces)).Chosen() {
				if activate() {
					app.openObjectSQLTemplate(object, action.kind)
				}
			}
		}
	})
	menu.Separator()
	if menu.Item("▦  " + app.localized("查看数据")).Disabled(!isSQLTableOrView(object.Type) || app.busy || app.exporting || app.dialogOpen).Chosen() {
		if activate() {
			app.browseObjectData(object)
		}
	}
	if menu.Item("▣  " + app.localized("新标签页查看")).Disabled(!isSQLTableOrView(object.Type) || app.busy || app.exporting || app.dialogOpen || app.queryWorkspaceRequested && app.workspaceCount() >= maxWorkspaces).Chosen() {
		if activate() {
			app.openDataWorkspaceInNewTab(object)
		}
	}
	if menu.Item("＋  " + app.localized("新建查询")).Disabled(app.busy || app.exporting || app.dialogOpen).Chosen() {
		if activate() {
			app.openObjectSQLQuery(object, "")
		}
	}
	if menu.Item("⌘  " + app.localized("查看 DDL")).Disabled(!isSQLTableOrView(object.Type) || app.busy || app.exporting || app.dialogOpen || app.aiDraftPending || app.queryWorkspaceRequested && app.workspaceCount() >= maxWorkspaces).Chosen() {
		if activate() {
			app.openTableDDL(object)
		}
	}
	if menu.Item("✎  " + app.localized("编辑表结构")).Disabled(object.Type != "table" || app.busy || app.exporting || app.dialogOpen || app.queryWorkspaceRequested && app.workspaceCount() >= maxWorkspaces).Chosen() {
		if activate() {
			app.openTableStructureEditor(object)
		}
	}
	if menu.Item("↗  " + app.localized("生成重命名 SQL")).Disabled(app.busy || app.exporting || app.dialogOpen || object.Type != "table" || app.queryWorkspaceRequested && app.workspaceCount() >= maxWorkspaces).Chosen() {
		if activate() {
			app.openObjectSQLTemplate(object, "rename")
		}
	}
	if menu.Item("✎  " + app.localized("查看结构")).Disabled(app.busy || app.exporting || app.dialogOpen).Chosen() {
		if activate() {
			app.inspectObject(object)
		}
	}
	if object.Type == "table" {
		menu.Separator()
		menu.Submenu("⇩  "+app.localized("导出数据"), func(submenu *ui.Menu) {
			for _, item := range []struct {
				label  string
				format objectExportFormat
			}{{"导出为 CSV…", objectExportCSV}, {"导出为 JSON…", objectExportJSON}, {"导出为 SQL INSERT…", objectExportSQL}} {
				item := item
				if submenu.Item(app.localized(item.label)).Disabled(app.busy || app.exporting || app.dialogOpen).Chosen() {
					if activate() {
						app.exportSQLObject(object, item.format)
					}
				}
			}
		})
		_, readOnly := app.sqlConnectionForID(connectionID)
		if menu.Item("⇧  " + app.localized("导入数据…")).Disabled(app.busy || app.exporting || app.dialogOpen || readOnly || app.tableImporterForSQLConnection(connectionID) == nil).Chosen() {
			if activate() {
				app.importCSVIntoTable(object)
			}
		}
	}
	driver := sqlObjectDriverForConnection(app, connectionID)
	if menu.Item("⎘  " + app.localized("复制结构 SQL")).Disabled(objectDefinitionSQLForDriver(app, driver, object) == "").Chosen() {
		if activate() {
			mygo.Clipboard.WriteText(objectDefinitionSQLForDriver(app, app.openedDriver, object))
		}
	}
	menu.Separator()
	if menu.Item("↻  " + app.localized("刷新")).Disabled(app.busy || app.exporting || app.dialogOpen).Chosen() {
		if activate() {
			app.refreshCurrentConnection()
		}
	}
}

func (app *workbench) openObjectSQLTemplate(object connection.Object, kind string) {
	app.openObjectSQLQuery(object, tableSQLTemplate(app, object, kind))
}

func (app *workbench) openObjectSQLQuery(object connection.Object, statement string) {
	if !app.openSQLQuery(statement) || app.activeQuery == nil {
		return
	}
	app.activeQuery.queryConnectionID = app.openedConnectionID
	if object.Schema == "" {
		return
	}
	app.activeQuery.queryNamespace = object.Schema
	if !slices.Contains(app.sqlNamespaces, object.Schema) {
		app.sqlNamespaces = append(app.sqlNamespaces, object.Schema)
		sort.Strings(app.sqlNamespaces)
	}
	app.sqlNamespaceChoice = object.Schema
	if object.Schema == app.sqlNamespace {
		return
	}
	if !app.hasOpenedSQLProfile {
		return
	}
	profile := app.openedSQLProfile
	switch app.openedDriver {
	case connection.DriverMySQL:
		profile.Database = object.Schema
		app.openMySQL(profile)
	case connection.DriverPostgreSQL:
		app.openPostgreSQLSchema(profile, object.Schema)
	}
}

func tableSQLTemplate(app *workbench, object connection.Object, kind string) string {
	table := app.quoteIdentifier(object.Name)
	if object.Schema != "" {
		table = app.quoteIdentifier(object.Schema) + "." + table
	}
	switch kind {
	case "rename":
		return "ALTER TABLE " + table + " RENAME TO /* new_table_name */;\n"
	case "alter":
		return "-- Edit this DDL draft before running it. It is not executed automatically.\nALTER TABLE " + table + "\n    /* ADD COLUMN column_name data_type */;\n"
	case "insert":
		return "INSERT INTO " + table + "\n    (/* columns */)\nVALUES\n    (/* values */);\n"
	case "update":
		return "UPDATE " + table + "\nSET /* column = value */\nWHERE /* primary key predicate */;\n"
	case "delete":
		return "DELETE FROM " + table + "\nWHERE /* primary key predicate */;\n"
	default:
		return "SELECT * FROM " + table + ";\n"
	}
}

func objectDefinitionSQL(app *workbench, object connection.Object) string {
	return objectDefinitionSQLForDriver(app, app.openedDriver, object)
}

func objectDefinitionSQLForDriver(app *workbench, driver connection.Driver, object connection.Object) string {
	definition := strings.TrimSpace(object.SQL)
	if definition == "" {
		return ""
	}
	if driver == connection.DriverMySQL && (object.Type == "procedure" || object.Type == "function" || object.Type == "trigger" || object.Type == "event") {
		// information_schema exposes body fragments, not executable CREATE statements.
		return ""
	}
	if driver == connection.DriverPostgreSQL && (object.Type == "view" || object.Type == "materialized_view") || driver == connection.DriverMySQL && object.Type == "view" {
		name := app.quoteIdentifier(object.Name)
		if object.Schema != "" {
			name = app.quoteIdentifier(object.Schema) + "." + name
		}
		if object.Type == "materialized_view" {
			return "CREATE MATERIALIZED VIEW " + name + " AS\n" + strings.TrimSuffix(definition, ";") + ";\n"
		}
		return "CREATE OR REPLACE VIEW " + name + " AS\n" + strings.TrimSuffix(definition, ";") + ";\n"
	}
	return definition
}

func sqlObjectDriverForConnection(app *workbench, connectionID string) connection.Driver {
	if connectionID == "" || connectionID == app.openedConnectionID {
		return app.openedDriver
	}
	if live, ok := app.liveConnections[connectionID]; ok && live.connection != nil {
		return live.connection.Driver()
	}
	for _, driver := range []connection.Driver{
		connection.DriverPostgreSQL, connection.DriverMySQL, connection.DriverSQLite,
		connection.DriverMongoDB, connection.DriverRedis, connection.DriverEtcd,
	} {
		if strings.HasPrefix(connectionID, string(driver)+":") {
			return driver
		}
	}
	return app.openedDriver
}

func tableDDLQuery(app *workbench, object connection.Object) string {
	table := app.quoteIdentifier(object.Name)
	if object.Schema != "" {
		table = app.quoteIdentifier(object.Schema) + "." + table
	}
	switch app.openedDriver {
	case connection.DriverSQLite:
		return "SELECT sql AS ddl FROM sqlite_schema WHERE name = '" + quoteSQLLiteral(object.Name) + "' AND type IN ('table', 'view');\n"
	case connection.DriverMySQL:
		verb := "TABLE"
		if object.Type == "view" || object.Type == "materialized_view" {
			verb = "VIEW"
		}
		return "SHOW CREATE " + verb + " " + table + ";\n"
	case connection.DriverPostgreSQL:
		if object.Type == "view" || object.Type == "materialized_view" {
			return "SELECT CASE WHEN c.relkind = 'm' THEN 'CREATE MATERIALIZED VIEW ' ELSE 'CREATE OR REPLACE VIEW ' END || quote_ident(n.nspname) || '.' || quote_ident(c.relname) || E' AS\\n' || pg_get_viewdef(c.oid, true) || ';' AS ddl FROM pg_class AS c JOIN pg_namespace AS n ON n.oid = c.relnamespace WHERE n.nspname = '" + quoteSQLLiteral(object.Schema) + "' AND c.relname = '" + quoteSQLLiteral(object.Name) + "' AND c.relkind IN ('v', 'm');\n"
		}
		return postgresTableDDLQuery(object)
	default:
		if object.SQL != "" {
			return "SELECT '" + quoteSQLLiteral(objectDefinitionSQL(app, object)) + "' AS ddl;\n"
		}
		return "SELECT 'DDL is not available for this object type' AS ddl;\n"
	}
}

func (app *workbench) openTableDDL(object connection.Object) {
	statement := strings.TrimSpace(tableDDLQuery(app, object))
	if statement == "" || !app.openSQLQuery(statement) {
		return
	}
	// This exact statement is generated from escaped identifiers and contains
	// only catalog reads. The generic SQL classifier treats PostgreSQL catalog
	// functions as unknown, so approve this metadata query without a write prompt.
	app.sqlConfirmationApproved = ""
	if requiresSQLConfirmation(statement, app.openedReadOnly) {
		app.sqlConfirmationApproved = app.openedConnectionID + "\n" + statement
	}
	app.executeSQL()
}

func quoteSQLLiteral(value string) string {
	return strings.ReplaceAll(value, "'", "''")
}

func postgresTableDDLQuery(object connection.Object) string {
	return postgresTableDDLQueryWithForeignKeys(object, true)
}

func postgresTableDumpDDLQuery(object connection.Object) string {
	return postgresTableDDLQueryWithForeignKeys(object, false)
}

func postgresTableDDLQueryWithForeignKeys(object connection.Object, includeForeignKeys bool) string {
	schema := quoteSQLLiteral(object.Schema)
	name := quoteSQLLiteral(object.Name)
	foreignKeyFilter := ""
	if !includeForeignKeys {
		foreignKeyFilter = " AND con.contype <> 'f'"
	}
	return `WITH target AS (
    SELECT c.oid, c.relkind, n.nspname, c.relname
    FROM pg_class AS c
    JOIN pg_namespace AS n ON n.oid = c.relnamespace
    WHERE n.nspname = '` + schema + `' AND c.relname = '` + name + `' AND c.relkind IN ('r', 'p')
)
SELECT 'CREATE TABLE ' || quote_ident(t.nspname) || '.' || quote_ident(t.relname) || E' (\n' ||
       cols.definitions || COALESCE(E',\n' || constraints.definitions, '') || E'\n);' AS ddl
FROM target AS t
LEFT JOIN LATERAL (
    SELECT string_agg(
        '    ' || quote_ident(a.attname) || ' ' || format_type(a.atttypid, a.atttypmod) ||
        CASE
            WHEN a.attgenerated <> '' THEN ' GENERATED ALWAYS AS (' || pg_get_expr(d.adbin, d.adrelid) || ') STORED'
            WHEN a.attidentity = 'a' THEN ' GENERATED ALWAYS AS IDENTITY'
            WHEN a.attidentity = 'd' THEN ' GENERATED BY DEFAULT AS IDENTITY'
            WHEN d.adbin IS NOT NULL THEN ' DEFAULT ' || pg_get_expr(d.adbin, d.adrelid)
            ELSE ''
        END ||
        CASE WHEN a.attnotnull AND a.attidentity = '' THEN ' NOT NULL' ELSE '' END,
        E',\n' ORDER BY a.attnum
    ) AS definitions
    FROM pg_attribute AS a
    LEFT JOIN pg_attrdef AS d ON d.adrelid = a.attrelid AND d.adnum = a.attnum
    WHERE a.attrelid = t.oid AND a.attnum > 0 AND NOT a.attisdropped
) AS cols ON true
LEFT JOIN LATERAL (
    SELECT string_agg('    ' || pg_get_constraintdef(con.oid, true), E',\n' ORDER BY con.conname) AS definitions
    FROM pg_constraint AS con
    WHERE con.conrelid = t.oid` + foreignKeyFilter + `
) AS constraints ON true;
`
}

func objectDataQuery(app *workbench, object connection.Object) string {
	name := app.quoteIdentifier(object.Name)
	if object.Schema != "" {
		name = app.quoteIdentifier(object.Schema) + "." + name
	}
	return "SELECT * FROM " + name + ";\n"
}

func (app *workbench) browseObjectData(object connection.Object) {
	app.openDataWorkspace(object)
}

func (app *workbench) openSQLQuery(statement string) bool {
	if app.store == nil || app.busy || app.exporting || app.dialogOpen || app.workspaceCount() >= maxWorkspaces && app.queryWorkspaceRequested {
		return false
	}
	app.addWorkspace()
	if app.activeQuery == nil {
		return false
	}
	app.activeQuery.query.Buffer.Set(statement)
	app.activeQuery.columns = nil
	app.activeQuery.resultSQL = ""
	return true
}

func (app *workbench) refreshCurrentConnection() {
	switch {
	case app.store != nil:
		app.refreshSQLObjects()
	case app.keyStore != nil && app.redisOverview:
		app.refreshRedisInstance()
	case app.keyStore != nil:
		app.scanRedis(true)
	case app.documentStore != nil:
		app.refreshMongoDatabases()
	case app.etcdStore != nil:
		app.refreshEtcd(true)
	}
}

func (app *workbench) refreshMongoDatabases() {
	if app.busy || app.exporting || app.dialogOpen || app.documentStore == nil {
		return
	}
	store, id := app.documentStore, app.openedConnectionID
	app.busy = true
	ctx, cancel := app.withTaskTimeout(15 * time.Second)
	go func() {
		defer cancel()
		databases, err := store.Databases(ctx)
		app.window.Update(func() {
			app.busy = false
			if app.openedConnectionID != id {
				return
			}
			if err != nil {
				app.reportError("刷新 MongoDB 数据库失败", err)
				return
			}
			app.mongoDatabases = databases
			app.mongoCollections = nil
			app.mongoSelectedDB, app.mongoSelectedColl = "", ""
			app.status = app.localized("MongoDB 数据库列表已刷新。")
		})
	}()
}

func (app *workbench) refreshSQLObjects() {
	if app.busy || app.exporting || app.dialogOpen || app.store == nil {
		return
	}
	store, id := app.store, app.openedConnectionID
	app.busy = true
	ctx, cancel := app.withTaskTimeout(10 * time.Second)
	go func() {
		defer cancel()
		page, err := store.Objects(ctx, "", "", "", sqliteObjectPageSize)
		var namespaces []string
		if err == nil {
			namespaces, _ = store.Namespaces(ctx)
		}
		app.window.Update(func() {
			app.busy = false
			if app.openedConnectionID != id {
				return
			}
			if err != nil {
				app.reportError("刷新数据库对象失败", err)
				return
			}
			app.objects = page.Objects
			app.objectCursorSchema, app.objectCursorType, app.objectCursorName = page.NextSchema, page.NextType, page.NextName
			app.objectsHasMore = page.HasMore
			app.sqlNamespaces = namespaces
			app.status = app.localized("数据库对象已刷新。")
		})
	}()
}

func (app *workbench) quoteIdentifier(name string) string {
	quote := `"`
	if app.openedDriver == connection.DriverMySQL {
		quote = "`"
	}
	return quote + strings.ReplaceAll(name, quote, quote+quote) + quote
}
