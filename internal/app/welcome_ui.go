package app

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/magalab/dbl/internal/connection"
	"github.com/magalab/dbl/internal/databaseicons"
	"github.com/magalab/dbl/internal/preferences"
	"github.com/magalab/dbl/internal/sqlite"
	"github.com/magalab/dbl/internal/sqlstream"
)

func (app *workbench) connectionWelcome(c *ui.Context) {
	theme := c.Theme()
	width, _ := c.Size()
	connected := len(app.liveConnections)
	drivers := make(map[string]struct{})
	for _, profile := range app.connectionProfiles {
		drivers[profile.Driver] = struct{}{}
	}
	profiles := app.connectionProfiles
	if len(profiles) > 5 {
		profiles = profiles[:5]
	}
	history := app.welcomeHistory()

	ui.Column(c).Fill().AlignItems(ui.Center).Children(func() {
		ui.Scroll(c).Fill().Children(func() {
			ui.Column(c).Width(1380).MaxWidthPercent(96).Padding(24).Gap(18).Children(func() {
				app.welcomeConnectionFeedback(c)
				ui.Row(c).FillWidth().Gap(12).Children(func() {
					app.welcomeStat(c, app.localized("连接总数"), fmt.Sprint(len(app.connectionProfiles)), actionIcons["network"])
					app.welcomeStat(c, app.localized("已连接"), fmt.Sprint(connected), actionIcons["plug"])
					app.welcomeStat(c, app.localized("数据库类型"), fmt.Sprint(len(drivers)), actionIcons["database"])
				})
				quickStart := func() {
					ui.Column(c).Grow(5).Background(theme.Surface).Border(1, theme.Border).Radius(theme.Radius).Children(func() {
						ui.Text(c, app.localized("快速开始")).FillWidth().Padding(14, 18).Bold().Role(ui.RoleHeading).Level(2)
						if len(profiles) == 0 && len(app.recentSQLitePaths) == 0 {
							ui.Text(c, app.localized("没有已保存连接。新建连接或打开一个 SQLite 文件开始使用。 ")).Padding(14, 18).TextColor(theme.TextMuted)
						}
						for _, profile := range profiles {
							profile := profile
							icon, _, hasIcon := databaseicons.Icon(profile.Driver)
							button := ui.Button(c, "").Label(app.localized("打开连接")+" "+profile.Name).FillWidth().Padding(12, 18).Background(theme.Surface).BorderWidth(0)
							button.Children(func() {
								ui.Row(c).FillWidth().Gap(12).AlignItems(ui.Center).Children(func() {
									if hasIcon {
										ui.Icon(c, icon).Size(22, 22)
									}
									ui.Column(c).Grow(1).Gap(2).Children(func() {
										name := profile.Name
										if name == "" {
											name = welcomeDriverLabel(profile.Driver)
										}
										ui.Text(c, name).Bold().SingleLine()
										ui.Text(c, app.connectionWelcomeProfileDetail(profile)).TextColor(theme.TextMuted).SingleLine()
									})
									if app.profileHasLiveConnection(profile) {
										ui.Text(c, "●").TextColor(theme.Success)
									} else {
										ui.Icon(c, actionIcons["open"]).Size(16, 16)
									}
								})
							})
							button.Disabled(app.busy || app.exporting || app.dialogOpen)
							if button.Clicked() {
								app.openSavedProfile(profile)
							}
						}
						if len(profiles) == 0 {
							for _, path := range app.recentSQLitePaths {
								path := path
								name := filepath.Base(path)
								button := ui.Button(c, name).FillWidth().Padding(12, 18).Background(theme.Surface).BorderWidth(0)
								button.Disabled(app.busy || app.exporting || app.dialogOpen)
								if button.Clicked() {
									app.path = path
									app.openSQLite(sqlite.ReadWriteExisting)
								}
							}
						}
					})
				}
				commonActions := func() {
					ui.Column(c).Grow(3).Background(theme.Surface).Border(1, theme.Border).Radius(theme.Radius).Padding(16).Gap(8).Children(func() {
						ui.Text(c, app.localized("常用操作")).Bold().Role(ui.RoleHeading).Level(2).Padding(0, 2, 8, 2)
						app.welcomeAction(c, app.localized("新建连接…"), actionIcons["add"], app.busy || app.exporting || app.dialogOpen, app.beginNewConnection)
						app.welcomeAction(c, app.localized("新建查询"), actionIcons["query"], app.busy || app.exporting || app.dialogOpen, app.openWelcomeQuery)
						app.welcomeAction(c, app.localized("查询历史"), actionIcons["history"], app.busy || app.exporting || app.dialogOpen, app.openWelcomeHistory)
						app.welcomeAction(c, app.localized("打开 SQLite 文件…"), actionIcons["folder"], app.busy || app.exporting || app.dialogOpen, app.chooseSQLiteFile)
						ui.Row(c).FillWidth().Padding(10, 12).Gap(8).Background(theme.Background).Radius(theme.Radius).Children(func() {
							ui.Icon(c, actionIcons["search"]).Size(16, 16)
							ui.Text(c, app.localized("也可以从左侧展开连接，或右键对象查看操作。")).TextColor(theme.TextMuted).SingleLine()
						})
					})
				}
				if width >= 900 {
					ui.Row(c).FillWidth().Gap(16).AlignItems(ui.Stretch).Children(func() {
						quickStart()
						commonActions()
					})
				} else {
					ui.Column(c).FillWidth().Gap(16).Children(func() {
						quickStart()
						commonActions()
					})
				}
				app.welcomeHistoryPanel(c, history)
				ui.Row(c).FillWidth().Padding(16).Gap(12).AlignItems(ui.Center).Background(theme.Surface).Border(1, theme.Border).Radius(theme.Radius).Children(func() {
					ui.Icon(c, actionIcons["sparkle"]).Size(22, 22)
					ui.Column(c).Grow(1).Gap(4).Children(func() {
						ui.Text(c, app.localized("AI 助手")).Bold().Role(ui.RoleHeading).Level(2)
						ui.Text(c, app.localized("根据当前数据库上下文生成 SQL 草稿；发送和执行前都需要你确认。 ")).TextColor(theme.TextMuted)
					})
					openAI := ui.Button(c, app.localized("打开 AI 助手")).Label(app.localized("打开 AI 助手"))
					openAI.Disabled(app.busy || app.exporting || app.dialogOpen)
					if openAI.Clicked() {
						app.showAI = true
					}
				})
			})
		})
	})
}

