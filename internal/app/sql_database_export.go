package app

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/egoist/mygo"
	"github.com/magalab/dbl/internal/connection"
	"github.com/magalab/dbl/internal/safefile"
)

func (app *workbench) exportSQLDatabase(scope string, includeData bool) {
	if app.busy || app.exporting || app.dialogOpen || app.store == nil {
		return
	}
	store, connectionID, driver, openedPath := app.store, app.openedConnectionID, app.openedDriver, app.openedPath
	name := scope
	if name == "" {
		name = app.openedSQLProfile.Database
	}
	if name == "" {
		name = app.openedLabel
	}
	defaultPath := safeExportName(name) + "-schema.sql"
	if includeData {
		defaultPath = safeExportName(name) + "-backup.sql"
	}
	app.dialogOpen = true
	app.status = app.localized("请选择 SQL 导出文件保存位置…")
	go func() {
		path, err := mygo.Dialog.Save(mygo.SaveDialogOptions{
			Parent: app.window, Title: app.localized("导出数据库") + " · " + app.localized(databaseDumpModeLabel(includeData)),
			ButtonLabel: app.localized("导出"), DefaultPath: defaultPath,
			Filters: []mygo.FileFilter{{Name: "SQL", Extensions: []string{"sql"}}},
		})
		app.window.Update(func() {
			app.dialogOpen = false
			if err != nil {
				app.reportError("选择导出文件位置失败", err)
				return
			}
			if path == "" {
				app.status = app.localized("已取消导出。")
				return
			}
			if app.store != store || app.openedConnectionID != connectionID || app.openedDriver != driver || driver == connection.DriverSQLite && app.openedPath != openedPath {
				app.status = app.localized("导出来源已变化，请重新选择结果后导出。")
				return
			}
			if filepath.Ext(path) == "" {
				path += ".sql"
			}
			app.exportSQLDatabaseToPath(store, driver, scope, includeData, path)
		})
	}()
}

func databaseDumpModeLabel(includeData bool) string {
	if includeData {
		return "结构和数据"
	}
	return "仅结构"
}

func (app *workbench) exportSQLDatabaseToPath(store connection.SQL, driver connection.Driver, scope string, includeData bool, path string) {
	ctx, cancel := app.withTaskTimeout(exportTimeout)
	app.cancelExport = cancel
	app.exporting = true
	app.status = app.localized("正在导出数据库结构…")
	if includeData {
		app.status = app.localized("正在导出数据库结构和数据…")
	}
	go func() {
		file, err := safefile.CreateExport(path)
		var tableCount, rowCount int64
		if err == nil {
			tableCount, rowCount, err = writeSQLDatabaseDump(ctx, store, driver, scope, includeData, file.File())
			if err == nil {
				err = file.Commit()
			}
			if err != nil {
				if cleanupErr := file.Abort(); cleanupErr != nil {
					err = errors.Join(err, cleanupErr)
				}
			}
		}
		canceled := err != nil && (errors.Is(err, context.Canceled) || errors.Is(ctx.Err(), context.Canceled))
		cancel()
		app.window.Update(func() {
			app.cancelExport = nil
			app.exporting = false
			switch {
			case canceled:
				app.status = app.localized("导出已取消，临时文件已清理。")
			case err != nil && safefile.IsPublished(err):
				app.status = fmt.Sprintf(app.localized("导出文件已完整生成到 %s，但目录同步失败，断电后是否保留无法确认。"), path)
			case err != nil:
				app.reportError("导出数据库失败", err)
			default:
				app.status = fmt.Sprintf(app.localized("已导出 %d 张表和 %d 行到 %s。"), tableCount, rowCount, path)
			}
		})
	}()
}

