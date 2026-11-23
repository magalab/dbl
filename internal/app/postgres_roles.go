package app

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/magalab/dbl/internal/connection"
	"github.com/magalab/dbl/internal/safeerror"
)

type postgresRole struct {
	Name        string
	Superuser   bool
	Inherit     bool
	CreateRole  bool
	CreateDB    bool
	CanLogin    bool
	Replication bool
	BypassRLS   bool
}

func (app *workbench) openPostgresRoleManager(connectionID, namespace string) {
	if app.busy || app.exporting || app.dialogOpen {
		return
	}
	if index := app.userManagerWorkspaceIndex(connectionID, connection.DriverPostgreSQL); index >= 0 {
		app.activateWorkspace(index)
		return
	}
	if app.queryWorkspaceRequested && app.workspaceCount() >= maxWorkspaces {
		return
	}
	if connectionID != app.openedConnectionID {
		if !app.activateLiveConnectionForWorkspace(connectionID) || app.store == nil {
			return
		}
	}
	if app.store == nil || app.openedDriver != connection.DriverPostgreSQL || !app.openSQLQuery("") || app.activeQuery == nil {
		return
	}
	workspace := app.activeQuery
	workspace.title = "用户与权限"
	workspace.postgresRoleManager = true
	workspace.queryConnectionID = connectionID
	workspace.queryNamespace = namespace
	workspace.postgresRoleListWidth = 290
	workspace.postgresRoleEditorWidth = 360
	workspace.postgresRoleGrantTargetType = "database"
	workspace.postgresRoleRevoke = false
	workspace.postgresRoleMemberRevoke = false
	workspace.postgresRolePrivileges = make([]bool, 3)
	app.switchQueryWorkspaceNamespace(workspace)
	app.status = app.localized("已打开 PostgreSQL 用户与权限面板。")
}

