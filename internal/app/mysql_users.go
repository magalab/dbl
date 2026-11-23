package app

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/magalab/dbl/internal/connection"
	"github.com/magalab/dbl/internal/safeerror"
)

type mysqlUserAccount struct {
	Name   string
	Host   string
	Plugin string
	Locked bool
}

func (account mysqlUserAccount) key() string {
	return account.Name + "\x00" + account.Host
}

var mysqlDatabasePrivileges = []string{
	"SELECT", "INSERT", "UPDATE", "DELETE", "CREATE", "DROP", "INDEX", "ALTER",
	"EXECUTE", "CREATE VIEW", "SHOW VIEW", "TRIGGER", "EVENT", "CREATE ROUTINE", "ALTER ROUTINE",
}

const mysqlGlobalGrantScope = "\x00dbl:mysql:global-grant-scope"
const mysqlNoGrantScope = "\x00dbl:mysql:no-grant-scope"

func (app *workbench) openMySQLUserManager(connectionID, namespace string) {
	if app.busy || app.exporting || app.dialogOpen {
		return
	}
	if index := app.userManagerWorkspaceIndex(connectionID, connection.DriverMySQL); index >= 0 {
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
	if app.store == nil || app.openedDriver != connection.DriverMySQL || !app.openSQLQuery("") || app.activeQuery == nil {
		return
	}
	workspace := app.activeQuery
	workspace.title = "用户与权限"
	workspace.mysqlUserManager = true
	workspace.queryConnectionID = connectionID
	workspace.queryNamespace = namespace
	workspace.mysqlUserListWidth = 290
	workspace.mysqlUserEditorWidth = 360
	workspace.mysqlUserGrantDatabase = namespace
	workspace.mysqlUserPrivileges = make([]bool, len(mysqlDatabasePrivileges))
	app.switchQueryWorkspaceNamespace(workspace)
	app.status = app.localized("已打开 MySQL 用户与权限面板。")
}

func (app *workbench) refreshMySQLUsers(workspace *queryWorkspace) {
	if workspace == nil || !workspace.mysqlUserManager || workspace.mysqlUserLoading || app.busy || app.store == nil || app.openedDriver != connection.DriverMySQL || app.openedConnectionID != workspace.queryConnectionID {
		return
	}
	store, connectionID := app.store, app.openedConnectionID
	workspace.mysqlUserLoading = true
	workspace.mysqlUserError = ""
	workspace.mysqlUserSQLModeKnown = false
	go func() {
		ctx, cancel := app.withTaskTimeout(15 * time.Second)
		defer cancel()
		rows, err := readSQLAdminRows(ctx, store, "SELECT User, Host, plugin, account_locked FROM mysql.user ORDER BY User, Host LIMIT 2000", 2000)
		lockSupported := err == nil
		if err != nil && strings.Contains(strings.ToLower(err.Error()), "account_locked") {
			rows, err = readSQLAdminRows(ctx, store, "SELECT User, Host, plugin FROM mysql.user ORDER BY User, Host LIMIT 2000", 2000)
		}
		var mode string
		modeKnown := false
		if err == nil {
			modeRows, modeErr := readSQLAdminRows(ctx, store, "SELECT @@SESSION.sql_mode", 1)
			if modeErr != nil {
				err = modeErr
			} else if len(modeRows) > 0 && len(modeRows[0]) > 0 {
				mode = sqlAdminValueText(modeRows[0][0])
				modeKnown = true
			} else {
				err = fmt.Errorf("could not read the MySQL session SQL mode")
			}
		}
		users := make([]mysqlUserAccount, 0, len(rows))
		if err == nil {
			for _, row := range rows {
				if len(row) < 3 {
					continue
				}
				account := mysqlUserAccount{Name: sqlAdminValueText(row[0]), Host: sqlAdminValueText(row[1]), Plugin: sqlAdminValueText(row[2])}
				if lockSupported && len(row) > 3 {
					account.Locked = sqlAdminValueText(row[3]) == "Y" || sqlAdminValueText(row[3]) == "1" || strings.EqualFold(sqlAdminValueText(row[3]), "true")
				}
				users = append(users, account)
			}
		}
		app.window.Update(func() {
			if !workspace.mysqlUserManager {
				return
			}
			workspace.mysqlUserLoading = false
			if app.store != store || app.openedConnectionID != connectionID {
				return
			}
			workspace.mysqlUserLoaded = true
			workspace.mysqlUserError = ""
			if err != nil {
				workspace.mysqlUserError = safeerror.Display(err)
				workspace.mysqlUsers = nil
				workspace.mysqlUserSelected = ""
				workspace.mysqlUserGrants = nil
				return
			}
			selected := workspace.mysqlUserSelected
			workspace.mysqlUsers = users
			workspace.mysqlUserSQLMode = mode
			workspace.mysqlUserSQLModeKnown = modeKnown
			workspace.mysqlUserLockSupported = lockSupported
			if !containsMySQLUser(users, selected) {
				if len(users) > 0 {
					selected = users[0].key()
				} else {
					selected = ""
				}
			}
			workspace.mysqlUserSelected = selected
			if selected != "" && app.activeQuery == workspace {
				app.loadMySQLUserGrants(workspace)
			}
		})
	}()
}

func containsMySQLUser(users []mysqlUserAccount, key string) bool {
	for _, account := range users {
		if account.key() == key {
			return true
		}
	}
	return false
}

func (app *workbench) loadMySQLUserGrants(workspace *queryWorkspace) {
	if workspace == nil || workspace.mysqlUserGrantsLoading || app.store == nil || app.openedDriver != connection.DriverMySQL || app.openedConnectionID != workspace.queryConnectionID {
		return
	}
	var selected mysqlUserAccount
	for _, account := range workspace.mysqlUsers {
		if account.key() == workspace.mysqlUserSelected {
			selected = account
			break
		}
	}
	if selected.Name == "" && selected.Host == "" {
		return
	}
	statement := "SHOW GRANTS FOR " + mysqlAccountSQL(selected, workspace.mysqlUserSQLMode)
	store, connectionID := app.store, app.openedConnectionID
	workspace.mysqlUserGrantsLoading = true
	workspace.mysqlUserGrantsError = ""
	workspace.mysqlUserGrants = nil
	go func() {
		ctx, cancel := app.withTaskTimeout(15 * time.Second)
		defer cancel()
		rows, err := readSQLAdminRows(ctx, store, statement, 2000)
		grants := make([]string, 0, len(rows))
		for _, row := range rows {
			if len(row) > 0 {
				grants = append(grants, sqlAdminValueText(row[0]))
			}
		}
		app.window.Update(func() {
			if !workspace.mysqlUserManager {
				return
			}
			workspace.mysqlUserGrantsLoading = false
			if app.store != store || app.openedConnectionID != connectionID {
				return
			}
			if workspace.mysqlUserSelected != selected.key() {
				app.loadMySQLUserGrants(workspace)
				return
			}
			if err != nil {
				workspace.mysqlUserGrantsError = safeerror.Display(err)
				return
			}
			workspace.mysqlUserGrants = grants
		})
	}()
}

func readSQLAdminRows(ctx context.Context, store connection.SQL, statement string, maxRows int) ([][]any, error) {
	cursor, err := store.StartQuery(ctx, statement)
	if err != nil {
		return nil, err
	}
	defer cursor.Close()
	rows := make([][]any, 0, 32)
	for {
		batch, done, err := cursor.NextBatch(256)
		if err != nil {
			return nil, err
		}
		rows = append(rows, batch...)
		if len(rows) > maxRows {
			return nil, fmt.Errorf("metadata query exceeded %d rows", maxRows)
		}
		if done {
			return rows, nil
		}
	}
}

func sqlAdminValueText(value any) string {
	switch value := value.(type) {
	case nil:
		return ""
	case string:
		return value
	case []byte:
		return string(value)
	case bool:
		return strconv.FormatBool(value)
	default:
		return fmt.Sprint(value)
	}
}

func mysqlAccountSQL(account mysqlUserAccount, sqlMode string) string {
	return mysqlSQLString(account.Name, sqlMode) + "@" + mysqlSQLString(account.Host, sqlMode)
}

func mysqlSQLString(value, sqlMode string) string {
	if strings.Contains(strings.ToUpper(sqlMode), "NO_BACKSLASH_ESCAPES") {
		return "'" + strings.ReplaceAll(value, "'", "''") + "'"
	}
	replacer := strings.NewReplacer("\\", "\\\\", "'", "\\'", "\x00", "\\0", "\n", "\\n", "\r", "\\r", "\x1a", "\\Z")
	return "'" + replacer.Replace(value) + "'"
}

func mysqlSQLIdentifier(value string) string {
	return "`" + strings.ReplaceAll(value, "`", "``") + "`"
}

func clearMySQLUserWorkspaceSecrets(workspace *queryWorkspace) {
	if workspace == nil {
		return
	}
	workspace.mysqlUserDraftPassword = ""
	workspace.mysqlUserNewPassword = ""
	workspace.mysqlUserCreateOpen = false
	workspace.mysqlUserPasswordOpen = false
}

func (app *workbench) refreshMySQLAuth(workspace *queryWorkspace) {
	if workspace == nil || workspace.mysqlUserLoading || app.busy || app.openedDriver != connection.DriverMySQL || app.openedConnectionID != workspace.queryConnectionID {
		return
	}
	workspace.mysqlUserLoaded = false
	app.refreshMySQLUsers(workspace)
}

func (app *workbench) runMySQLUserMutation(workspace *queryWorkspace, statement, preview, title, success string) {
	app.runMySQLUserMutationAfter(workspace, statement, preview, title, success, nil)
}

func (app *workbench) runMySQLUserMutationAfter(workspace *queryWorkspace, statement, preview, title, success string, onSuccess func()) {
	if workspace == nil || !workspace.mysqlUserSQLModeKnown || app.openedReadOnly || app.store == nil || app.openedDriver != connection.DriverMySQL || app.openedConnectionID != workspace.queryConnectionID || app.busy || app.dialogOpen {
		return
	}
	_, ok := app.store.(connection.SQLExecutor)
	if !ok {
		app.status = app.localized("当前 MySQL 连接不支持用户管理操作。")
		return
	}
	app.requestConfirmation(title, "请核对用户和权限变更。", preview, []string{"取消", "确认"}, 1, func(choice int) {
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
					app.reportError("MySQL 用户操作失败", err)
					return
				}
				app.status = app.localized(success)
				if onSuccess != nil {
					onSuccess()
				}
				app.refreshMySQLAuth(workspace)
			})
		}()
	})
}