func writeSQLDatabaseDump(ctx context.Context, store connection.SQL, driver connection.Driver, scope string, includeData bool, output io.Writer) (int64, int64, error) {
	objects, err := readAllSQLObjects(ctx, store)
	if err != nil {
		return 0, 0, err
	}
	objects = filterDumpObjects(objects, driver, scope)
	sort.SliceStable(objects, func(i, j int) bool {
		return dumpObjectOrder(driver, objects[i]) < dumpObjectOrder(driver, objects[j])
	})
	writer := bufio.NewWriter(output)
	if _, err := fmt.Fprintf(writer, "-- DBL SQL export\n-- Driver: %s\n-- Scope: %s\n-- Mode: %s\n\n", driver, scope, databaseDumpModeLabel(includeData)); err != nil {
		return 0, 0, err
	}
	if driver == connection.DriverMySQL && scope != "" {
		if _, err := fmt.Fprintf(writer, "CREATE DATABASE IF NOT EXISTS %s;\nUSE %s;\n\n", quoteIdentifierForDriver(driver, scope), quoteIdentifierForDriver(driver, scope)); err != nil {
			return 0, 0, err
		}
	}
	if driver == connection.DriverMySQL {
		if _, err := io.WriteString(writer, "SET FOREIGN_KEY_CHECKS=0;\n\n"); err != nil {
			return 0, 0, err
		}
	}
	localApp := &workbench{openedDriver: driver}
	var tableCount int64
	var rowCount int64
	var tables []connection.Object
	var sequences []connection.Object
	var deferredObjects []connection.Object
	for _, object := range objects {
		if err := ctx.Err(); err != nil {
			return tableCount, rowCount, err
		}
		if object.Type == "table" {
			tables = append(tables, object)
		} else if driver == connection.DriverPostgreSQL && object.Type == "sequence" {
			sequences = append(sequences, object)
		}
		if dumpObjectNeedsDataDeferral(driver, object) {
			deferredObjects = append(deferredObjects, object)
			continue
		}
		ddl, ddlErr := sqlObjectDumpDDL(ctx, store, localApp, driver, object)
		if ddlErr != nil {
			return tableCount, rowCount, fmt.Errorf("export %s %s: %w", object.Type, qualifiedObjectName(object), ddlErr)
		}
		if strings.TrimSpace(ddl) != "" {
			if driver == connection.DriverMySQL && mysqlObjectNeedsDelimiter(object.Type) {
				if _, err := fmt.Fprintf(writer, "DELIMITER $$\n%s$$\nDELIMITER ;\n\n", strings.TrimSuffix(strings.TrimSpace(ddl), ";")); err != nil {
					return tableCount, rowCount, err
				}
				continue
			}
			if _, err := fmt.Fprintf(writer, "%s\n\n", strings.TrimSuffix(strings.TrimSpace(ddl), ";")+";"); err != nil {
				return tableCount, rowCount, err
			}
		}
	}
	if driver == connection.DriverPostgreSQL {
		// Helpers write directly to output, so drain the buffered DDL first to
		// preserve script order.
		if err := writer.Flush(); err != nil {
			return tableCount, rowCount, err
		}
		if err := writePostgreSQLForeignKeys(ctx, store, scope, writer); err != nil {
			return tableCount, rowCount, err
		}
		if err := writePostgreSQLIndexes(ctx, store, scope, writer); err != nil {
			return tableCount, rowCount, err
		}
		if err := writePostgreSQLSequenceOwnership(ctx, store, sequences, writer); err != nil {
			return tableCount, rowCount, err
		}
	}
	if includeData {
		if driver == connection.DriverPostgreSQL {
			tables, err = orderPostgreSQLTablesByDependencies(ctx, store, tables, scope)
			if err != nil {
				return tableCount, rowCount, err
			}
		}
		transaction := "BEGIN;"
		if driver == connection.DriverMySQL {
			transaction = "START TRANSACTION;"
		}
		if len(tables) > 0 {
			if _, err := fmt.Fprintf(writer, "\n%s\n", transaction); err != nil {
				return tableCount, rowCount, err
			}
			if driver == connection.DriverPostgreSQL {
				if err := writePostgreSQLDeferrableConstraints(ctx, store, scope, writer); err != nil {
					return tableCount, rowCount, err
				}
			}
			if driver == connection.DriverSQLite {
				if _, err := io.WriteString(writer, "PRAGMA defer_foreign_keys=ON;\n"); err != nil {
					return tableCount, rowCount, err
				}
			}
		}
		for _, table := range tables {
			if err := ctx.Err(); err != nil {
				return tableCount, rowCount, err
			}
			count, err := writeSQLInsertRows(ctx, store, driver, table, writer, true)
			if err != nil {
				return tableCount, rowCount, fmt.Errorf("export data for %s: %w", qualifiedObjectName(table), err)
			}
			rowCount += count
			tableCount++
		}
		if driver == connection.DriverPostgreSQL {
			for _, sequence := range sequences {
				if err := writePostgreSQLSequenceState(ctx, store, sequence, writer); err != nil {
					return tableCount, rowCount, err
				}
			}
		}
		if driver == connection.DriverSQLite {
			if err := writeSQLiteSequenceState(ctx, store, tables, writer); err != nil {
				return tableCount, rowCount, err
			}
		}
		if len(tables) > 0 {
			if _, err := io.WriteString(writer, "COMMIT;\n"); err != nil {
				return tableCount, rowCount, err
			}
		}
		if driver == connection.DriverMySQL {
			if _, err := io.WriteString(writer, "SET FOREIGN_KEY_CHECKS=1;\n"); err != nil {
				return tableCount, rowCount, err
			}
		}
	}
	if driver == connection.DriverMySQL && !includeData {
		if _, err := io.WriteString(writer, "\nSET FOREIGN_KEY_CHECKS=1;\n"); err != nil {
			return tableCount, rowCount, err
		}
	}
	for _, object := range deferredObjects {
		if err := ctx.Err(); err != nil {
			return tableCount, rowCount, err
		}
		ddl, ddlErr := sqlObjectDumpDDL(ctx, store, localApp, driver, object)
		if ddlErr != nil {
			return tableCount, rowCount, fmt.Errorf("export %s %s: %w", object.Type, qualifiedObjectName(object), ddlErr)
		}
		if err := writeSQLDumpObjectDDL(writer, driver, object, ddl); err != nil {
			return tableCount, rowCount, err
		}
	}
	if err := writer.Flush(); err != nil {
		return tableCount, rowCount, err
	}
	if !includeData {
		tableCount = int64(len(tables))
	}
	return tableCount, rowCount, nil
}