func (app *workbench) welcomeConnectionFeedback(c *ui.Context) {
	if app.welcomeConnectionProfileID == "" {
		return
	}
	theme := c.Theme()
	if app.welcomeConnectionPending {
		ui.Row(c).FillWidth().Padding(12).Gap(12).AlignItems(ui.Center).Background(theme.Surface).Border(1, theme.Accent).Radius(theme.Radius).Children(func() {
			ui.Spinner(c).Size(20, 20).TextColor(theme.Accent).Label(app.localized("正在连接数据库"))
			ui.Column(c).Grow(1).Gap(3).Children(func() {
				ui.Text(c, app.localized("正在连接：")+app.welcomeConnectionProfileName).Bold()
				ui.Text(c, app.localized("连接失败时会在这里显示原因。 ")).TextColor(theme.TextMuted)
			})
		})
		return
	}
	if app.errorSummary != app.status || app.errorSummary == "" {
		return
	}
	ui.Column(c).FillWidth().Padding(12).Gap(8).Background(theme.Surface.Mix(theme.Danger, 0.06)).Border(1, theme.Border.Mix(theme.Danger, 0.45)).Radius(theme.Radius).Children(func() {
		ui.Row(c).FillWidth().Gap(10).AlignItems(ui.Center).Children(func() {
			ui.Icon(c, actionIcons["warning"]).Size(20, 20).TextColor(theme.Danger)
			ui.Column(c).Grow(1).Gap(3).Children(func() {
				ui.Text(c, app.localized("连接失败：")+app.welcomeConnectionProfileName).Bold().TextColor(theme.Danger)
				ui.Text(c, app.localized(app.errorSummary)).FillWidth()
			})
			retry := iconButton(c, app.localized("重试连接"), actionIcons["refresh"]).Size(32, 32).
				BorderWidth(0).TextColor(theme.Accent).Background(theme.Surface.Mix(theme.Accent, 0.12)).
				Disabled(app.busy || app.exporting || app.dialogOpen)
			if retry.Hovered() {
				retry.Background(theme.Surface.Mix(theme.Accent, 0.22))
			}
			if retry.Clicked() {
				app.retryWelcomeConnection()
			}
			close := iconButton(c, app.localized("关闭提示"), actionIcons["close"]).Size(32, 32).BorderWidth(0)
			if close.Clicked() {
				app.dismissWelcomeConnectionFeedback()
			}
		})
		if app.errorDetails != "" {
			ui.Text(c, app.errorDetails).FillWidth().TextColor(theme.TextMuted).Selectable()
		}
	})
}

