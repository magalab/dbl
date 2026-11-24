package app

import (
	"fmt"
	"strings"
	"time"

	"github.com/magalab/dbl/internal/connection"
)

func (app *workbench) openSQLDatabaseCreator(connectionID string, driver connection.Driver) {
	if app.busy || app.exporting || app.dialogOpen || driver != connection.DriverMySQL && driver != connection.DriverPostgreSQL {
		return
	}
	if connectionID != app.openedConnectionID && !app.activateLiveConnectionForWorkspace(connectionID) {
		return
	}
	if app.store == nil || app.openedDriver != driver || app.openedReadOnly {
		return
	}
	app.sqlDatabaseCreateName = ""
	app.sqlDatabaseCreateConnectionID = app.openedConnectionID
	app.sqlDatabaseCreateDriver = driver
	app.sqlDatabaseCreateOpen = true
	app.dialogOpen = true
}

func (app *workbench) closeSQLDatabaseCreator() {
	app.sqlDatabaseCreateOpen = false
	app.dialogOpen = false
	app.sqlDatabaseCreateName = ""
	app.sqlDatabaseCreateConnectionID = ""
	app.sqlDatabaseCreateDriver = ""
}

func (app *workbench) confirmSQLDatabaseCreate() {
	name := strings.TrimSpace(app.sqlDatabaseCreateName)
	connectionID, driver := app.sqlDatabaseCreateConnectionID, app.sqlDatabaseCreateDriver
	if name == "" {
		app.status = app.localized("请输入数据库名称。")
		return
	}
	app.closeSQLDatabaseCreator()
	statement := "CREATE DATABASE " + quoteIdentifierForDriver(driver, name) + ";"
	connectionLabel := app.openedLabel
	app.requestConfirmation(
		"新建数据库",
		fmt.Sprintf(app.localized("将在连接‘%s’中创建数据库‘%s’。"), connectionLabel, name),
		app.localized("此操作会在服务器上新建数据库，不会自动切换当前连接。"),
		[]string{"取消", "创建"}, 1,
		func(choice int) {
			if choice == 1 {
				app.executeSQLDatabaseCreate(connectionID, driver, name, statement)
			}
		},
	)
}

func (app *workbench) executeSQLDatabaseCreate(connectionID string, driver connection.Driver, name, statement string) {
	if app.busy || app.store == nil || app.openedConnectionID != connectionID || app.openedDriver != driver || app.openedReadOnly {
		app.status = app.localized("连接已变化，数据库没有创建。")
		return
	}
	executor, ok := app.store.(connection.SQLExecutor)
	if !ok {
		app.status = app.localized("当前连接不支持创建数据库。")
		return
	}
	store := app.store
	app.busy = true
	app.status = app.localized("正在创建数据库…")
	ctx, cancel := app.withTaskTimeout(30 * time.Second)
	go func() {
		_, err := executor.ExecStatement(ctx, statement)
		cancel()
		app.window.Update(func() {
			app.busy = false
			if app.store != store || app.openedConnectionID != connectionID || app.openedDriver != driver {
				app.status = app.localized("连接已变化，数据库创建结果未关联当前连接。")
				return
			}
			if err != nil {
				app.reportError("创建数据库失败", err)
				return
			}
			app.status = fmt.Sprintf(app.localized("数据库‘%s’已创建。"), name)
			if driver == connection.DriverMySQL {
				app.refreshCurrentConnection()
				app.status = fmt.Sprintf(app.localized("数据库‘%s’已创建。"), name)
			}
		})
	}()
}