func dumpObjectNeedsDataDeferral(driver connection.Driver, object connection.Object) bool {
	switch driver {
	case connection.DriverSQLite:
		return object.Type == "trigger"
	case connection.DriverMySQL:
		return object.Type == "trigger" || object.Type == "event"
	case connection.DriverPostgreSQL:
		return object.Type == "trigger" || object.Type == "event_trigger"
	default:
		return false
	}
}

func writeSQLDumpObjectDDL(writer io.Writer, driver connection.Driver, object connection.Object, ddl string) error {
	if strings.TrimSpace(ddl) == "" {
		return nil
	}
	if driver == connection.DriverMySQL && mysqlObjectNeedsDelimiter(object.Type) {
		_, err := fmt.Fprintf(writer, "DELIMITER $$\n%s$$\nDELIMITER ;\n\n", strings.TrimSuffix(strings.TrimSpace(ddl), ";"))
		return err
	}
	_, err := fmt.Fprintf(writer, "%s\n\n", strings.TrimSuffix(strings.TrimSpace(ddl), ";")+";")
	return err
}

func writeSQLiteSequenceState(ctx context.Context, store connection.SQL, tables []connection.Object, output io.Writer) error {
	cursor, err := store.StartQuery(ctx, "SELECT 1 FROM sqlite_schema WHERE type = 'table' AND name = 'sqlite_sequence' LIMIT 1")
	if err != nil {
		return err
	}
	exists, _, err := cursor.NextBatch(1)
	closeErr := cursor.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if len(exists) == 0 {
		return nil
	}
	allowed := make(map[string]bool, len(tables))
	for _, table := range tables {
		allowed[table.Name] = true
	}
	sequenceRows, err := store.StartQuery(ctx, "SELECT name, seq FROM sqlite_sequence ORDER BY name")
	if err != nil {
		return err
	}
	defer sequenceRows.Close()
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		rows, done, err := sequenceRows.NextBatch(100)
		if err != nil {
			return err
		}
		for _, row := range rows {
			if len(row) < 2 {
				continue
			}
			name := fmt.Sprint(row[0])
			if !allowed[name] {
				continue
			}
			sequence, err := strconv.ParseInt(fmt.Sprint(row[1]), 10, 64)
			if err != nil {
				return fmt.Errorf("invalid SQLite sequence value for %q: %w", name, err)
			}
			literal := quoteSQLLiteral(name)
			if _, err := fmt.Fprintf(output, "DELETE FROM sqlite_sequence WHERE name = '%s';\nINSERT INTO sqlite_sequence(name, seq) VALUES ('%s', %d);\n", literal, literal, sequence); err != nil {
				return err
			}
		}
		if done {
			return nil
		}
	}
}