func (app *workbench) welcomeStat(c *ui.Context, label, value string, icon *ui.SVG) {
	theme := c.Theme()
	ui.Column(c).Grow(1).Padding(16, 18).Gap(10).Background(theme.Surface).Border(1, theme.Border).Radius(theme.Radius).Children(func() {
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			if icon != nil {
				ui.Icon(c, icon).Size(16, 16)
			}
			ui.Text(c, label).TextColor(theme.TextMuted)
		})
		ui.Text(c, value).Bold().Role(ui.RoleHeading).Level(1)
	})
}

func (app *workbench) welcomeAction(c *ui.Context, label string, icon *ui.SVG, disabled bool, action func()) {
	theme := c.Theme()
	button := ui.Button(c, "").Label(label).FillWidth().Padding(10, 12).Background(theme.Surface).BorderWidth(0)
	button.Children(func() {
		ui.Row(c).FillWidth().Gap(10).AlignItems(ui.Center).Children(func() {
			ui.Icon(c, icon).Size(18, 18)
			ui.Text(c, label).SingleLine()
		})
	})
	button.Disabled(disabled)
	if button.Clicked() {
		action()
	}
}

func (app *workbench) connectionWelcomeProfileDetail(profile preferences.ConnectionProfile) string {
	driver := welcomeDriverLabel(profile.Driver)
	detail := []string{driver, app.localized(environmentLabel(profile.Environment))}
	if profile.Driver == preferences.DriverSQLite {
		if profile.Path != "" {
			detail = append(detail, filepath.Base(profile.Path))
		}
	} else {
		endpoint := profile.Host
		if endpoint != "" && profile.Port > 0 {
			endpoint = fmt.Sprintf("%s:%d", endpoint, profile.Port)
		}
		if endpoint != "" {
			detail = append(detail, endpoint)
		}
		if profile.Database != "" {
			detail = append(detail, profile.Database)
		}
	}
	if profile.Driver == preferences.DriverRedis {
		detail = append(detail, app.redisDatabaseLabelForProfile(profile, profile.RedisDB))
	}
	return strings.Join(detail, " · ")
}

func welcomeDriverLabel(driver string) string {
	if label, ok := map[string]string{
		preferences.DriverSQLite:     "SQLite",
		preferences.DriverPostgreSQL: "PostgreSQL",
		preferences.DriverMySQL:      "MySQL",
		preferences.DriverRedis:      "Redis",
		preferences.DriverMongoDB:    "MongoDB",
		preferences.DriverEtcd:       "etcd",
	}[driver]; ok {
		return label
	}
	return driver
}

func (app *workbench) profileHasLiveConnection(profile preferences.ConnectionProfile) bool {
	_, ok := app.liveConnections[app.profileLiveSessionID(profile)]
	return ok
}

func (app *workbench) welcomeHistory() []queryRecord {
	var history []queryRecord
	for _, workspace := range app.workspaces {
		if workspace == nil || workspace.dataView || workspace.schemaView || workspace.connectionView {
			continue
		}
		history = append(history, workspace.history...)
	}
	sort.SliceStable(history, func(i, j int) bool {
		return history[i].Started.After(history[j].Started)
	})
	if len(history) > 5 {
		history = history[:5]
	}
	return history
}