func (app *workbench) refreshPostgresRoles(workspace *queryWorkspace) {
	if workspace == nil || !workspace.postgresRoleManager || workspace.postgresRoleLoading || app.busy || app.store == nil || app.openedDriver != connection.DriverPostgreSQL || app.openedConnectionID != workspace.queryConnectionID {
		return
	}
	store, connectionID := app.store, app.openedConnectionID
	workspace.postgresRoleLoading = true
	workspace.postgresRoleError = ""
	const query = `SELECT rolname, rolsuper, rolinherit, rolcreaterole, rolcreatedb, rolcanlogin,
		COALESCE((pg_catalog.to_jsonb(r)->>'rolreplication')::boolean, false),
		COALESCE((pg_catalog.to_jsonb(r)->>'rolbypassrls')::boolean, false)
		FROM pg_catalog.pg_roles AS r ORDER BY rolname LIMIT 3000`
	go func() {
		ctx, cancel := app.withTaskTimeout(15 * time.Second)
		defer cancel()
		rows, err := readSQLAdminRows(ctx, store, query, 3000)
		database := ""
		var tableRows [][]any
		if err == nil {
			database, err = readPostgresDatabaseName(ctx, store)
		}
		if err == nil {
			tableRows, err = readSQLAdminRows(ctx, store, postgresRoleTablesQuery(), 3000)
		}
		roles := make([]postgresRole, 0, len(rows))
		tableTargets := make([]string, 0, len(tableRows))
		tableKinds := make(map[string]string, len(tableRows))
		for _, row := range tableRows {
			if len(row) > 1 {
				target := sqlAdminValueText(row[0])
				tableTargets = append(tableTargets, target)
				tableKinds[target] = sqlAdminValueText(row[1])
			}
		}
		if err == nil {
			for _, row := range rows {
				if len(row) < 8 {
					continue
				}
				roles = append(roles, postgresRole{
					Name: sqlAdminValueText(row[0]), Superuser: postgresRoleBool(row[1]),
					Inherit: postgresRoleBool(row[2]), CreateRole: postgresRoleBool(row[3]),
					CreateDB: postgresRoleBool(row[4]), CanLogin: postgresRoleBool(row[5]),
					Replication: postgresRoleBool(row[6]), BypassRLS: postgresRoleBool(row[7]),
				})
			}
		}
		app.window.Update(func() {
			if !workspace.postgresRoleManager {
				return
			}
			workspace.postgresRoleLoading = false
			if app.store != store || app.openedConnectionID != connectionID {
				return
			}
			workspace.postgresRoleLoaded = true
			if err != nil {
				workspace.postgresRoleError = safeerror.Display(err)
				workspace.postgresRoles = nil
				workspace.postgresRoleSelected = ""
				workspace.postgresRoleDetails = nil
				return
			}
			workspace.postgresRoles = roles
			workspace.postgresRoleDatabase = database
			workspace.postgresRoleTableTargets = tableTargets
			workspace.postgresRoleTableKinds = tableKinds
			previousGrantTable := workspace.postgresRoleGrantTable
			previousGrantTableKind := workspace.postgresRoleGrantTableKind
			if !containsString(tableTargets, workspace.postgresRoleGrantTable) {
				workspace.postgresRoleGrantTable = ""
			}
			if workspace.postgresRoleGrantTable == "" && len(tableTargets) > 0 {
				workspace.postgresRoleGrantTable = tableTargets[0]
			}
			workspace.postgresRoleGrantTableKind = tableKinds[workspace.postgresRoleGrantTable]
			if workspace.postgresRoleGrantTargetType == "table" &&
				(previousGrantTable != workspace.postgresRoleGrantTable || previousGrantTableKind != workspace.postgresRoleGrantTableKind) {
				workspace.postgresRolePrivileges = make([]bool, len(postgresRoleWorkspacePrivileges(workspace)))
			}
			selected := workspace.postgresRoleSelected
			if !containsPostgresRole(roles, selected) {
				selected = ""
				if len(roles) > 0 {
					selected = roles[0].Name
				}
			}
			workspace.postgresRoleSelected = selected
			if role, ok := selectedPostgresRole(workspace); ok {
				workspace.postgresRoleDraft = role
				if workspace.postgresRoleMemberRole == "" || workspace.postgresRoleMemberRole == selected {
					workspace.postgresRoleMemberRole = firstOtherPostgresRole(roles, selected)
				}
				if app.activeQuery == workspace {
					app.loadPostgresRoleDetails(workspace)
				}
			}
		})
	}()
}

func postgresRoleBool(value any) bool {
	switch value := value.(type) {
	case bool:
		return value
	default:
		switch strings.ToLower(strings.TrimSpace(sqlAdminValueText(value))) {
		case "t", "true", "1", "yes", "y":
			return true
		default:
			return false
		}
	}
}

func containsPostgresRole(roles []postgresRole, name string) bool {
	for _, role := range roles {
		if role.Name == name {
			return true
		}
	}
	return false
}

func selectedPostgresRole(workspace *queryWorkspace) (postgresRole, bool) {
	if workspace == nil {
		return postgresRole{}, false
	}
	for _, role := range workspace.postgresRoles {
		if role.Name == workspace.postgresRoleSelected {
			return role, true
		}
	}
	return postgresRole{}, false
}

func firstOtherPostgresRole(roles []postgresRole, selected string) string {
	for _, role := range roles {
		if role.Name != selected {
			return role.Name
		}
	}
	return ""
}

