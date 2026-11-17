package app

import (
	"fmt"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/magalab/dbl/internal/databaseicons"
	"github.com/magalab/dbl/internal/preferences"
	"github.com/magalab/dbl/internal/sqlite"
	"github.com/magalab/dbl/internal/sqliteuri"
)

func (app *workbench) chooseSQLiteFile() {
	if app.busy || app.exporting || app.dialogOpen {
		return
	}
	app.dialogOpen = true
	app.status = app.localized("请选择要打开的 SQLite 文件…")
	defaultPath := app.path
	go func() {
		paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
			Title:       "打开 SQLite 文件",
			DefaultPath: defaultPath,
		})
		app.window.Update(func() {
			app.dialogOpen = false
			if err != nil {
				app.reportError("文件选择失败", err)
				return
			}
			if len(paths) == 0 {
				app.status = app.localized("已取消选择文件。")
				return
			}
			app.path = paths[0]
			access := sqlite.ReadWriteExisting
			if app.readOnlyMode {
				access = sqlite.ReadOnly
			}
			app.openSQLite(access)
		})
	}()
}

func (app *workbench) chooseSQLiteProfileFile() {
	if app.busy || app.exporting || app.dialogOpen {
		return
	}
	app.dialogOpen = true
	app.status = app.localized("请选择连接配置使用的 SQLite 文件…")
	defaultPath := app.path
	go func() {
		paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
			Title:       "选择 SQLite 连接文件",
			DefaultPath: defaultPath,
			Filters:     []mygo.FileFilter{{Name: "SQLite", Extensions: []string{"db", "sqlite", "sqlite3"}}},
		})
		app.window.Update(func() {
			app.dialogOpen = false
			if err != nil {
				app.reportError("选择 SQLite 连接文件失败", err)
				return
			}
			if len(paths) == 0 {
				app.status = app.localized("已取消选择 SQLite 文件。")
				return
			}
			app.path = paths[0]
			app.status = app.localized("已选择 SQLite 连接文件；保存配置后可测试或打开。")
		})
	}()
}

func (app *workbench) chooseNewSQLiteProfileFile() {
	if app.busy || app.exporting || app.dialogOpen || app.profileDriver != preferences.DriverSQLite {
		return
	}
	app.dialogOpen = true
	app.status = app.localized("请选择新 SQLite 文件的位置…")
	defaultPath := sqliteuri.SuggestNewFilePath(app.path)
	go func() {
		path, err := mygo.Dialog.Save(mygo.SaveDialogOptions{
			Title:       "新建 SQLite 文件",
			DefaultPath: defaultPath,
			Filters:     []mygo.FileFilter{{Name: "SQLite", Extensions: []string{"db", "sqlite", "sqlite3"}}},
		})
		if err != nil {
			app.window.Update(func() {
				app.dialogOpen = false
				app.reportError("新建 SQLite 文件失败", err)
			})
			return
		}
		if path == "" {
			app.window.Update(func() {
				app.dialogOpen = false
				app.status = app.localized("已取消新建文件。")
			})
			return
		}
		ctx, cancel := app.withTaskTimeout(10 * time.Second)
		store, err := sqlite.Open(ctx, path, sqlite.CreateNew)
		if err == nil {
			err = store.Close()
		}
		cancel()
		app.window.Update(func() {
			app.dialogOpen = false
			if err != nil {
				app.reportError("新建 SQLite 文件失败", err)
				return
			}
			app.path = path
			app.status = app.localized("SQLite 文件已创建。")
		})
	}()
}

func (app *workbench) chooseEtcdTLSFile(clientCertificate bool) {
	if app.busy || app.exporting || app.dialogOpen || app.profileDriver != preferences.DriverEtcd {
		return
	}
	app.dialogOpen = true
	label := "客户端证书"
	if !clientCertificate {
		label = "客户端私钥"
	}
	app.status = app.localized("请选择 etcd ") + label + " PEM 文件…"
	go func() {
		paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{Title: "选择 etcd " + label, Multiple: false, Filters: []mygo.FileFilter{{Name: "PEM", Extensions: []string{"pem", "crt", "key"}}}})
		app.window.Update(func() {
			app.dialogOpen = false
			if err != nil {
				app.reportError("选择 etcd TLS 文件失败", err)
				return
			}
			if len(paths) == 0 {
				app.status = app.localized("已取消选择文件。")
				return
			}
			if clientCertificate {
				app.profileTLSCertPath = paths[0]
			} else {
				app.profileTLSKeyPath = paths[0]
			}
			app.status = app.localized("TLS 文件将在保存配置时加密写入本机凭据库。")
		})
	}()
}