func (app *workbench) welcomeHistoryPanel(c *ui.Context, history []queryRecord) {
	theme := c.Theme()
	ui.Column(c).FillWidth().Background(theme.Surface).Border(1, theme.Border).Radius(theme.Radius).Children(func() {
		ui.Row(c).FillWidth().Padding(12, 16).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Icon(c, actionIcons["history"]).Size(18, 18)
			ui.Text(c, app.localized("历史查询")).Bold().Role(ui.RoleHeading).Level(2).Grow(1)
			if len(history) > 0 {
				more := ui.Button(c, app.localized("查看全部"))
				if more.Clicked() {
					app.openWelcomeHistory()
				}
			}
		})
		if len(history) == 0 {
			ui.Text(c, app.localized("还没有查询记录。 ")).Padding(18, 16).TextColor(theme.TextMuted)
			return
		}
		for _, record := range history {
			record := record
			button := ui.Button(c, "").Label(shortSQL(record.SQL)).FillWidth().Padding(10, 16).Background(theme.Surface).BorderWidth(0)
			button.Children(func() {
				ui.Row(c).FillWidth().Gap(12).AlignItems(ui.Center).Children(func() {
					ui.Icon(c, actionIcons["query"]).Size(16, 16)
					ui.Text(c, shortSQL(record.SQL)).Grow(1).SingleLine()
					ui.Text(c, record.Started.Format("2006-01-02 15:04")).TextColor(theme.TextMuted).SingleLine()
				})
			})
			if button.Clicked() {
				app.openWelcomeHistorySQL(record.SQL)
			}
		}
	})
}

func (app *workbench) openWelcomeQuery() {
	if app.busy || app.exporting || app.dialogOpen {
		return
	}
	app.addWorkspace()
}

func (app *workbench) openWelcomeHistory() {
	if app.busy || app.exporting || app.dialogOpen {
		return
	}
	if !app.queryWorkspaceRequested {
		app.queryWorkspaceRequested = true
		app.activeWorkspace = 0
		app.activeQuery = app.workspaces[0]
	}
	if app.activeQuery != nil {
		app.activeQuery.implicitQuery = false
		app.activeQuery.showHistory = true
	}
}

func (app *workbench) openWelcomeHistorySQL(statement string) {
	if app.busy || app.exporting || app.dialogOpen || len(app.workspaces) == 0 {
		return
	}
	workspace := app.workspaces[0]
	app.cancelResultCountForWorkspace(workspace)
	app.queryWorkspaceRequested = true
	workspace.implicitQuery = false
	app.activeWorkspace = 0
	app.activeQuery = workspace
	workspace.connectionView, workspace.dataView, workspace.schemaView = false, false, false
	workspace.queryConnectionID, workspace.resultConnectionID, workspace.resultPath = "", "", ""
	workspace.resultSQL, workspace.columns = "", nil
	workspace.resultWindow = sqlstream.Window{MaxRows: maxDisplayedRows, MaxBytes: maxRetainedResultBytes}
	workspace.resultTotal = 0
	workspace.resultTotalKnown = false
	workspace.resultTotalCounted = false
	workspace.resultCountCancelled = false
	workspace.activeHistoryID = 0
	workspace.streamStopped = false
	workspace.showHistory = false
	workspace.query.Buffer.Set(statement)
	app.resetResultOrder()
	app.status = app.localized("已将历史 SQL 放入查询工作区。")
}

func (app *workbench) sqlConnectionOverview(c *ui.Context) {
	theme := c.Theme()
	driver := string(app.openedDriver)
	profile := app.openedSQLProfile
	live := app.liveConnections[app.openedConnectionID]
	name := profile.Name
	if name == "" {
		name = live.profile.Name
	}
	if app.openedPath != "" && (name == "" || name == app.openedPath) {
		name = filepath.Base(app.openedPath)
	}
	if name == "" {
		name = app.openedLabel
	}
	if name == "" {
		name = welcomeDriverLabel(driver)
	}
	database := profile.Database
	if database == "" {
		database = live.tree.database
	}
	access := "读写"
	if app.openedReadOnly {
		access = "只读"
	}
	fields := []overviewField{
		{"数据库版本", live.sqlVersion, "info"},
		{"主机 / Endpoint", profileEndpoint(profile), "plug"},
		{"SQLite 文件路径", app.openedPath, "folder"},
		{"文件大小", live.sqliteFileSize, "memory"},
		{"数据库", database, "database"},
		{"访问模式", app.localized(access), "shield"},
	}
	if app.openedDriver == connection.DriverPostgreSQL && app.sqlNamespace != "" {
		fields = append(fields, overviewField{"当前 Schema", app.sqlNamespace, "schema"})
	} else if app.openedDriver == connection.DriverMySQL && app.sqlNamespace != "" {
		fields = append(fields, overviewField{"当前数据库", app.sqlNamespace, "database"})
	}
	if app.openedEnvironment != "" {
		fields = append(fields, overviewField{"环境", app.localized(environmentLabel(app.openedEnvironment)), "tag"})
	}

	ui.Scroll(c).Fill().Children(func() {
		ui.Column(c).FillWidth().AlignItems(ui.Center).Padding(24).Children(func() {
			ui.Column(c).Width(680).MaxWidthPercent(100).Gap(14).Children(func() {
				ui.Row(c).FillWidth().Gap(10).AlignItems(ui.Center).Children(func() {
					if icon, color, ok := databaseicons.Icon(driver); ok {
						ui.Icon(c, icon).Size(24, 24).TextColor(color).Tooltip(welcomeDriverLabel(driver))
					}
					ui.Text(c, name).Grow(1).MinWidth(0).FontSize(theme.FontSize + 6).Bold().Role(ui.RoleHeading).Level(1)
					ui.Text(c, app.localized("已连接")).TextColor(theme.Success)
				})
				ui.Column(c).FillWidth().Padding(12, 16).Gap(10).
					Background(theme.Surface).Border(1, theme.Border).Radius(theme.Radius).Children(func() {
					for _, field := range fields {
						app.connectionOverviewField(c, field)
					}
				})
				app.sqlSchemaOverview(c)
				ui.Row(c).FillWidth().Gap(12).AlignItems(ui.Center).Children(func() {
					ui.Spacer(c)
					newQuery := iconPrimaryButton(c, app.localized("新建查询工作区"), actionIcons["query"]).Size(32, 32).Padding(6)
					newQuery.Disabled(app.busy || app.exporting || app.dialogOpen)
					if newQuery.Clicked() {
						app.addWorkspace()
					}
				})
			})
		})
	})
}

