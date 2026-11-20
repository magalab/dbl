package app

import (
	"fmt"
	"net"
	"net/url"
	"runtime"
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"
	mysqldriver "github.com/go-sql-driver/mysql"

	"github.com/magalab/dbl/internal/preferences"
	"github.com/magalab/dbl/internal/sqliteuri"
)

func (app *workbench) connectionContextMenu(menu *ui.Menu, profile preferences.ConnectionProfile, connected bool) {
	if connected {
		if menu.Item("⏏  " + app.localized("关闭连接")).Disabled(app.busy || app.exporting || app.dialogOpen).Chosen() {
			app.closeProfileConnection(profile)
		}
	} else if menu.Item("⏵  " + app.localized("打开连接")).Disabled(app.busy || app.exporting || app.dialogOpen).Chosen() {
		app.openSavedProfile(profile)
	}
	menu.Separator()
	if menu.Item("＋  " + app.localized("新建查询")).Disabled(!app.canOpenProfileQuery(profile, connected)).Chosen() {
		app.openProfileQuery(profile, "")
	}
	menu.Separator()
	if menu.Item("↻  " + app.localized("刷新")).Disabled(!connected || app.busy || app.exporting || app.dialogOpen).Chosen() {
		app.refreshProfileConnection(profile)
	}
	if menu.Item("ⓘ  " + app.localized("实例信息")).Disabled(app.dialogOpen).Chosen() {
		app.showInstanceInfo(profile)
	}
	menu.Separator()
	menu.Submenu("☷  "+app.localized("连接管理"), func(submenu *ui.Menu) {
		if submenu.Item("✎  " + app.localized("编辑连接")).Disabled(app.busy || app.exporting || app.dialogOpen).Chosen() {
			app.loadConnectionProfile(profile)
			app.showConnectionEditor = true
		}
		if submenu.Item("⎘  " + app.localized("复制连接")).Disabled(app.busy || app.exporting || app.dialogOpen).Chosen() {
			app.loadConnectionProfile(profile)
			app.selectedProfileID = ""
			app.profileName = profile.Name + " 副本"
			app.showConnectionEditor = true
		}
		if submenu.Item("⌫  " + app.localized("删除连接…")).Disabled(app.busy || app.exporting || app.dialogOpen || app.preferences == nil).Chosen() {
			app.confirmDeleteConnectionProfile(profile)
		}
	})
}

func (app *workbench) openProfileQuery(profile preferences.ConnectionProfile, query string) {
	if app.profileIsSelected(profile) || app.activateLiveConnection(app.profileLiveSessionID(profile)) {
		app.openSQLQuery(query)
	}
}

func (app *workbench) refreshProfileConnection(profile preferences.ConnectionProfile) {
	if app.profileIsSelected(profile) {
		app.refreshCurrentConnection()
		return
	}
	if profile.Driver == preferences.DriverRedis {
		app.openSavedProfile(profile)
		app.refreshRedisInstance()
	} else if !app.activateLiveConnection(app.profileLiveSessionID(profile)) {
		app.openSavedProfile(profile)
	}
}

func (app *workbench) canOpenProfileQuery(profile preferences.ConnectionProfile, connected bool) bool {
	switch profile.Driver {
	case preferences.DriverSQLite, preferences.DriverPostgreSQL, preferences.DriverMySQL:
		return connected && !app.busy && !app.exporting && !app.dialogOpen
	default:
		return false
	}
}

func (app *workbench) showInstanceInfo(profile preferences.ConnectionProfile) {
	fields := []overviewField{
		{"连接名称", profile.Name, "tag"},
	}
	endpoint := profileEndpoint(profile)
	if profile.Driver == preferences.DriverSQLite {
		fields = append(fields, overviewField{"SQLite 文件路径", profile.Path, "folder"})
	} else if endpoint != "" {
		fields = append(fields, overviewField{"主机 / Endpoint", endpoint, "plug"})
	}
	if profile.Driver != preferences.DriverRedis && profile.Database != "" {
		fields = append(fields, overviewField{"数据库", profile.Database, "database"})
	}
	if dsn := instanceDSN(profile, endpoint); dsn != "" {
		fields = append(fields, overviewField{"DSN", dsn, "terminal"})
	}
	if app.dialogOpen {
		return
	}
	app.requestConfirmation("实例信息", "DSN 默认隐藏密码，复制 DSN 时会包含密码。连接时请在客户端补充认证与自定义证书设置。", "", []string{"关闭"}, 0, nil)
	app.confirmation.fields = fields
	app.confirmation.driver = profile.Driver
	app.confirmation.passwordRef = profile.CredentialRef
}

