package app

import (
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode"

	"github.com/magalab/dbl/internal/connection"

	"github.com/magalab/dbl/internal/sqlstream"
)

func schemaWorkspaceDirty(workspace *queryWorkspace) bool {
	if workspace == nil || !workspace.schemaView {
		return false
	}
	if workspace.schemaCreateTable {
		return true
	}
	for _, column := range workspace.schemaColumns {
		if column.IsNew {
			if !column.Dropped {
				return true
			}
			continue
		}
		if column.Dropped || column.Name != column.OriginalName {
			return true
		}
		for _, original := range workspace.schemaBaseline {
			if original.Name != column.OriginalName {
				continue
			}
			defaultValue := ""
			defaultSet := original.Default != nil
			if defaultSet {
				defaultValue = fmt.Sprint(original.Default)
			}
			if column.Type != original.Type || column.NotNull != original.NotNull || column.DefaultSet != defaultSet || column.Default != defaultValue {
				return true
			}
			break
		}
	}
	return false
}

func (app *workbench) hasStagedSchemaForConnections(connectionIDs map[string]bool) bool {
	for _, workspace := range app.workspaces {
		if workspace.schemaView && connectionIDs[workspace.schemaConnectionID] && schemaWorkspaceDirty(workspace) {
			return true
		}
	}
	return false
}

func (app *workbench) reloadSchemaWorkspace(workspace *queryWorkspace) {
	if workspace == nil || workspace.schemaConnectionID != app.openedConnectionID || app.busy || app.dialogOpen || app.exporting {
		return
	}
	workspace.schemaBaseline = nil
	workspace.schemaColumns = nil
	workspace.schemaLoaded = false
	workspace.schemaLoadError = ""
	workspace.schemaError = ""
	app.loadSchemaWorkspaceColumns(workspace)
}

func (app *workbench) confirmReloadSchemaWorkspace(workspace *queryWorkspace) {
	if app.dialogOpen || workspace == nil || !schemaWorkspaceDirty(workspace) {
		return
	}
	app.requestConfirmation(
		app.localized("丢弃结构草稿"),
		app.localized("刷新会丢弃尚未应用的字段修改。"),
		app.localized("数据库中的结构不受影响；会重新读取服务端字段状态。"),
		[]string{"取消", "丢弃并刷新"}, 1,
		func(choice int) {
			if choice == 1 {
				app.reloadSchemaWorkspace(workspace)
			}
		},
	)
}