func (app *workbench) etcdTLSFileLabel(path string, profiles []preferences.ConnectionProfile, id string, cert bool) string {
	if path != "" {
		if cert {
			return "更换客户端证书…"
		}
		return "更换客户端私钥…"
	}
	for _, profile := range profiles {
		if profile.ID == id {
			if cert && profile.TLSCertRef != "" {
				return "本机凭据库已有客户端证书"
			}
			if !cert && profile.TLSKeyRef != "" {
				return "本机凭据库已有客户端私钥"
			}
		}
	}
	if cert {
		return "选择客户端证书…"
	}
	return "选择客户端私钥…"
}

func (app *workbench) chooseNewSQLiteFile() {
	if app.readOnlyMode {
		app.status = app.localized("请关闭只读模式后再新建 SQLite 文件。")
		return
	}
	if app.busy || app.exporting || app.dialogOpen {
		return
	}
	app.dialogOpen = true
	app.status = app.localized("请选择新 SQLite 文件的位置…")
	defaultPath := sqliteuri.SuggestNewFilePath(app.path)
	go func() {
		path, err := mygo.Dialog.Save(mygo.SaveDialogOptions{
			Title:       "新建 SQLite 文件",
			DefaultPath: defaultPath,
		})
		app.window.Update(func() {
			app.dialogOpen = false
			if err != nil {
				app.reportError("选择新文件位置失败", err)
				return
			}
			if path == "" {
				app.status = app.localized("已取消新建文件。")
				return
			}
			app.path = path
			app.openSQLite(sqlite.CreateNew)
		})
	}()
}

func (app *workbench) connections(c *ui.Context) {
	ui.Column(c).Fill().Padding(6).Gap(4).Children(func() {
		ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
			search := ui.TextInput(c, &app.connectionSearch).Height(30).Padding(2, 8).Placeholder(app.localized("⌕ 搜索连接或文件…")).Label(app.localized("搜索连接或文件")).Grow(1)
			search.Disabled(app.busy || app.exporting || app.dialogOpen)
			activeOnly := app.localized("仅显示活跃连接")
			activeFilter := ui.Toggle(c, &app.activeConnectionsOnly, "").Label(activeOnly).Tooltip(activeOnly)
			if app.activeConnectionsOnly {
				activeFilter.Background(c.Theme().Accent).TextColor(c.Theme().AccentText)
			}
			activeFilter.Children(func() { ui.Icon(c, actionIcons["filter"]).Size(16, 16) })
			activeFilter.Size(30, 30).Padding(0)
		})
		ui.Scroll(c).Grow(1).Children(func() {
			ui.Tree(c, func() { app.connectionTree(c) }).Fill()
		})
		if app.objectsHasMore && app.store != nil {
			more := ui.Button(c, app.localized("加载更多对象")).Height(32)
			more.Disabled(app.busy || app.exporting || app.dialogOpen)
			if more.Clicked() {
				app.loadMoreObjects()
			}
		}
	})
}

func (app *workbench) openSavedProfile(profile preferences.ConnectionProfile) {
	if app.busy || app.exporting || app.dialogOpen {
		return
	}
	id := app.profileLiveSessionID(profile)
	if app.activateLiveConnectionForWorkspace(id) {
		app.activateConnectionWorkspace(id)
		return
	}
	app.loadConnectionProfile(profile)
	switch profile.Driver {
	case preferences.DriverSQLite:
		access := sqlite.ReadWriteExisting
		if profile.ReadOnly {
			access = sqlite.ReadOnly
		}
		app.openSQLite(access)
	case preferences.DriverPostgreSQL:
		app.openPostgreSQL(profile)
	case preferences.DriverMySQL:
		app.openMySQL(profile)
	case preferences.DriverRedis:
		app.openRedis(profile)
	case preferences.DriverMongoDB:
		app.openMongo(profile)
	case preferences.DriverEtcd:
		app.openEtcd(profile)
	default:
		app.status = app.localized("该连接驱动尚不可用。")
	}
}