func orderPostgreSQLTablesByDependencies(ctx context.Context, store connection.SQL, tables []connection.Object, scope string) ([]connection.Object, error) {
	if len(tables) < 2 {
		return tables, nil
	}
	filter := ""
	if scope != "" {
		filter = " AND child_ns.nspname = '" + quoteSQLLiteral(scope) + "'"
	}
	statement := `SELECT child_ns.nspname, child.relname, parent_ns.nspname, parent.relname
	FROM pg_catalog.pg_constraint AS con
	JOIN pg_catalog.pg_class AS child ON child.oid = con.conrelid
	JOIN pg_catalog.pg_namespace AS child_ns ON child_ns.oid = child.relnamespace
	JOIN pg_catalog.pg_class AS parent ON parent.oid = con.confrelid
	JOIN pg_catalog.pg_namespace AS parent_ns ON parent_ns.oid = parent.relnamespace
	WHERE con.contype = 'f'` + filter + ` ORDER BY child_ns.nspname, child.relname, parent_ns.nspname, parent.relname`
	cursor, err := store.StartQuery(ctx, statement)
	if err != nil {
		return nil, err
	}
	defer cursor.Close()
	type edge struct{ parent, child string }
	var edges []edge
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		rows, done, err := cursor.NextBatch(100)
		if err != nil {
			return nil, err
		}
		for _, row := range rows {
			if len(row) >= 4 {
				edges = append(edges, edge{
					child:  fmt.Sprint(row[0]) + "\x00" + fmt.Sprint(row[1]),
					parent: fmt.Sprint(row[2]) + "\x00" + fmt.Sprint(row[3]),
				})
			}
		}
		if done {
			break
		}
	}
	order := make(map[string]int, len(tables))
	objects := make(map[string]connection.Object, len(tables))
	for index, table := range tables {
		key := table.Schema + "\x00" + table.Name
		order[key], objects[key] = index, table
	}
	indegree := make(map[string]int, len(tables))
	children := make(map[string]map[string]bool, len(tables))
	for _, relation := range edges {
		if _, found := objects[relation.parent]; !found {
			continue
		}
		if _, found := objects[relation.child]; !found || relation.child == relation.parent {
			continue
		}
		if children[relation.parent] == nil {
			children[relation.parent] = make(map[string]bool)
		}
		if !children[relation.parent][relation.child] {
			children[relation.parent][relation.child] = true
			indegree[relation.child]++
		}
	}
	ready := make([]string, 0, len(tables))
	for _, table := range tables {
		key := table.Schema + "\x00" + table.Name
		if indegree[key] == 0 {
			ready = append(ready, key)
		}
	}
	ordered := make([]connection.Object, 0, len(tables))
	seen := make(map[string]bool, len(tables))
	for len(ready) > 0 {
		key := ready[0]
		ready = ready[1:]
		ordered = append(ordered, objects[key])
		seen[key] = true
		for child := range children[key] {
			indegree[child]--
			if indegree[child] == 0 {
				ready = append(ready, child)
				sort.SliceStable(ready, func(i, j int) bool { return order[ready[i]] < order[ready[j]] })
			}
		}
	}
	for _, table := range tables {
		key := table.Schema + "\x00" + table.Name
		if !seen[key] {
			ordered = append(ordered, table)
		}
	}
	return ordered, nil
}

func readAllSQLObjects(ctx context.Context, store connection.SQL) ([]connection.Object, error) {
	objects := make([]connection.Object, 0)
	afterSchema, afterType, afterName := "", "", ""
	for {
		page, err := store.Objects(ctx, afterSchema, afterType, afterName, 500)
		if err != nil {
			return nil, err
		}
		objects = append(objects, page.Objects...)
		if !page.HasMore || len(page.Objects) == 0 {
			return objects, nil
		}
		afterSchema, afterType, afterName = page.NextSchema, page.NextType, page.NextName
	}
}

func filterDumpObjects(objects []connection.Object, driver connection.Driver, scope string) []connection.Object {
	filtered := make([]connection.Object, 0, len(objects))
	for _, object := range objects {
		if scope != "" && object.Schema != scope && !(driver == connection.DriverSQLite && scope == "main" && object.Schema == "") {
			continue
		}
		switch driver {
		case connection.DriverSQLite:
			if object.Type == "table" || object.Type == "view" || object.Type == "index" || object.Type == "trigger" {
				filtered = append(filtered, object)
			}
		case connection.DriverMySQL:
			if object.Type == "table" || object.Type == "view" || object.Type == "procedure" || object.Type == "function" || object.Type == "trigger" || object.Type == "event" {
				filtered = append(filtered, object)
			}
		case connection.DriverPostgreSQL:
			switch object.Type {
			case "schema", "type", "table", "view", "materialized_view", "sequence", "function", "procedure", "trigger", "event_trigger", "extension":
				filtered = append(filtered, object)
			}
		}
	}
	return filtered
}

func dumpObjectOrder(driver connection.Driver, object connection.Object) int {
	if driver == connection.DriverSQLite {
		switch object.Type {
		case "table":
			return 1
		case "view":
			return 2
		case "index":
			return 3
		case "trigger":
			return 4
		}
	}
	switch object.Type {
	case "schema":
		return 0
	case "extension":
		return 1
	case "type", "sequence":
		return 2
	case "table":
		return 3
	case "function", "procedure":
		return 4
	case "view", "materialized_view":
		return 5
	case "trigger", "event_trigger":
		return 6
	default:
		return 5
	}
}