func (app *workbench) loadPostgresRoleDetails(workspace *queryWorkspace) {
	if workspace == nil || workspace.postgresRoleDetailsLoading || app.store == nil || app.openedDriver != connection.DriverPostgreSQL || app.openedConnectionID != workspace.queryConnectionID || workspace.postgresRoleSelected == "" {
		return
	}
	selected := workspace.postgresRoleSelected
	store, connectionID := app.store, app.openedConnectionID
	query := postgresRoleDetailsQuery(selected)
	workspace.postgresRoleDetailsLoading = true
	workspace.postgresRoleDetailsError = ""
	workspace.postgresRoleDetails = nil
	go func() {
		ctx, cancel := app.withTaskTimeout(15 * time.Second)
		defer cancel()
		rows, err := readSQLAdminRows(ctx, store, query, 2000)
		details := make([]string, 0, len(rows))
		for _, row := range rows {
			if len(row) > 0 {
				details = append(details, sqlAdminValueText(row[0]))
			}
		}
		app.window.Update(func() {
			if !workspace.postgresRoleManager {
				return
			}
			workspace.postgresRoleDetailsLoading = false
			if app.store != store || app.openedConnectionID != connectionID {
				return
			}
			if workspace.postgresRoleSelected != selected {
				app.loadPostgresRoleDetails(workspace)
				return
			}
			if err != nil {
				workspace.postgresRoleDetailsError = safeerror.Display(err)
				return
			}
			workspace.postgresRoleDetails = details
		})
	}()
}

func postgresRoleDetailsQuery(roleName string) string {
	role := postgresSQLLiteral(roleName)
	return `SELECT detail FROM (
		SELECT CASE WHEN member.rolname = ` + role + `
			THEN 'MEMBER OF ' || pg_catalog.quote_ident(granted.rolname) || CASE WHEN am.admin_option THEN ' (ADMIN OPTION)' ELSE '' END
			ELSE 'HAS MEMBER ' || pg_catalog.quote_ident(member.rolname) || CASE WHEN am.admin_option THEN ' (ADMIN OPTION)' ELSE '' END
		END AS detail
		FROM pg_catalog.pg_auth_members AS am
		JOIN pg_catalog.pg_roles AS granted ON granted.oid = am.roleid
		JOIN pg_catalog.pg_roles AS member ON member.oid = am.member
		WHERE member.rolname = ` + role + ` OR granted.rolname = ` + role + `
		UNION ALL
		SELECT 'DATABASE ' || pg_catalog.quote_ident(current_database()) || ': ' || privilege
		FROM (VALUES ('CONNECT'), ('CREATE'), ('TEMPORARY')) AS database_privileges(privilege)
		WHERE pg_catalog.has_database_privilege(` + role + `, current_database(), privilege)
		UNION ALL
		SELECT 'SCHEMA ' || pg_catalog.quote_ident(schema_name) || ': ' || privilege
		FROM information_schema.schemata
		CROSS JOIN (VALUES ('USAGE'), ('CREATE')) AS schema_privileges(privilege)
		WHERE schema_name <> 'information_schema' AND schema_name NOT LIKE 'pg\_%' ESCAPE '\'
			AND pg_catalog.has_schema_privilege(` + role + `, schema_name, privilege)
		UNION ALL
		SELECT 'TABLE ' || pg_catalog.quote_ident(table_schema) || '.' || pg_catalog.quote_ident(table_name) || ': ' || privilege_type ||
			CASE WHEN is_grantable = 'YES' THEN ' (GRANT OPTION)' ELSE '' END
		FROM information_schema.role_table_grants
		WHERE grantee = ` + role + `
	) AS role_details ORDER BY detail LIMIT 2000`
}

func postgresRoleTablesQuery() string {
	return `SELECT pg_catalog.quote_ident(table_schema) || '.' || pg_catalog.quote_ident(table_name), table_type
		FROM information_schema.tables
		WHERE table_type IN ('BASE TABLE', 'VIEW', 'FOREIGN')
			AND table_schema <> 'information_schema' AND table_schema NOT LIKE 'pg\_%' ESCAPE '\'
		ORDER BY table_schema, table_name LIMIT 3000`
}

func postgresSQLIdentifier(value string) string {
	return `"` + strings.ReplaceAll(value, `"`, `""`) + `"`
}