func (app *workbench) connectionProfileTextField(c *ui.Context, label, placeholder string, value *string, password bool, grow float32) {
	field := ui.Column(c).MinWidth(0).Gap(4)
	if grow > 0 {
		field.Grow(grow)
	}
	field.Children(func() {
		ui.Text(c, app.localized(label)).TextColor(c.Theme().TextMuted)
		if password {
			ui.Row(c).FillWidth().Gap(0).AlignItems(ui.Center).Children(func() {
				input := ui.TextInput(c, value).Grow(1).Placeholder(app.localized(placeholder)).Label(app.localized(label))
				if !app.profileSecretVisible {
					input.Password()
				}
				input.Disabled(app.busy || app.exporting || app.dialogOpen)
				label := "显示密码"
				icon := actionIcons["eye"]
				if app.profileSecretVisible {
					label = "隐藏密码"
					icon = actionIcons["eyeOff"]
				}
				toggle := iconButton(c, app.localized(label), icon).Size(26, 26).Padding(3).Background(ui.Color{}).BorderWidth(0).TextColor(c.Theme().TextMuted)
				toggle.Disabled(app.busy || app.exporting || app.dialogOpen)
				if toggle.Clicked() {
					app.profileSecretVisible = !app.profileSecretVisible
				}
			})
			return
		}
		input := ui.TextInput(c, value).FillWidth().Placeholder(app.localized(placeholder)).Label(app.localized(label))
		input.Disabled(app.busy || app.exporting || app.dialogOpen)
	})
}

func (app *workbench) connectionProfileSelectField(c *ui.Context, label string, value *string, options []string, grow float32) {
	field := ui.Column(c).MinWidth(0).Gap(4)
	if grow > 0 {
		field.Grow(grow)
	}
	field.Children(func() {
		ui.Text(c, app.localized(label)).TextColor(c.Theme().TextMuted)
		selectField := ui.Select(c, value, options).FillWidth().Label(app.localized(label))
		selectField.Disabled(app.busy || app.exporting || app.dialogOpen)
	})
}

func (app *workbench) changeConnectionProfileDriver(driver string) {
	if driver == app.profileDriver {
		return
	}
	app.profileDriver = driver
	app.resetConnectionProfileTest()
	app.selectedProfileID = ""
	app.profileHost, app.profilePort, app.profileDatabase, app.profileRedisDB = "", "", "", ""
	app.profileRedisAliases = nil
	app.profileUsername, app.profileAuthSource = "", ""
	app.profileTLSCAPath, app.profileTLSCertPath, app.profileTLSKeyPath = "", "", ""
	app.profileSecretVisible = false
	if driver == preferences.DriverSQLite {
		app.profileTLSMode = ""
	} else {
		app.profileTLSMode = tlsModeLabel("disable")
	}
	app.profileCredential, app.profileSecret = "", ""
	app.removeCredential = false
}