func sqlObjectDumpDDL(ctx context.Context, store connection.SQL, app *workbench, driver connection.Driver, object connection.Object) (string, error) {
	if driver == connection.DriverSQLite {
		return object.SQL, nil
	}
	if driver == connection.DriverMySQL && object.Type == "table" || driver == connection.DriverPostgreSQL && object.Type == "table" {
		statement := tableDDLQuery(app, object)
		if driver == connection.DriverPostgreSQL {
			statement = postgresTableDumpDDLQuery(object)
		}
		return readSQLDDL(ctx, store, statement, driver)
	}
	if driver == connection.DriverMySQL {
		switch object.Type {
		case "view":
			return readSQLDDL(ctx, store, tableDDLQuery(app, object), driver)
		case "procedure", "function", "trigger", "event":
			return readMySQLObjectDDL(ctx, store, object)
		}
	}
	if driver == connection.DriverPostgreSQL && object.Type == "schema" {
		return "CREATE SCHEMA IF NOT EXISTS " + quoteIdentifierForDriver(driver, object.Name) + ";", nil
	}
	if driver == connection.DriverPostgreSQL && object.Type == "sequence" {
		return readPostgreSQLSequenceDDL(ctx, store, object)
	}
	if driver == connection.DriverPostgreSQL && object.Type == "type" {
		return readPostgreSQLTypeDDL(ctx, store, object)
	}
	return objectDefinitionSQLForDriver(app, driver, object), nil
}

func mysqlObjectNeedsDelimiter(objectType string) bool {
	switch objectType {
	case "procedure", "function", "trigger", "event":
		return true
	default:
		return false
	}
}

func readMySQLObjectDDL(ctx context.Context, store connection.SQL, object connection.Object) (string, error) {
	verb, columnName := "", ""
	switch object.Type {
	case "procedure":
		verb, columnName = "PROCEDURE", "Create Procedure"
	case "function":
		verb, columnName = "FUNCTION", "Create Function"
	case "trigger":
		verb, columnName = "TRIGGER", "SQL Original Statement"
	case "event":
		verb, columnName = "EVENT", "Create Event"
	default:
		return "", fmt.Errorf("unsupported MySQL object type %q", object.Type)
	}
	name := quoteIdentifierForDriver(connection.DriverMySQL, object.Name)
	if object.Schema != "" {
		name = quoteIdentifierForDriver(connection.DriverMySQL, object.Schema) + "." + name
	}
	cursor, err := store.StartQuery(ctx, "SHOW CREATE "+verb+" "+name)
	if err != nil {
		return "", err
	}
	defer cursor.Close()
	column := -1
	for index, metadata := range cursor.Columns() {
		if strings.EqualFold(metadata.Name, columnName) {
			column = index
			break
		}
	}
	if column < 0 {
		return "", fmt.Errorf("MySQL SHOW CREATE %s did not return %q", verb, columnName)
	}
	rows, _, err := cursor.NextBatch(1)
	if err != nil {
		return "", err
	}
	if len(rows) == 0 || column >= len(rows[0]) {
		return "", fmt.Errorf("MySQL SHOW CREATE %s returned no definition for %s", verb, qualifiedObjectName(object))
	}
	if rows[0][column] == nil || strings.TrimSpace(fmt.Sprint(rows[0][column])) == "" {
		return "", fmt.Errorf("MySQL SHOW CREATE %s returned an empty definition for %s", verb, qualifiedObjectName(object))
	}
	return fmt.Sprint(rows[0][column]), nil
}

func readPostgreSQLSequenceDDL(ctx context.Context, store connection.SQL, object connection.Object) (string, error) {
	schema, name := quoteSQLLiteral(object.Schema), quoteSQLLiteral(object.Name)
	statement := `SELECT CASE WHEN EXISTS (
		SELECT 1 FROM pg_catalog.pg_class AS seq
		JOIN pg_catalog.pg_namespace AS ns ON ns.oid = seq.relnamespace
		JOIN pg_catalog.pg_depend AS dep ON dep.classid = 'pg_catalog.pg_class'::regclass AND dep.objid = seq.oid
		WHERE ns.nspname = '` + schema + `' AND seq.relname = '` + name + `' AND dep.deptype = 'i'
	) THEN '' ELSE
	'CREATE SEQUENCE ' || pg_catalog.quote_ident(ns.nspname) || '.' || pg_catalog.quote_ident(seq.relname) ||
	' AS ' || pg_catalog.format_type(info.seqtypid, NULL) ||
	' INCREMENT BY ' || info.seqincrement::text ||
	' MINVALUE ' || info.seqmin::text ||
	' MAXVALUE ' || info.seqmax::text ||
	' START WITH ' || info.seqstart::text ||
	' CACHE ' || info.seqcache::text ||
	CASE WHEN info.seqcycle THEN ' CYCLE' ELSE ' NO CYCLE' END || ';'
	END AS ddl
	FROM pg_catalog.pg_class AS seq
	JOIN pg_catalog.pg_namespace AS ns ON ns.oid = seq.relnamespace
	JOIN pg_catalog.pg_sequence AS info ON info.seqrelid = seq.oid
	WHERE ns.nspname = '` + schema + `' AND seq.relname = '` + name + `' AND seq.relkind = 'S'`
	return readSQLDDL(ctx, store, statement, connection.DriverPostgreSQL)
}