func (app *workbench) createMySQLUser(workspace *queryWorkspace) {
	if workspace == nil || !workspace.mysqlUserSQLModeKnown {
		app.status = app.localized("无法确认 MySQL SQL mode，已禁用用户修改操作。")
		return
	}
	name, host, password := strings.TrimSpace(workspace.mysqlUserDraftName), strings.TrimSpace(workspace.mysqlUserDraftHost), workspace.mysqlUserDraftPassword
	if name == "" || host == "" || password == "" || strings.ContainsAny(name+host+password, "\x00\r\n") {
		app.status = app.localized("用户名、Host 和密码必须有效。")
		return
	}
	account := mysqlUserAccount{Name: name, Host: host}
	statement := "CREATE USER " + mysqlAccountSQL(account, workspace.mysqlUserSQLMode) + " IDENTIFIED BY " + mysqlSQLString(password, workspace.mysqlUserSQLMode)
	preview := "CREATE USER " + mysqlAccountSQL(account, workspace.mysqlUserSQLMode) + " IDENTIFIED BY '••••••';"
	workspace.mysqlUserDraftPassword = ""
	workspace.mysqlUserCreateOpen = false
	app.runMySQLUserMutation(workspace, statement, preview, "创建 MySQL 用户", "已创建 MySQL 用户。")
}

func (app *workbench) changeMySQLUserPassword(workspace *queryWorkspace, account mysqlUserAccount) {
	if workspace == nil || !workspace.mysqlUserSQLModeKnown {
		app.status = app.localized("无法确认 MySQL SQL mode，已禁用用户修改操作。")
		return
	}
	password := workspace.mysqlUserNewPassword
	if password == "" || strings.ContainsAny(password, "\x00\r\n") {
		app.status = app.localized("新密码不能为空，且不能包含换行符。")
		return
	}
	statement := "ALTER USER " + mysqlAccountSQL(account, workspace.mysqlUserSQLMode) + " IDENTIFIED BY " + mysqlSQLString(password, workspace.mysqlUserSQLMode)
	preview := "ALTER USER " + mysqlAccountSQL(account, workspace.mysqlUserSQLMode) + " IDENTIFIED BY '••••••';"
	workspace.mysqlUserNewPassword = ""
	workspace.mysqlUserPasswordOpen = false
	app.runMySQLUserMutation(workspace, statement, preview, "修改 MySQL 用户密码", "已修改 MySQL 用户密码。")
}