func (app *workbench) connectionProfileForm(c *ui.Context) {
	disabled := app.busy || app.exporting || app.dialogOpen
	drivers := []string{preferences.DriverSQLite, preferences.DriverPostgreSQL, preferences.DriverMySQL, preferences.DriverRedis, preferences.DriverMongoDB, preferences.DriverEtcd}
	selectedDriver := 0
	for i, driver := range drivers {
		if driver == app.profileDriver {
			selectedDriver = i
			break
		}
	}
	driverPicker := ui.SegmentedBase(c.Key("connection-profile-driver"), &selectedDriver, len(drivers))
	driverPicker.Track.Gap(4).AlignItems(ui.Center).Children(func() {
		for i, driver := range drivers {
			driver := driver
			label := welcomeDriverLabel(driver)
			option := driverPicker.Segment(i).Size(40, 40).Padding(6).Radius(c.Theme().Radius).Label(label).Tooltip(label)
			option.Disabled(disabled)
			if i == selectedDriver {
				option.Background(c.Theme().Accent)
			}
			option.Children(func() {
				if icon, color, ok := databaseicons.Icon(driver); ok {
					if i == selectedDriver {
						color = c.Theme().AccentText
					}
					ui.Icon(c, icon).Size(24, 24).TextColor(color)
				}
			})
		}
	}).Padding(2).Radius(c.Theme().Radius+2).Background(c.Theme().Surface).Border(1, c.Theme().Border).AlignSelf(ui.Start).Label(app.localized("数据库类型"))
	if driverPicker.Track.Changed() && selectedDriver >= 0 && selectedDriver < len(drivers) {
		app.changeConnectionProfileDriver(drivers[selectedDriver])
	}
	ui.Row(c).FillWidth().Gap(12).Children(func() {
		app.connectionProfileTextField(c, "连接名称", "例如：本地开发库", &app.profileName, false, 1)
		app.connectionProfileTextField(c, "分组", "可选", &app.profileGroup, false, 1)
	})
	if strings.TrimSpace(app.profileName) == "" {
		ui.Text(c, app.localized("请填写连接名称。")).TextColor(c.Theme().Danger)
	}

	if app.profileDriver == preferences.DriverSQLite {
		ui.Column(c).Gap(4).Children(func() {
			ui.Text(c, app.localized("SQLite 文件")).TextColor(c.Theme().TextMuted)
			ui.Row(c).FillWidth().Gap(8).Children(func() {
				path := ui.TextInput(c, &app.path).Grow(1).Placeholder(app.localized("SQLite 数据库文件路径")).Label(app.localized("SQLite 文件路径"))
				path.Disabled(disabled)
				create := iconButton(c, app.localized("新建 SQLite 文件"), actionIcons["filePlus"]).Size(32, 32).Padding(4).Radius(8)
				create.Disabled(disabled)
				if create.Clicked() {
					app.chooseNewSQLiteProfileFile()
				}
				browse := iconButton(c, app.localized("浏览 SQLite 文件…"), actionIcons["folder"]).Size(32, 32).Padding(4).Radius(8)
				browse.Disabled(disabled)
				if browse.Clicked() {
					app.chooseSQLiteProfileFile()
				}
			})
		})
		if strings.TrimSpace(app.path) == "" {
			ui.Text(c, app.localized("请选择 SQLite 文件。")).TextColor(c.Theme().Danger)
		}
	} else {
		hostPlaceholder := "localhost"
		if app.profileDriver == preferences.DriverEtcd {
			hostPlaceholder = app.localized("http://127.0.0.1:2379 或多个 endpoint（逗号分隔）")
		}
		portPlaceholder := "5432"
		switch app.profileDriver {
		case preferences.DriverMySQL:
			portPlaceholder = "3306"
		case preferences.DriverRedis:
			portPlaceholder = "6379"
		case preferences.DriverMongoDB:
			portPlaceholder = "27017"
		case preferences.DriverEtcd:
			portPlaceholder = "2379"
		}
		ui.Row(c).FillWidth().Gap(12).Children(func() {
			app.connectionProfileTextField(c, "主机", hostPlaceholder, &app.profileHost, false, 3)
			app.connectionProfileTextField(c, "端口", portPlaceholder, &app.profilePort, false, 1)
		})
		if strings.TrimSpace(app.profileHost) == "" {
			ui.Text(c, app.localized("请填写主机或 Endpoint。")).TextColor(c.Theme().Danger)
		}

		if app.profileDriver == preferences.DriverMongoDB {
			ui.Row(c).FillWidth().Gap(12).Children(func() {
				app.connectionProfileTextField(c, "数据库", "数据库名称", &app.profileDatabase, false, 1)
				app.connectionProfileTextField(c, "认证数据库", "admin（可选）", &app.profileAuthSource, false, 1)
			})
		} else if app.profileDriver == preferences.DriverRedis {
			app.connectionProfileTextField(c, "Redis DB 索引（非负整数）", "0", &app.profileRedisDB, false, 0)
		} else if app.profileDriver != preferences.DriverEtcd {
			app.connectionProfileTextField(c, "数据库", "数据库名称", &app.profileDatabase, false, 0)
		}

		ui.Row(c).FillWidth().Gap(12).Children(func() {
			app.connectionProfileTextField(c, "用户名", "用户名", &app.profileUsername, false, 1)
			app.connectionProfileTextField(c, "密码（留空则保留已保存密码）", "不会写入配置文件", &app.profileSecret, true, 1)
		})
		if app.profileCredential != "" {
			ui.Text(c, app.localized("本机加密凭据库中已有密码")).TextColor(c.Theme().TextMuted)
			remove := ui.Checkbox(c, &app.removeCredential, app.localized("保存时移除已保存密码"))
			remove.Disabled(disabled)
		}

		tlsOptions := []string{tlsModeLabel("verify-full"), tlsModeLabel("verify-ca"), tlsModeLabel("require"), tlsModeLabel("disable")}
		if app.profileDriver == preferences.DriverPostgreSQL || app.profileDriver == preferences.DriverMySQL {
			tlsOptions = []string{tlsModeLabel("verify-full"), tlsModeLabel("verify-ca"), tlsModeLabel("require"), tlsModeLabel("prefer"), tlsModeLabel("disable")}
		} else if app.profileDriver == preferences.DriverEtcd {
			tlsOptions = []string{tlsModeLabel("verify-full"), tlsModeLabel("disable")}
		}
		app.connectionProfileSelectField(c, "TLS 模式", &app.profileTLSMode, tlsOptions, 0)

		if app.profileDriver == preferences.DriverPostgreSQL || app.profileDriver == preferences.DriverMySQL || app.profileDriver == preferences.DriverMongoDB {
			app.connectionProfileTextField(c, "自定义 CA（可选，PEM 文件路径；追加到系统根证书）", "留空使用系统根证书", &app.profileTLSCAPath, false, 0)
			if app.profileTLSCAPath != "" && profileTLSMode(app.profileTLSMode) != "verify-full" && profileTLSMode(app.profileTLSMode) != "verify-ca" {
				ui.Text(c, app.localized("自定义 CA 只适用于 verify-full/verify-ca；请清空 CA 路径或切换 TLS 模式。 ")).TextColor(c.Theme().Warning)
			}
		}

		if app.profileDriver == preferences.DriverEtcd {
			app.connectionProfileTextField(c, "自定义 CA（可选，PEM 文件路径）", "留空使用系统根证书", &app.profileTLSCAPath, false, 0)
			ui.Row(c).FillWidth().Gap(6).Children(func() {
				cert := ui.Button(c, app.localized(app.etcdTLSFileLabel(app.profileTLSCertPath, app.connectionProfiles, app.selectedProfileID, true)))
				cert.Disabled(disabled)
				if cert.Clicked() {
					app.chooseEtcdTLSFile(true)
				}
				key := ui.Button(c, app.localized(app.etcdTLSFileLabel(app.profileTLSKeyPath, app.connectionProfiles, app.selectedProfileID, false)))
				key.Disabled(disabled)
				if key.Clicked() {
					app.chooseEtcdTLSFile(false)
				}
			})
		}

		if app.profileDriver == preferences.DriverMongoDB {
			switch profileTLSMode(app.profileTLSMode) {
			case "verify-full":
				ui.Text(c, app.localized("MongoDB TLS 使用系统根证书和可选自定义 CA 验证证书和主机名。")).TextColor(c.Theme().TextMuted)
			case "verify-ca":
				ui.Text(c, app.localized("MongoDB TLS 使用系统根证书和可选自定义 CA 验证证书链，不验证主机名。")).TextColor(c.Theme().TextMuted)
			case "require":
				ui.Text(c, app.localized("MongoDB TLS 加密但不验证服务器证书。")).TextColor(c.Theme().TextMuted)
			case "disable":
				ui.Text(c, app.localized("MongoDB TLS 已关闭。")).TextColor(c.Theme().TextMuted)
			}
		} else if app.profileDriver == preferences.DriverRedis {
			switch profileTLSMode(app.profileTLSMode) {
			case "verify-full":
				ui.Text(c, app.localized("Redis TLS 使用系统根证书验证证书和主机名。")).TextColor(c.Theme().TextMuted)
			case "verify-ca":
				ui.Text(c, app.localized("Redis TLS 验证证书链，不验证主机名。")).TextColor(c.Theme().TextMuted)
			case "require":
				ui.Text(c, app.localized("Redis TLS 加密但不验证服务器证书。")).TextColor(c.Theme().TextMuted)
			case "disable":
				ui.Text(c, app.localized("Redis TLS 已关闭。")).TextColor(c.Theme().TextMuted)
			}
		} else if app.profileDriver == preferences.DriverPostgreSQL || app.profileDriver == preferences.DriverMySQL {
			driverName := "MySQL"
			if app.profileDriver == preferences.DriverPostgreSQL {
				driverName = "PostgreSQL"
			}
			switch profileTLSMode(app.profileTLSMode) {
			case "verify-full":
				ui.Text(c, fmt.Sprintf(app.localized("%s 使用系统根证书和可选自定义 CA 验证证书链及主机名；不支持客户端证书。"), driverName)).TextColor(c.Theme().TextMuted)
			case "verify-ca":
				ui.Text(c, fmt.Sprintf(app.localized("%s 使用系统根证书和可选自定义 CA 验证证书链，但不验证主机名；不支持客户端证书。"), driverName)).TextColor(c.Theme().TextMuted)
			case "require":
				ui.Text(c, app.localized("加密连接，但不验证服务器证书。仅在信任网络环境中使用。")).TextColor(c.Theme().TextMuted)
			case "prefer":
				ui.Text(c, app.localized("TLS 不可用时会回退到明文连接。")).TextColor(c.Theme().TextMuted)
			case "disable":
				ui.Text(c, app.localized("TLS 已关闭，连接流量不会加密。")).TextColor(c.Theme().TextMuted)
			}
		} else if app.profileDriver == preferences.DriverEtcd {
			ui.Text(c, app.localized("etcd 使用用户名/密码认证；endpoint scheme 决定 HTTP 或 HTTPS。HTTPS 使用系统根证书和可选自定义 CA，也可配置 vault 中的 mTLS 客户端证书/私钥。")).TextColor(c.Theme().TextMuted)
		}
	}

	if app.profileEnvironment == "" {
		app.profileEnvironment = preferences.EnvironmentDevelopment
	}
	ui.Text(c, app.localized("环境")).TextColor(c.Theme().TextMuted)
	ui.Row(c).FillWidth().Gap(12).AlignItems(ui.Center).Children(func() {
		environment := ui.Select(c, &app.profileEnvironment, []string{preferences.EnvironmentDevelopment, preferences.EnvironmentStaging, preferences.EnvironmentProduction}).Grow(1).Label(app.localized("连接环境"))
		environment.Disabled(disabled)
		readOnly := ui.Checkbox(c, &app.profileReadOnly, app.localized("以只读模式打开"))
		readOnly.Disabled(disabled)
		favorite := ui.Checkbox(c, &app.profileFavorite, app.localized("收藏"))
		favorite.Disabled(disabled)
	})
	if app.profileEnvironment == preferences.EnvironmentProduction {
		ui.Text(c, app.localized("生产环境：写操作会要求再次确认目标与语句。请核对连接信息。 ")).TextColor(c.Theme().Warning)
	}

}