func postgresSQLLiteral(value string) string {
	replacer := strings.NewReplacer("\\", "\\\\", "'", "''", "\x00", "")
	return "E'" + replacer.Replace(value) + "'"
}

func clearPostgresRoleWorkspaceSecrets(workspace *queryWorkspace) {
	if workspace == nil {
		return
	}
	workspace.postgresRoleDraftPassword = ""
	workspace.postgresRoleNewPassword = ""
	workspace.postgresRoleCreateOpen = false
	workspace.postgresRolePasswordOpen = false
}

func clearPostgresRoleSecret(value *string) {
	if value != nil {
		*value = ""
	}
}

func (app *workbench) refreshPostgresRoleManager(workspace *queryWorkspace) {
	if workspace == nil || workspace.postgresRoleLoading || app.busy || app.openedDriver != connection.DriverPostgreSQL || app.openedConnectionID != workspace.queryConnectionID {
		return
	}
	workspace.postgresRoleLoaded = false
	app.refreshPostgresRoles(workspace)
}

func (app *workbench) runPostgresRoleMutation(workspace *queryWorkspace, statement, preview, title, success string, onSuccess func()) {
	if workspace == nil || app.openedReadOnly || app.store == nil || app.openedDriver != connection.DriverPostgreSQL || app.openedConnectionID != workspace.queryConnectionID || app.busy || app.dialogOpen {
		return
	}
	if _, ok := app.store.(connection.SQLExecutor); !ok {
		app.status = app.localized("当前 PostgreSQL 连接不支持角色管理操作。")
		return
	}
	app.requestConfirmation(title, "请核对 PostgreSQL 角色和权限变更。", preview, []string{"取消", "确认"}, 1, func(choice int) {
		if choice != 1 || app.store == nil || app.openedConnectionID != workspace.queryConnectionID {
			return
		}
		store := app.store
		executor, ok := store.(connection.SQLExecutor)
		if !ok {
			return
		}
		app.busy = true
		ctx, cancel := app.withTaskTimeout(30 * time.Second)
		go func() {
			defer cancel()
			_, err := executor.ExecStatement(ctx, statement)
			app.window.Update(func() {
				app.busy = false
				if app.store != store || app.openedConnectionID != workspace.queryConnectionID {
					return
				}
				if err != nil {
					app.reportError("PostgreSQL 角色操作失败", err)
					return
				}
				app.status = app.localized(success)
				if onSuccess != nil {
					onSuccess()
				}
				app.refreshPostgresRoleManager(workspace)
			})
		}()
	})
}

func (app *workbench) createPostgresRole(workspace *queryWorkspace) {
	name := strings.TrimSpace(workspace.postgresRoleDraftName)
	password := workspace.postgresRoleDraftPassword
	if name == "" || strings.ContainsAny(name, "\x00\r\n") || len([]byte(name)) > 63 || strings.ContainsRune(password, '\x00') {
		app.status = app.localized("角色名无效或超过 63 个字节。")
		return
	}
	statement := "CREATE ROLE " + postgresSQLIdentifier(name)
	options := []string{"INHERIT", "NOLOGIN", "NOSUPERUSER", "NOCREATEDB", "NOCREATEROLE"}
	if workspace.postgresRoleDraftLogin {
		options[1] = "LOGIN"
	}
	if workspace.postgresRoleDraftSuperuser {
		options[2] = "SUPERUSER"
	}
	if workspace.postgresRoleDraftCreateDB {
		options[3] = "CREATEDB"
	}
	if workspace.postgresRoleDraftCreateRole {
		options[4] = "CREATEROLE"
	}
	statement += " WITH " + strings.Join(options, " ")
	preview := statement
	if workspace.postgresRoleDraftLogin && password != "" {
		statement += " PASSWORD " + postgresSQLLiteral(password)
		preview += " PASSWORD '••••••'"
	}
	statement += ";"
	preview += ";"
	workspace.postgresRoleDraftPassword = ""
	workspace.postgresRoleCreateOpen = false
	app.runPostgresRoleMutation(workspace, statement, preview, "创建 PostgreSQL 角色", "已创建 PostgreSQL 角色。", nil)
}