func profileEndpoint(profile preferences.ConnectionProfile) string {
	if profile.Host != "" && profile.Port > 0 && profile.Driver != preferences.DriverEtcd {
		return net.JoinHostPort(strings.Trim(profile.Host, "[]"), strconv.Itoa(profile.Port))
	}
	return profile.Host
}

func instanceDSN(profile preferences.ConnectionProfile, endpoint string) string {
	mode := profile.TLSMode
	if mode == "" {
		mode = "verify-full"
	}
	switch profile.Driver {
	case preferences.DriverSQLite:
		access := "rw"
		if profile.ReadOnly {
			access = "ro"
		}
		dsn, _ := sqliteuri.DataSourceName(profile.Path, access, runtime.GOOS == "windows")
		return dsn
	case preferences.DriverEtcd:
		// etcd accepts an endpoint list rather than a driver-specific DSN.
		return endpoint
	case preferences.DriverMySQL:
		config := mysqldriver.NewConfig()
		config.User, config.Net, config.Addr, config.DBName = profile.Username, "tcp", endpoint, profile.Database
		config.TLSConfig = strconv.FormatBool(mode != "disable")
		return config.FormatDSN()
	}
	dsn := url.URL{Scheme: profile.Driver, Host: endpoint}
	query := url.Values{}
	switch profile.Driver {
	case preferences.DriverPostgreSQL:
		query.Set("sslmode", mode)
	case preferences.DriverMongoDB:
		query.Set("tls", strconv.FormatBool(mode != "disable"))
		if profile.AuthSource != "" {
			query.Set("authSource", profile.AuthSource)
		}
	case preferences.DriverRedis:
		if mode != "disable" {
			dsn.Scheme = "rediss"
		}
		profile.Database = strconv.Itoa(profile.RedisDB)
	default:
		return ""
	}
	if profile.Username != "" {
		dsn.User = url.User(profile.Username)
	}
	if profile.Database != "" {
		dsn.Path = "/" + profile.Database
		dsn.RawPath = "/" + url.PathEscape(profile.Database)
	}
	dsn.RawQuery = query.Encode()
	return dsn.String()
}

func dsnSupportsPassword(driver string) bool {
	switch driver {
	case preferences.DriverMySQL, preferences.DriverPostgreSQL, preferences.DriverMongoDB, preferences.DriverRedis:
		return true
	default:
		return false
	}
}

func dsnWithPassword(driver, dsn string, password []byte) string {
	switch driver {
	case preferences.DriverMySQL:
		config, err := mysqldriver.ParseDSN(dsn)
		if err != nil {
			return dsn
		}
		config.Passwd = string(password)
		return config.FormatDSN()
	case preferences.DriverPostgreSQL, preferences.DriverMongoDB, preferences.DriverRedis:
		parsed, err := url.Parse(dsn)
		if err != nil {
			return dsn
		}
		username := ""
		if parsed.User != nil {
			username = parsed.User.Username()
		}
		parsed.User = url.UserPassword(username, string(password))
		formatted := parsed.String()
		if string(password) == "********" {
			formatted = strings.Replace(formatted, parsed.User.String(), url.User(username).String()+":********", 1)
		}
		return formatted
	default:
		return dsn
	}
}

func (app *workbench) confirmDeleteConnectionProfile(profile preferences.ConnectionProfile) {
	if app.busy || app.exporting || app.dialogOpen || app.preferences == nil {
		return
	}
	app.requestConfirmation(
		app.localized("删除连接"),
		fmt.Sprintf(app.localized("确定删除连接‘%s’吗？"), profile.Name),
		app.localized("此操作会从本机移除已保存的连接配置。"),
		[]string{"取消", "删除"}, 1,
		func(choice int) {
			if choice == 1 {
				app.deleteConnectionProfile(profile.ID)
			}
		},
	)
}

func (app *workbench) databaseContextMenu(menu *ui.Menu, database string) {
	if menu.Item("＋  " + app.localized("新建查询")).Disabled(app.busy || app.exporting || app.dialogOpen || app.queryWorkspaceRequested && app.workspaceCount() >= maxWorkspaces).Chosen() {
		app.openSQLQuery("")
	}
	menu.Separator()
	if menu.Item("↻  " + app.localized("刷新")).Disabled(app.busy || app.exporting || app.dialogOpen).Chosen() {
		app.refreshCurrentConnection()
	}
	if menu.Item("ⓘ  " + app.localized("数据库信息")).Chosen() {
		app.status = fmt.Sprintf("%s · %s", app.openedLabel, database)
	}
}