func (app *workbench) connectionProfileFormActions(c *ui.Context) {
	disabled := app.busy || app.exporting || app.dialogOpen
	profileTestMessage := app.profileTestMessage
	profileTestState := app.profileTestState
	if (app.profileTestState == profileTestPassed || app.profileTestState == profileTestFailed) && app.profileTestFingerprint != app.connectionProfileTestFingerprint() {
		profileTestMessage = "连接配置已更改，请重新测试。"
		profileTestState = profileTestNone
	}
	if profileTestMessage != "" {
		color := c.Theme().Warning
		switch profileTestState {
		case profileTestPassed:
			color = c.Theme().Success
		case profileTestFailed:
			color = c.Theme().Danger
		}
		ui.Text(c, app.localized(profileTestMessage)).TextColor(color).Padding(4, 8)
	}
	if app.profileTestDetails != "" && profileTestState == profileTestFailed {
		ui.Text(c, app.profileTestDetails).TextColor(c.Theme().TextMuted).Selectable().Padding(4, 8)
	}
	ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
		ui.Spacer(c)
		ui.Toolbar(c, func() {
			test := iconButton(c, app.localized("测试连接"), actionIcons["plug"]).Size(32, 32).Padding(3).Radius(8)
			testDisabled := disabled || (app.profileDriver == preferences.DriverSQLite && strings.TrimSpace(app.path) == "") || (app.profileDriver != preferences.DriverSQLite && strings.TrimSpace(app.profileHost) == "")
			test.Disabled(testDisabled)
			if test.Clicked() {
				app.testConnectionProfileFromForm()
			}
			saveDisabled := disabled || app.preferences == nil || strings.TrimSpace(app.profileName) == ""
			if app.profileTestState != profileTestPassed || app.profileTestFingerprint != app.connectionProfileTestFingerprint() {
				saveDisabled = true
			}
			if app.profileDriver == preferences.DriverSQLite && strings.TrimSpace(app.path) == "" {
				saveDisabled = true
			} else if app.profileDriver != preferences.DriverSQLite && strings.TrimSpace(app.profileHost) == "" {
				saveDisabled = true
			}
			save := iconButton(c, app.localized("保存连接配置"), actionIcons["save"]).Size(32, 32).Padding(3).Radius(8)
			save.Disabled(saveDisabled)
			if save.Clicked() {
				app.saveConnectionProfile(false)
			}
			saveConnect := iconPrimaryButton(c, app.localized("保存并连接"), actionIcons["saveConnect"]).Size(32, 32).Padding(3).Radius(8)
			saveConnect.Disabled(saveDisabled)
			if saveConnect.Clicked() {
				app.saveConnectionProfile(true)
			}
		}).Label(app.localized("连接配置操作"))
	})
}