func (app *workbench) sqlSchemaOverview(c *ui.Context) {
	theme := c.Theme()
	type objectCounts struct{ tables, views int }
	counts := make(map[string]objectCounts)
	for _, object := range app.objects {
		count := counts[object.Schema]
		switch object.Type {
		case "table":
			count.tables++
		case "view", "materialized view":
			count.views++
		default:
			continue
		}
		counts[object.Schema] = count
	}
	ui.Column(c).FillWidth().Gap(6).Children(func() {
		ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
			label := "Schema 列表"
			if app.openedDriver == connection.DriverMySQL {
				label = "数据库列表"
			}
			ui.Text(c, app.localized(label)).Bold().Role(ui.RoleHeading).Level(2).Grow(1)
			if count := len(app.sqlNamespaces); count > 0 {
				ui.Text(c, fmt.Sprint(count)).TextColor(theme.TextMuted)
			}
		})
		if len(app.sqlNamespaces) == 0 {
			ui.Text(c, app.localized("暂无 Schema 信息")).TextColor(theme.TextMuted)
			return
		}
		const rowHeight = 30
		ui.List(c, nil, len(app.sqlNamespaces), func(i int) {
			namespace := app.sqlNamespaces[i]
			iconName := "schema"
			if app.openedDriver == connection.DriverMySQL {
				iconName = "database"
			}
			details := ""
			if count, ok := counts[namespace]; ok {
				details = fmt.Sprintf(app.localized("已加载：%d 表 · %d 视图"), count.tables, count.views)
			}
			button := app.overviewListButton(c, namespace, app.localized("切换到此数据库 / Schema"), iconName, namespace == app.sqlNamespace, details)
			button.Height(rowHeight).Padding(0, 6).Disabled(app.busy || app.exporting || app.dialogOpen)
			if button.Clicked() && (app.openedDriver == connection.DriverPostgreSQL || app.openedDriver == connection.DriverMySQL) {
				app.switchSQLNamespace(namespace)
			}
		}).FillWidth().Height(float32(min(len(app.sqlNamespaces), 6) * rowHeight))
	})
}