func postgresRoleAttributesChanged(role, draft postgresRole) bool {
	return role.Superuser != draft.Superuser || role.Inherit != draft.Inherit || role.CreateRole != draft.CreateRole || role.CreateDB != draft.CreateDB || role.CanLogin != draft.CanLogin || role.Replication != draft.Replication || role.BypassRLS != draft.BypassRLS
}

func (app *workbench) updatePostgresRoleAttributes(workspace *queryWorkspace) {
	role, ok := selectedPostgresRole(workspace)
	if !ok || !postgresRoleAttributesChanged(role, workspace.postgresRoleDraft) {
		return
	}
	var options []string
	for _, change := range []struct {
		before, after bool
		positive      string
		negative      string
	}{
		{role.Superuser, workspace.postgresRoleDraft.Superuser, "SUPERUSER", "NOSUPERUSER"},
		{role.CreateDB, workspace.postgresRoleDraft.CreateDB, "CREATEDB", "NOCREATEDB"},
		{role.CreateRole, workspace.postgresRoleDraft.CreateRole, "CREATEROLE", "NOCREATEROLE"},
		{role.CanLogin, workspace.postgresRoleDraft.CanLogin, "LOGIN", "NOLOGIN"},
		{role.Inherit, workspace.postgresRoleDraft.Inherit, "INHERIT", "NOINHERIT"},
		{role.Replication, workspace.postgresRoleDraft.Replication, "REPLICATION", "NOREPLICATION"},
		{role.BypassRLS, workspace.postgresRoleDraft.BypassRLS, "BYPASSRLS", "NOBYPASSRLS"},
	} {
		if change.before != change.after {
			if change.after {
				options = append(options, change.positive)
			} else {
				options = append(options, change.negative)
			}
		}
	}
	statement := "ALTER ROLE " + postgresSQLIdentifier(role.Name) + " WITH " + strings.Join(options, " ") + ";"
	app.runPostgresRoleMutation(workspace, statement, statement, "更新 PostgreSQL 角色属性", "已更新 PostgreSQL 角色属性。", nil)
}

func (app *workbench) changePostgresRolePassword(workspace *queryWorkspace) {
	role, ok := selectedPostgresRole(workspace)
	if !ok {
		return
	}
	password := workspace.postgresRoleNewPassword
	if password == "" || strings.ContainsRune(password, '\x00') {
		app.status = app.localized("新密码不能为空，且不能包含 NUL 字符。")
		return
	}
	statement := "ALTER ROLE " + postgresSQLIdentifier(role.Name) + " PASSWORD " + postgresSQLLiteral(password) + ";"
	preview := "ALTER ROLE " + postgresSQLIdentifier(role.Name) + " PASSWORD '••••••';"
	workspace.postgresRoleNewPassword = ""
	workspace.postgresRolePasswordOpen = false
	app.runPostgresRoleMutation(workspace, statement, preview, "修改 PostgreSQL 角色密码", "已修改 PostgreSQL 角色密码。", nil)
}

func (app *workbench) togglePostgresRoleLogin(workspace *queryWorkspace, role postgresRole) {
	verb, success := "NOLOGIN", "已禁用 PostgreSQL 角色登录。"
	if !role.CanLogin {
		verb, success = "LOGIN", "已启用 PostgreSQL 角色登录。"
	}
	statement := "ALTER ROLE " + postgresSQLIdentifier(role.Name) + " " + verb + ";"
	app.runPostgresRoleMutation(workspace, statement, statement, "更改 PostgreSQL 角色登录状态", success, nil)
}