func (app *workbench) schemaChangesSQL(workspace *queryWorkspace) (string, error) {
	if workspace == nil || workspace.schemaConnectionID != app.openedConnectionID {
		return "", fmt.Errorf("%s", app.localized("切换回结构编辑器所属的连接后再生成 SQL。 "))
	}
	if app.openedDriver != connection.DriverPostgreSQL && app.openedDriver != connection.DriverMySQL && app.openedDriver != connection.DriverSQLite {
		return "", fmt.Errorf("%s", app.localized("当前驱动暂不支持图形化结构修改。 "))
	}
	if workspace.schemaCreateTable {
		return app.newTableSchemaSQL(workspace)
	}
	table := app.quoteIdentifier(workspace.schemaObject.Name)
	if workspace.schemaObject.Schema != "" && app.openedDriver != connection.DriverSQLite {
		table = app.quoteIdentifier(workspace.schemaObject.Schema) + "." + table
	}
	statements := make([]string, 0)
	seen := make(map[string]bool)
	for _, column := range workspace.schemaColumns {
		if column.IsNew && column.Dropped {
			continue
		}
		if column.Dropped {
			statements = append(statements, "ALTER TABLE "+table+" DROP COLUMN "+app.quoteIdentifier(column.OriginalName)+";")
			continue
		}
		if strings.TrimSpace(column.Name) == "" {
			return "", fmt.Errorf("%s", app.localized("字段名不能为空。 "))
		}
		key := schemaNameKey(app.openedDriver, column.Name)
		if seen[key] {
			return "", fmt.Errorf("%s", app.localized("字段名不能重复。 "))
		}
		seen[key] = true
		if column.IsNew {
			if !validSchemaType(column.Type) {
				return "", fmt.Errorf("%s", app.localized("数据类型只能包含类型名、空格、括号、逗号和类型修饰符。 "))
			}
			if column.DefaultSet && !validSchemaSQLFragment(column.Default) {
				return "", fmt.Errorf("%s", app.localized("默认表达式不能为空，也不能包含分号或 SQL 注释。 "))
			}
			definition := app.schemaColumnDefinition(column, nil)
			statements = append(statements, "ALTER TABLE "+table+" ADD COLUMN "+app.quoteIdentifier(column.Name)+" "+definition+";")
			continue
		}
		original, ok := schemaOriginalColumn(workspace.schemaBaseline, column.OriginalName)
		if !ok {
			return "", fmt.Errorf("%s", app.localized("字段元数据已变化，请刷新结构后重试。 "))
		}
		defaultChanged := column.DefaultSet != (original.Default != nil) || (column.DefaultSet && column.Default != fmt.Sprint(original.Default))
		if column.Type != original.Type && !validSchemaType(column.Type) {
			return "", fmt.Errorf("%s", app.localized("数据类型只能包含类型名、空格、括号、逗号和类型修饰符。 "))
		}
		if defaultChanged && column.DefaultSet && !validSchemaSQLFragment(column.Default) {
			return "", fmt.Errorf("%s", app.localized("默认表达式不能为空，也不能包含分号或 SQL 注释。 "))
		}
		if column.Hidden {
			originalDefault := ""
			if original.Default != nil {
				originalDefault = fmt.Sprint(original.Default)
			}
			if column.Name != column.OriginalName || column.Type != original.Type || column.NotNull != original.NotNull || column.DefaultSet != (original.Default != nil) || column.Default != originalDefault {
				return "", fmt.Errorf("%s", app.localized("生成列或自增列不能通过此编辑器修改。 "))
			}
			continue
		}
		if column.Name != column.OriginalName {
			statements = append(statements, "ALTER TABLE "+table+" RENAME COLUMN "+app.quoteIdentifier(column.OriginalName)+" TO "+app.quoteIdentifier(column.Name)+";")
		}
		columnRef := app.quoteIdentifier(column.Name)
		switch app.openedDriver {
		case connection.DriverPostgreSQL:
			if column.Type != original.Type {
				statements = append(statements, "ALTER TABLE "+table+" ALTER COLUMN "+columnRef+" TYPE "+strings.TrimSpace(column.Type)+";")
			}
			if column.NotNull != original.NotNull {
				verb := "DROP NOT NULL"
				if column.NotNull {
					verb = "SET NOT NULL"
				}
				statements = append(statements, "ALTER TABLE "+table+" ALTER COLUMN "+columnRef+" "+verb+";")
			}
			if column.DefaultSet != (original.Default != nil) || (column.DefaultSet && column.Default != fmt.Sprint(original.Default)) {
				verb := "DROP DEFAULT"
				if column.DefaultSet {
					verb = "SET DEFAULT " + strings.TrimSpace(column.Default)
				}
				statements = append(statements, "ALTER TABLE "+table+" ALTER COLUMN "+columnRef+" "+verb+";")
			}
		case connection.DriverMySQL:
			if column.Type != original.Type || column.NotNull != original.NotNull {
				definition := app.schemaColumnDefinition(column, &original)
				statements = append(statements, "ALTER TABLE "+table+" MODIFY COLUMN "+columnRef+" "+definition+";")
			}
			if column.DefaultSet != (original.Default != nil) || (column.DefaultSet && column.Default != fmt.Sprint(original.Default)) {
				verb := "DROP DEFAULT"
				if column.DefaultSet {
					verb = "SET DEFAULT " + strings.TrimSpace(column.Default)
				}
				statements = append(statements, "ALTER TABLE "+table+" ALTER COLUMN "+columnRef+" "+verb+";")
			}
		case connection.DriverSQLite:
			if column.Type != original.Type || column.NotNull != original.NotNull || column.DefaultSet != (original.Default != nil) || (column.DefaultSet && column.Default != fmt.Sprint(original.Default)) {
				return "", fmt.Errorf("%s", app.localized("SQLite 的现有字段类型、非空和默认值需要重建整张表；当前编辑器仅支持重命名、删除和新增字段。 "))
			}
		}
	}
	originalOwners := make(map[string]schemaColumnDraft)
	for _, column := range workspace.schemaColumns {
		if !column.IsNew {
			originalOwners[schemaNameKey(app.openedDriver, column.OriginalName)] = column
		}
	}
	for _, column := range workspace.schemaColumns {
		if column.Dropped {
			continue
		}
		if other, exists := originalOwners[schemaNameKey(app.openedDriver, column.Name)]; exists && other.OriginalName != column.OriginalName {
			return "", fmt.Errorf("%s", app.localized("目标字段名仍被另一字段占用；请分步重命名后再应用。 "))
		}
	}
	return strings.Join(statements, "\n"), nil
}