func (app *workbench) sqlNamespaceOverview(c *ui.Context, namespace string) {
	theme := c.Theme()
	objects := make([]connection.Object, 0)
	tables, views := 0, 0
	for _, object := range app.objects {
		if object.Schema != namespace || !isSQLTableOrView(object.Type) {
			continue
		}
		objects = append(objects, object)
		if object.Type == "table" {
			tables++
		} else {
			views++
		}
	}
	kind := "Schema 概览"
	iconName := "schema"
	access := "读写"
	if app.openedReadOnly {
		access = "只读"
	}
	if app.openedDriver == connection.DriverMySQL {
		kind = "数据库概览"
		iconName = "database"
	}
	ui.Scroll(c).Fill().Children(func() {
		ui.Column(c).FillWidth().AlignItems(ui.Center).Padding(24).Children(func() {
			ui.Column(c).Width(760).MaxWidthPercent(100).Gap(14).Children(func() {
				ui.Row(c).FillWidth().Gap(10).AlignItems(ui.Center).Children(func() {
					if icon := treeItemIcons[iconName]; icon != nil {
						ui.Icon(c, icon).Size(24, 24).Shrink(0).TextColor(treeItemIconColors[iconName])
					}
					ui.Column(c).Grow(1).Gap(2).Children(func() {
						ui.Text(c, namespace).FontSize(theme.FontSize + 6).Bold().Role(ui.RoleHeading).Level(1)
						ui.Text(c, app.localized(kind)+" · "+app.openedLabel).TextColor(theme.TextMuted).SingleLine()
					})
					ui.Text(c, fmt.Sprintf(app.localized("已加载：%d 表 · %d 视图"), tables, views)).TextColor(theme.TextMuted)
				})
				ui.Row(c).FillWidth().Gap(12).Children(func() {
					for _, field := range []overviewField{
						{"主机 / Endpoint", profileEndpoint(app.openedSQLProfile), "plug"},
						{"访问模式", app.localized(access), "shield"},
					} {
						ui.Column(c).Grow(1).Padding(12, 16).Gap(8).Background(theme.Surface).Border(1, theme.Border).Radius(theme.Radius).Children(func() {
							app.connectionOverviewField(c, field)
						})
					}
				})
				ui.Column(c).FillWidth().Gap(6).Padding(12, 16).Background(theme.Surface).Border(1, theme.Border).Radius(theme.Radius).Children(func() {
					ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
						ui.Text(c, app.localized("表和视图")).Bold().Role(ui.RoleHeading).Level(2).Grow(1)
						ui.Text(c, fmt.Sprint(len(objects))).TextColor(theme.TextMuted)
					})
					if len(objects) == 0 {
						ui.Text(c, app.localized("此数据库或 Schema 暂无已加载的表和视图。")).TextColor(theme.TextMuted)
						return
					}
					for _, object := range objects {
						object := object
						typeLabel := "表"
						if object.Type != "table" {
							typeLabel = "视图"
						}
						button := app.overviewListButton(c, object.Name, app.localized("查看数据"), sqlObjectIcon(object.Type), false, app.localized(typeLabel))
						button.Disabled(app.busy || app.exporting || app.dialogOpen)
						if button.Clicked() {
							app.browseObjectData(object)
						}
					}
				})
			})
		})
	})
}

func (app *workbench) mongoConnectionOverview(c *ui.Context) {
	id := app.openedConnectionID
	live := app.liveConnections[id]
	app.readMongoOverview(id, live)
	theme := c.Theme()
	name := live.profile.Name
	if name == "" {
		name = "MongoDB"
	}
	topology, version := app.localized("不可读取"), live.mongoOverview.Version
	switch live.mongoOverview.Topology {
	case "sharded":
		topology = app.localized("分片集群")
	case "replica":
		topology = app.localized("副本集")
	case "standalone":
		topology = app.localized("单节点")
	}
	if version == "" {
		version = app.localized("不可读取")
	}
	ui.Scroll(c).Fill().Children(func() {
		ui.Column(c).FillWidth().Padding(24).AlignItems(ui.Center).Children(func() {
			ui.Column(c).Width(680).MaxWidthPercent(100).Gap(12).Children(func() {
				ui.Row(c).FillWidth().Gap(10).AlignItems(ui.Center).Children(func() {
					if icon, color, ok := databaseicons.Icon("mongodb"); ok {
						ui.Icon(c, icon).Size(24, 24).TextColor(color)
					}
					ui.Text(c, name).Grow(1).MinWidth(0).FontSize(theme.FontSize + 6).Bold().Role(ui.RoleHeading).Level(1)
					ui.Text(c, app.localized("已连接")).TextColor(theme.Success)
				})
				for _, field := range []overviewField{
					{"主机 / Endpoint", profileEndpoint(live.profile), "plug"},
					{"数据库版本", version, "info"},
					{"部署类型", topology, "network"},
				} {
					app.connectionOverviewField(c, field)
				}
				ui.Text(c, app.localized("选择数据库和集合。文档结果按需加载。")).TextColor(theme.TextMuted)
				if app.mongoSelectedDB != "" {
					ui.Text(c, fmt.Sprintf(app.localized("%s · %d 个可见集合"), app.mongoSelectedDB, len(app.mongoCollections))).Bold()
				}
				if len(app.mongoDatabases) > 0 {
					ui.Text(c, app.localized("可见数据库")).Bold().Role(ui.RoleHeading).Level(2)
					for _, database := range app.mongoDatabases {
						button := app.overviewListButton(c, database, app.localized("切换到此数据库"), "database", database == app.mongoSelectedDB, "")
						button.Disabled(app.busy || app.exporting || app.dialogOpen)
						if button.Clicked() && database != app.mongoSelectedDB {
							app.selectMongoDatabase(database)
						}
					}
				}
			})
		})
	})
}