func (app *workbench) dropPostgresRole(workspace *queryWorkspace, role postgresRole) {
	statement := "DROP ROLE " + postgresSQLIdentifier(role.Name) + ";"
	app.runPostgresRoleMutation(workspace, statement, statement, "删除 PostgreSQL 角色", "已删除 PostgreSQL 角色。", nil)
}

func postgresRoleGrantPreview(workspace *queryWorkspace, database string, schemaNames []string) string {
	role, ok := selectedPostgresRole(workspace)
	if !ok {
		return ""
	}
	var target string
	switch workspace.postgresRoleGrantTargetType {
	case "database":
		target = "DATABASE " + postgresSQLIdentifier(database)
	case "schema":
		schema := workspace.postgresRoleGrantSchema
		found := false
		for _, name := range schemaNames {
			if name == schema {
				found = true
				break
			}
		}
		if !found {
			return ""
		}
		target = "SCHEMA " + postgresSQLIdentifier(schema)
	case "table":
		table := workspace.postgresRoleGrantTable
		if !containsString(workspace.postgresRoleTableTargets, table) {
			return ""
		}
		target = "TABLE " + table
	default:
		return ""
	}
	privileges := postgresRoleWorkspacePrivileges(workspace)
	var selected []string
	for index, privilege := range privileges {
		if index < len(workspace.postgresRolePrivileges) && workspace.postgresRolePrivileges[index] {
			selected = append(selected, privilege)
		}
	}
	if len(selected) == 0 {
		return ""
	}
	verb, receiver := "GRANT", "TO"
	if workspace.postgresRoleRevoke {
		verb, receiver = "REVOKE", "FROM"
	}
	statement := verb + " " + strings.Join(selected, ", ") + " ON " + target + " " + receiver + " " + postgresSQLIdentifier(role.Name)
	if verb == "GRANT" && workspace.postgresRoleGrantOption {
		statement += " WITH GRANT OPTION"
	}
	return statement + ";"
}

func postgresRoleWorkspacePrivileges(workspace *queryWorkspace) []string {
	if workspace == nil {
		return nil
	}
	if workspace.postgresRoleGrantTargetType == "table" && workspace.postgresRoleGrantTableKind == "VIEW" {
		return []string{"SELECT", "INSERT", "UPDATE", "DELETE"}
	}
	return postgresRolePrivilegesForTarget(workspace.postgresRoleGrantTargetType)
}

func postgresRolePrivilegesForTarget(targetType string) []string {
	switch targetType {
	case "database":
		return []string{"CONNECT", "CREATE", "TEMPORARY"}
	case "schema":
		return []string{"USAGE", "CREATE"}
	case "table":
		return []string{"SELECT", "INSERT", "UPDATE", "DELETE", "TRUNCATE", "REFERENCES", "TRIGGER"}
	default:
		return nil
	}
}

func postgresRoleMembershipPreview(workspace *queryWorkspace) string {
	role, ok := selectedPostgresRole(workspace)
	groupRole := workspace.postgresRoleMemberRole
	if !ok || groupRole == "" || groupRole == role.Name || !containsPostgresRole(workspace.postgresRoles, groupRole) {
		return ""
	}
	if workspace.postgresRoleMemberRevoke {
		return "REVOKE " + postgresSQLIdentifier(groupRole) + " FROM " + postgresSQLIdentifier(role.Name) + ";"
	}
	statement := "GRANT " + postgresSQLIdentifier(groupRole) + " TO " + postgresSQLIdentifier(role.Name)
	if workspace.postgresRoleMemberAdmin {
		statement += " WITH ADMIN OPTION"
	}
	return statement + ";"
}

func readPostgresDatabaseName(ctx context.Context, store connection.SQL) (string, error) {
	rows, err := readSQLAdminRows(ctx, store, "SELECT current_database()", 1)
	if err != nil {
		return "", err
	}
	if len(rows) == 0 || len(rows[0]) == 0 {
		return "", fmt.Errorf("could not read the current PostgreSQL database")
	}
	return sqlAdminValueText(rows[0][0]), nil
}