func readPostgreSQLTypeDDL(ctx context.Context, store connection.SQL, object connection.Object) (string, error) {
	schema, name := quoteSQLLiteral(object.Schema), quoteSQLLiteral(object.Name)
	statement := `SELECT CASE t.typtype
	WHEN 'e' THEN 'CREATE TYPE ' || pg_catalog.quote_ident(ns.nspname) || '.' || pg_catalog.quote_ident(t.typname) ||
		' AS ENUM (' || labels.enum_values || ');'
	WHEN 'd' THEN 'CREATE DOMAIN ' || pg_catalog.quote_ident(ns.nspname) || '.' || pg_catalog.quote_ident(t.typname) ||
		' AS ' || pg_catalog.format_type(t.typbasetype, t.typtypmod) ||
		CASE WHEN t.typnotnull THEN ' NOT NULL' ELSE '' END ||
		CASE WHEN t.typdefaultbin IS NOT NULL THEN ' DEFAULT ' || pg_catalog.pg_get_expr(t.typdefaultbin, 0) ELSE '' END || ';' ||
		COALESCE(domain_checks.statements, '')
	WHEN 'r' THEN 'CREATE TYPE ' || pg_catalog.quote_ident(ns.nspname) || '.' || pg_catalog.quote_ident(t.typname) ||
		' AS RANGE (subtype = ' || pg_catalog.format_type(r.rngsubtype, NULL) ||
		CASE WHEN opc.opcname IS NOT NULL THEN ', subtype_opclass = ' || pg_catalog.quote_ident(opc_ns.nspname) || '.' || pg_catalog.quote_ident(opc.opcname) ELSE '' END ||
		CASE WHEN canonical.proname IS NOT NULL THEN ', canonical = ' || pg_catalog.quote_ident(canonical_ns.nspname) || '.' || pg_catalog.quote_ident(canonical.proname) ELSE '' END ||
		CASE WHEN difference.proname IS NOT NULL THEN ', subtype_diff = ' || pg_catalog.quote_ident(difference_ns.nspname) || '.' || pg_catalog.quote_ident(difference.proname) ELSE '' END || ');'
	WHEN 'c' THEN 'CREATE TYPE ' || pg_catalog.quote_ident(ns.nspname) || '.' || pg_catalog.quote_ident(t.typname) ||
		' AS (' || composite_columns.definitions || ');'
	ELSE '' END AS ddl
	FROM pg_catalog.pg_type AS t
	JOIN pg_catalog.pg_namespace AS ns ON ns.oid = t.typnamespace
	LEFT JOIN pg_catalog.pg_range AS r ON r.rngtypid = t.oid
	LEFT JOIN pg_catalog.pg_opclass AS opc ON opc.oid = r.rngsubopc
	LEFT JOIN pg_catalog.pg_namespace AS opc_ns ON opc_ns.oid = opc.opcnamespace
	LEFT JOIN pg_catalog.pg_proc AS canonical ON canonical.oid = r.rngcanonical
	LEFT JOIN pg_catalog.pg_namespace AS canonical_ns ON canonical_ns.oid = canonical.pronamespace
	LEFT JOIN pg_catalog.pg_proc AS difference ON difference.oid = r.rngsubdiff
	LEFT JOIN pg_catalog.pg_namespace AS difference_ns ON difference_ns.oid = difference.pronamespace
	LEFT JOIN LATERAL (
		SELECT pg_catalog.string_agg(pg_catalog.quote_literal(enum.enumlabel), ', ' ORDER BY enum.enumsortorder) AS enum_values
		FROM pg_catalog.pg_enum AS enum WHERE enum.enumtypid = t.oid
	) AS labels ON true
	LEFT JOIN LATERAL (
		SELECT pg_catalog.string_agg(
			E'\nALTER DOMAIN ' || pg_catalog.quote_ident(ns.nspname) || '.' || pg_catalog.quote_ident(t.typname) ||
			' ADD CONSTRAINT ' || pg_catalog.quote_ident(con.conname) || ' ' || pg_catalog.pg_get_constraintdef(con.oid, true) || ';',
			'' ORDER BY con.conname
		) AS statements
		FROM pg_catalog.pg_constraint AS con WHERE con.contypid = t.oid AND con.contype = 'c'
	) AS domain_checks ON true
	LEFT JOIN LATERAL (
		SELECT pg_catalog.string_agg(pg_catalog.quote_ident(attr.attname) || ' ' || pg_catalog.format_type(attr.atttypid, attr.atttypmod), ', ' ORDER BY attr.attnum) AS definitions
		FROM pg_catalog.pg_attribute AS attr
		WHERE attr.attrelid = t.typrelid AND attr.attnum > 0 AND NOT attr.attisdropped
	) AS composite_columns ON true
	WHERE ns.nspname = '` + schema + `' AND t.typname = '` + name + `' AND t.typtype IN ('d', 'e', 'r', 'c')`
	return readSQLDDL(ctx, store, statement, connection.DriverPostgreSQL)
}