func (app *workbench) mongoDatabaseOverview(c *ui.Context) {
	theme := c.Theme()
	name := app.mongoSelectedDB
	ui.Scroll(c).Fill().Children(func() {
		ui.Column(c).FillWidth().AlignItems(ui.Center).Padding(24).Children(func() {
			ui.Column(c).Width(680).MaxWidthPercent(100).Gap(12).Children(func() {
				ui.Row(c).FillWidth().Gap(10).AlignItems(ui.Center).Children(func() {
					ui.Icon(c, treeItemIcons["database"]).Size(24, 24).TextColor(treeItemIconColors["database"])
					ui.Column(c).Grow(1).Gap(2).Children(func() {
						ui.Text(c, name).FontSize(theme.FontSize + 6).Bold().Role(ui.RoleHeading).Level(1)
						ui.Text(c, app.openedLabel).TextColor(theme.TextMuted).SingleLine()
					})
					ui.Text(c, fmt.Sprintf(app.localized("%d 个可见集合"), len(app.mongoCollections))).TextColor(theme.TextMuted)
				})
				ui.Column(c).FillWidth().Gap(6).Padding(12, 16).Background(theme.Surface).Border(1, theme.Border).Radius(theme.Radius).Children(func() {
					ui.Text(c, app.localized("集合列表")).Bold().Role(ui.RoleHeading).Level(2)
					if len(app.mongoCollections) == 0 {
						ui.Text(c, app.localized("此数据库没有可见集合。")).TextColor(theme.TextMuted)
						return
					}
					for _, collection := range app.mongoCollections {
						button := app.overviewListButton(c, collection, app.localized("浏览集合"), "table", false, "")
						button.Disabled(app.busy || app.exporting || app.dialogOpen)
						if button.Clicked() {
							app.selectMongoCollection(collection)
						}
					}
				})
			})
		})
	})
}

func (app *workbench) overviewListButton(c *ui.Context, label, tooltip, iconName string, selected bool, details string) ui.Element {
	theme := c.Theme()
	button := ui.Button(c, "").Label(label).FillWidth().Height(34).Padding(6, 8).
		Background(ui.Transparent).BorderWidth(0).Tooltip(tooltip)
	if selected || button.Hovered() {
		button.Background(theme.SurfaceHover)
	}
	button.Children(func() {
		ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
			if icon := treeItemIcons[iconName]; icon != nil {
				ui.Icon(c, icon).Size(16, 16).Shrink(0).TextColor(treeItemIconColors[iconName])
			}
			text := ui.Text(c, label).Grow(1).MinWidth(0).SingleLine().Ellipsis("…").TextColor(theme.Text)
			if selected {
				text.Bold()
			}
			if details != "" {
				ui.Text(c, details).TextColor(theme.TextMuted).SingleLine()
			}
			if selected {
				ui.Icon(c, actionIcons["check"]).Size(16, 16).Shrink(0).TextColor(theme.Accent)
			}
		})
	})
	return button
}

type overviewField struct{ label, value, icon string }

func (app *workbench) connectionOverviewField(c *ui.Context, field overviewField) {
	if field.value == "" {
		return
	}
	label := app.localized(field.label)
	row := ui.Row(c).FillWidth().Gap(10).AlignItems(ui.Center).Role(ui.RoleGroup).Label(label)
	row.Children(func() {
		ui.Icon(c, actionIcons[field.icon]).Size(18, 18).Shrink(0).TextColor(c.Theme().TextMuted).Tooltip(label)
		ui.Text(c, field.value).Grow(1).MinWidth(0).Selectable()
		app.copyFieldButton(c, row, label, field.value, writeClipboardText)
	})
}