func (app *workbench) newTableSchemaSQL(workspace *queryWorkspace) (string, error) {
	name := strings.TrimSpace(workspace.schemaObject.Name)
	if name == "" {
		return "", fmt.Errorf("%s", app.localized("表名不能为空。"))
	}
	table := app.quoteIdentifier(name)
	if workspace.schemaObject.Schema != "" && app.openedDriver != connection.DriverSQLite {
		table = app.quoteIdentifier(workspace.schemaObject.Schema) + "." + table
	}
	definitions := make([]string, 0, len(workspace.schemaColumns))
	primaryColumns := make([]string, 0, len(workspace.schemaColumns))
	seen := make(map[string]bool, len(workspace.schemaColumns))
	autoIncrementCount := 0
	for _, column := range workspace.schemaColumns {
		if column.Dropped {
			continue
		}
		if strings.TrimSpace(column.Name) == "" {
			return "", fmt.Errorf("%s", app.localized("字段名不能为空。 "))
		}
		key := schemaNameKey(app.openedDriver, column.Name)
		if seen[key] {
			return "", fmt.Errorf("%s", app.localized("字段名不能重复。 "))
		}
		seen[key] = true
		if !validSchemaType(column.Type) {
			return "", fmt.Errorf("%s", app.localized("数据类型只能包含类型名、空格、括号、逗号和类型修饰符。 "))
		}
		if column.DefaultSet && !validSchemaSQLFragment(column.Default) {
			return "", fmt.Errorf("%s", app.localized("默认表达式不能为空，也不能包含分号或 SQL 注释。 "))
		}
		if column.PrimaryKey {
			primaryColumns = append(primaryColumns, app.quoteIdentifier(column.Name))
		}
		if column.AutoIncrement {
			autoIncrementCount++
		}
		if column.AutoIncrement && (!column.PrimaryKey || !autoIncrementTypeSupported(app.openedDriver, column.Type) || column.DefaultSet) {
			return "", fmt.Errorf("%s", app.localized("自增字段需要使用受支持的整数类型、主键，且不能设置默认值。"))
		}
		definition := app.quoteIdentifier(column.Name) + " "
		if column.AutoIncrement && app.openedDriver == connection.DriverSQLite {
			definition += "INTEGER PRIMARY KEY AUTOINCREMENT"
		} else {
			definition += strings.TrimSpace(column.Type)
			if column.AutoIncrement {
				switch app.openedDriver {
				case connection.DriverMySQL:
					definition += " NOT NULL AUTO_INCREMENT"
				case connection.DriverPostgreSQL:
					definition += " GENERATED BY DEFAULT AS IDENTITY"
				}
			} else {
				if column.NotNull {
					definition += " NOT NULL"
				}
				if column.DefaultSet {
					definition += " DEFAULT " + strings.TrimSpace(column.Default)
				}
			}
		}
		definitions = append(definitions, definition)
	}
	if len(definitions) == 0 {
		return "", fmt.Errorf("%s", app.localized("新表至少需要一个字段。"))
	}
	if autoIncrementCount > 1 {
		return "", fmt.Errorf("%s", app.localized("此数据库的每张表最多只能有一个自增字段。"))
	}
	if autoIncrementCount > 0 && app.openedDriver == connection.DriverSQLite && len(primaryColumns) > 1 {
		return "", fmt.Errorf("%s", app.localized("SQLite 自增字段不能与复合主键同时使用。"))
	}
	if len(primaryColumns) > 0 {
		inlineSQLitePrimaryKey := false
		if app.openedDriver == connection.DriverSQLite && len(primaryColumns) == 1 {
			for _, column := range workspace.schemaColumns {
				if column.PrimaryKey && column.AutoIncrement {
					inlineSQLitePrimaryKey = true
					break
				}
			}
		}
		if !inlineSQLitePrimaryKey {
			definitions = append(definitions, "PRIMARY KEY ("+strings.Join(primaryColumns, ", ")+")")
		}
	}
	return "CREATE TABLE " + table + " (\n    " + strings.Join(definitions, ",\n    ") + "\n);\n", nil
}