func writePostgreSQLForeignKeys(ctx context.Context, store connection.SQL, scope string, output io.Writer) error {
	filter := ""
	if scope != "" {
		filter = " AND ns.nspname = '" + quoteSQLLiteral(scope) + "'"
	}
	statement := `SELECT 'ALTER TABLE ' || pg_catalog.quote_ident(ns.nspname) || '.' || pg_catalog.quote_ident(table_class.relname) ||
		' ADD CONSTRAINT ' || pg_catalog.quote_ident(con.conname) || ' ' || pg_catalog.pg_get_constraintdef(con.oid, true) || ';'
	FROM pg_catalog.pg_constraint AS con
	JOIN pg_catalog.pg_class AS table_class ON table_class.oid = con.conrelid
	JOIN pg_catalog.pg_namespace AS ns ON ns.oid = table_class.relnamespace
	WHERE con.contype = 'f' AND ns.nspname <> 'information_schema' AND left(ns.nspname, 3) <> 'pg_'` + filter +
		` ORDER BY ns.nspname, table_class.relname, con.conname`
	cursor, err := store.StartQuery(ctx, statement)
	if err != nil {
		return err
	}
	defer cursor.Close()
	writer := bufio.NewWriter(output)
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		rows, done, err := cursor.NextBatch(100)
		if err != nil {
			return err
		}
		for _, row := range rows {
			if len(row) > 0 {
				if _, err := fmt.Fprintf(writer, "%s\n", fmt.Sprint(row[0])); err != nil {
					return err
				}
			}
		}
		if err := writer.Flush(); err != nil {
			return err
		}
		if done {
			return nil
		}
	}
}

func writePostgreSQLDeferrableConstraints(ctx context.Context, store connection.SQL, scope string, output io.Writer) error {
	filter := ""
	if scope != "" {
		filter = " AND ns.nspname = '" + quoteSQLLiteral(scope) + "'"
	}
	statement := `SELECT ns.nspname, con.conname
	FROM pg_catalog.pg_constraint AS con
	JOIN pg_catalog.pg_class AS table_class ON table_class.oid = con.conrelid
	JOIN pg_catalog.pg_namespace AS ns ON ns.oid = table_class.relnamespace
	WHERE con.contype = 'f' AND con.condeferrable` + filter + `
	ORDER BY ns.nspname, con.conname, table_class.relname`
	cursor, err := store.StartQuery(ctx, statement)
	if err != nil {
		return err
	}
	defer cursor.Close()
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		rows, done, err := cursor.NextBatch(100)
		if err != nil {
			return err
		}
		for _, row := range rows {
			if len(row) < 2 {
				continue
			}
			name := quoteIdentifierForDriver(connection.DriverPostgreSQL, fmt.Sprint(row[0])) + "." + quoteIdentifierForDriver(connection.DriverPostgreSQL, fmt.Sprint(row[1]))
			if _, err := fmt.Fprintf(output, "SET CONSTRAINTS %s DEFERRED;\n", name); err != nil {
				return err
			}
		}
		if done {
			return nil
		}
	}
}

func writePostgreSQLIndexes(ctx context.Context, store connection.SQL, scope string, output io.Writer) error {
	filter := ""
	if scope != "" {
		filter = " AND ns.nspname = '" + quoteSQLLiteral(scope) + "'"
	}
	statement := `SELECT pg_catalog.pg_get_indexdef(idx.indexrelid)
	FROM pg_catalog.pg_index AS idx
	JOIN pg_catalog.pg_class AS index_class ON index_class.oid = idx.indexrelid
	JOIN pg_catalog.pg_namespace AS ns ON ns.oid = index_class.relnamespace
	WHERE ns.nspname <> 'information_schema' AND ns.nspname NOT LIKE 'pg\_%' ESCAPE '\'
	AND NOT EXISTS (SELECT 1 FROM pg_catalog.pg_constraint AS con WHERE con.conindid = idx.indexrelid)` + filter +
		` ORDER BY ns.nspname, index_class.relname`
	cursor, err := store.StartQuery(ctx, statement)
	if err != nil {
		return err
	}
	defer cursor.Close()
	writer := bufio.NewWriter(output)
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		rows, done, err := cursor.NextBatch(100)
		if err != nil {
			return err
		}
		for _, row := range rows {
			if len(row) > 0 && strings.TrimSpace(fmt.Sprint(row[0])) != "" {
				ddl := strings.TrimSuffix(strings.TrimSpace(fmt.Sprint(row[0])), ";")
				if _, err := fmt.Fprintf(writer, "%s;\n", ddl); err != nil {
					return err
				}
			}
		}
		if err := writer.Flush(); err != nil {
			return err
		}
		if done {
			return nil
		}
	}
}

func writePostgreSQLSequenceOwnership(ctx context.Context, store connection.SQL, sequences []connection.Object, output io.Writer) error {
	writer := bufio.NewWriter(output)
	for _, sequence := range sequences {
		owner, err := readPostgreSQLSequenceOwner(ctx, store, sequence)
		if err != nil {
			return err
		}
		if owner.kind != "a" {
			continue
		}
		name := quoteIdentifierForDriver(connection.DriverPostgreSQL, sequence.Schema) + "." + quoteIdentifierForDriver(connection.DriverPostgreSQL, sequence.Name)
		table := quoteIdentifierForDriver(connection.DriverPostgreSQL, owner.schema) + "." + quoteIdentifierForDriver(connection.DriverPostgreSQL, owner.table) + "." + quoteIdentifierForDriver(connection.DriverPostgreSQL, owner.column)
		if _, err := fmt.Fprintf(writer, "ALTER SEQUENCE %s OWNED BY %s;\n", name, table); err != nil {
			return err
		}
	}
	return writer.Flush()
}

type postgresSequenceOwner struct {
	schema, table, column, kind string
}

func readPostgreSQLSequenceOwner(ctx context.Context, store connection.SQL, sequence connection.Object) (postgresSequenceOwner, error) {
	schema, name := quoteSQLLiteral(sequence.Schema), quoteSQLLiteral(sequence.Name)
	statement := `SELECT table_ns.nspname, table_class.relname, attr.attname, dep.deptype
	FROM pg_catalog.pg_class AS seq
	JOIN pg_catalog.pg_namespace AS seq_ns ON seq_ns.oid = seq.relnamespace
	JOIN pg_catalog.pg_depend AS dep ON dep.classid = 'pg_catalog.pg_class'::regclass AND dep.objid = seq.oid
	JOIN pg_catalog.pg_class AS table_class ON table_class.oid = dep.refobjid
	JOIN pg_catalog.pg_namespace AS table_ns ON table_ns.oid = table_class.relnamespace
	JOIN pg_catalog.pg_attribute AS attr ON attr.attrelid = table_class.oid AND attr.attnum = dep.refobjsubid
	WHERE seq_ns.nspname = '` + schema + `' AND seq.relname = '` + name + `' AND dep.deptype IN ('a', 'i')
	LIMIT 1`
	cursor, err := store.StartQuery(ctx, statement)
	if err != nil {
		return postgresSequenceOwner{}, err
	}
	defer cursor.Close()
	rows, _, err := cursor.NextBatch(1)
	if err != nil {
		return postgresSequenceOwner{}, err
	}
	if len(rows) == 0 || len(rows[0]) < 4 {
		return postgresSequenceOwner{}, nil
	}
	return postgresSequenceOwner{fmt.Sprint(rows[0][0]), fmt.Sprint(rows[0][1]), fmt.Sprint(rows[0][2]), fmt.Sprint(rows[0][3])}, nil
}

func writePostgreSQLSequenceState(ctx context.Context, store connection.SQL, sequence connection.Object, output io.Writer) error {
	name := quoteIdentifierForDriver(connection.DriverPostgreSQL, sequence.Schema) + "." + quoteIdentifierForDriver(connection.DriverPostgreSQL, sequence.Name)
	cursor, err := store.StartQuery(ctx, "SELECT last_value, is_called FROM "+name)
	if err != nil {
		return err
	}
	defer cursor.Close()
	rows, _, err := cursor.NextBatch(1)
	if err != nil {
		return err
	}
	if len(rows) == 0 || len(rows[0]) < 2 {
		return fmt.Errorf("read sequence state for %s returned no value", name)
	}
	lastValue := fmt.Sprint(rows[0][0])
	if _, err := strconv.ParseInt(lastValue, 10, 64); err != nil {
		return fmt.Errorf("invalid sequence value for %s: %w", name, err)
	}
	isCalled := strings.EqualFold(fmt.Sprint(rows[0][1]), "true")
	regclass := quoteSQLLiteral(name)
	_, err = fmt.Fprintf(output, "SELECT pg_catalog.setval('%s'::regclass, %s, %t);\n", regclass, lastValue, isCalled)
	return err
}

func readSQLDDL(ctx context.Context, store connection.SQL, statement string, driver connection.Driver) (string, error) {
	cursor, err := store.StartQuery(ctx, statement)
	if err != nil {
		return "", err
	}
	defer cursor.Close()
	rows, _, err := cursor.NextBatch(1)
	if err != nil {
		return "", err
	}
	if len(rows) == 0 || len(rows[0]) == 0 {
		return "", nil
	}
	index := 0
	if driver == connection.DriverMySQL && len(rows[0]) > 1 {
		index = 1
	}
	return fmt.Sprint(rows[0][index]), nil
}