func autoIncrementTypeSupported(driver connection.Driver, typeName string) bool {
	typeName = strings.ToUpper(strings.TrimSpace(typeName))
	if fields := strings.Fields(typeName); len(fields) > 0 {
		typeName = fields[0]
	}
	if open := strings.IndexByte(typeName, '('); open >= 0 {
		typeName = typeName[:open]
	}
	switch driver {
	case connection.DriverSQLite:
		return typeName == "INTEGER"
	case connection.DriverMySQL:
		return slices.Contains([]string{"TINYINT", "SMALLINT", "MEDIUMINT", "INT", "INTEGER", "BIGINT"}, typeName)
	case connection.DriverPostgreSQL:
		return slices.Contains([]string{"SMALLINT", "INT2", "INTEGER", "INT", "INT4", "BIGINT", "INT8"}, typeName)
	}
	return false
}

func schemaNameKey(driver connection.Driver, name string) string {
	if driver == connection.DriverPostgreSQL {
		return name
	}
	return strings.ToLower(name)
}

func (app *workbench) schemaColumnDefinition(column schemaColumnDraft, original *connection.Column) string {
	definition := strings.TrimSpace(column.Type)
	if column.NotNull {
		definition += " NOT NULL"
	}
	if column.DefaultSet {
		definition += " DEFAULT " + strings.TrimSpace(column.Default)
	}
	if original != nil && app.openedDriver == connection.DriverMySQL {
		if original.Collation != "" {
			definition += " COLLATE " + app.quoteIdentifier(original.Collation)
		}
		if original.Comment != "" {
			definition += " COMMENT '" + strings.ReplaceAll(original.Comment, "'", "''") + "'"
		}
		if extra := mysqlColumnOnUpdate(original.Extra); extra != "" {
			definition += " " + extra
		}
	}
	return definition
}

func mysqlColumnOnUpdate(extra string) string {
	lower := strings.ToLower(extra)
	if index := strings.Index(lower, "on update "); index >= 0 {
		return strings.TrimSpace(extra[index:])
	}
	return ""
}

func schemaOriginalColumn(columns []connection.Column, name string) (connection.Column, bool) {
	for _, column := range columns {
		if column.Name == name {
			return column, true
		}
	}
	return connection.Column{}, false
}

func validSchemaSQLFragment(value string) bool {
	trimmed := strings.TrimSpace(value)
	return trimmed != "" && !strings.ContainsAny(trimmed, ";\r\n") && !strings.Contains(trimmed, "--") && !strings.Contains(trimmed, "/*") && !strings.Contains(trimmed, "*/")
}

func validSchemaType(value string) bool {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" || strings.Contains(trimmed, "--") || strings.Contains(trimmed, "/*") || strings.Contains(trimmed, "*/") {
		return false
	}
	const allowed = "_ .,()[]\"'`+-\\"
	for _, character := range trimmed {
		if unicode.IsLetter(character) || unicode.IsDigit(character) || strings.ContainsRune(allowed, character) {
			continue
		}
		return false
	}
	return true
}

func (app *workbench) applySchemaWorkspaceChanges(workspace *queryWorkspace) {
	if workspace == nil || app.activeQuery != workspace || app.busy || app.exporting || app.dialogOpen || app.queryHistorySaving || app.openedReadOnly || workspace.schemaConnectionID != app.openedConnectionID {
		return
	}
	app.cancelResultCountForWorkspace(workspace)
	statement, err := app.schemaChangesSQL(workspace)
	if err != nil {
		workspace.schemaCloseAfterApply = false
		workspace.schemaError = err.Error()
		app.status = err.Error()
		return
	}
	if statement == "" {
		workspace.schemaCloseAfterApply = false
		workspace.schemaError = ""
		app.status = app.localized("当前没有结构变更。 ")
		return
	}
	workspace.schemaError = ""
	workspace.query.Buffer.Set(statement)
	workspace.schemaApplyPending = true
	app.executeSQL()
}

func (app *workbench) executeSchemaDDL(statement string) {
	workspace := app.activeQuery
	if workspace == nil || !workspace.schemaView || !workspace.schemaApplyPending || app.store == nil || workspace.schemaConnectionID != app.openedConnectionID {
		if workspace != nil {
			app.completeSchemaWorkspaceApply(workspace, false)
		}
		app.status = app.localized("无法应用结构变更：连接已变化或不再可用。 ")
		return
	}
	executor, ok := app.store.(connection.SchemaEditor)
	if !ok {
		app.completeSchemaWorkspaceApply(workspace, false)
		app.status = app.localized("当前驱动没有结构编辑能力。 ")
		return
	}
	statements := make([]string, 0)
	for _, part := range strings.Split(statement, ";") {
		part = strings.TrimSpace(part)
		if part != "" {
			statements = append(statements, part)
		}
	}
	if len(statements) == 0 {
		app.completeSchemaWorkspaceApply(workspace, false)
		app.status = app.localized("当前没有结构变更。 ")
		return
	}
	ctx, cancel := app.withTaskTimeout(queryTimeout)
	connectionID := app.openedConnectionID
	started := time.Now()
	app.nextQueryHistoryID++
	historyID := app.nextQueryHistoryID
	workspace.activeHistoryID = historyID
	workspace.history = append(workspace.history, queryRecord{ID: historyID, SQL: statement, Started: started, Outcome: "执行中"})
	if len(workspace.history) > maxHistoryEntries {
		workspace.history = workspace.history[1:]
	}
	workspace.resultSQL = statement
	workspace.resultConnectionID = connectionID
	workspace.columns = nil
	workspace.resultWindow = sqlstream.Window{MaxRows: maxDisplayedRows, MaxBytes: maxRetainedResultBytes}
	workspace.resultTotal = 0
	workspace.resultTotalKnown = false
	workspace.resultTotalCounted = false
	workspace.resultCountCancelled = false
	workspace.streamStopped = false
	app.cursor, app.cancelQuery, app.queryContext = nil, cancel, ctx
	app.busy = true
	app.status = app.localized("正在应用表结构变更…")
	app.persistQueryHistory()
	go func() {
		err := executor.ApplySchemaChanges(ctx, statements)
		cancel()
		app.window.Update(func() {
			app.busy = false
			app.cancelQuery = nil
			app.queryContext = nil
			if app.openedConnectionID != connectionID || app.activeQuery != workspace {
				app.completeSchemaWorkspaceApply(workspace, false)
				return
			}
			if err != nil {
				app.reportError("应用表结构失败", err)
				app.updateHistory(historyID, started, app.status)
				app.completeSchemaWorkspaceApply(workspace, false)
				return
			}
			app.status = app.localized("表结构变更已应用。")
			app.errorSummary, app.errorDetails, app.showErrorDetails = "", "", false
			app.updateHistory(historyID, started, app.status)
			app.completeSchemaWorkspaceApply(workspace, true)
		})
	}()
}

func (app *workbench) completeSchemaWorkspaceApply(workspace *queryWorkspace, success bool) {
	if workspace == nil || !workspace.schemaApplyPending {
		return
	}
	workspace.schemaApplyPending = false
	if !success {
		workspace.schemaCloseAfterApply = false
		return
	}
	closeAfterApply := workspace.schemaCloseAfterApply
	workspace.schemaCloseAfterApply = false
	if closeAfterApply {
		for index, candidate := range app.workspaces {
			if candidate == workspace {
				app.removeWorkspace(index)
				break
			}
		}
		app.refreshSQLObjects()
		return
	}
	if workspace.schemaCreateTable {
		workspace.schemaCreateTable = false
		workspace.schemaObject.Type = "table"
		workspace.title = "结构 · " + workspace.schemaObject.Name
	}
	workspace.schemaBaseline = nil
	workspace.schemaColumns = nil
	workspace.schemaLoaded = false
	app.loadSchemaWorkspaceColumns(workspace)
	app.refreshSQLObjects()
}

func (app *workbench) confirmCloseSchemaWorkspace(index int, workspace *queryWorkspace) {
	if app.dialogOpen || workspace == nil {
		return
	}
	app.requestConfirmation(
		app.localized("结构变更尚未应用"),
		app.localized("此结构编辑器有未应用的修改。"),
		app.localized("可以继续编辑、丢弃草稿，或先应用结构变更再关闭。"),
		[]string{"继续编辑", "丢弃并关闭", "应用并关闭"}, 2,
		func(choice int) {
			if choice == 1 {
				app.removeWorkspace(index)
				return
			}
			if choice == 2 {
				workspace.schemaCloseAfterApply = true
				app.applySchemaWorkspaceChanges(workspace)
			}
		},
	)
}

func (app *workbench) schemaConnectionLabel(workspace *queryWorkspace) string {
	if workspace == nil {
		return app.openedLabel
	}
	if workspace.schemaConnectionID == app.openedConnectionID {
		return app.openedLabel
	}
	if live, ok := app.liveConnections[workspace.schemaConnectionID]; ok {
		if live.profile.Name != "" {
			return live.profile.Name
		}
		return live.profile.Path
	}
	return app.localized("连接已关闭")
}
