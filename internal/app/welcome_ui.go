package app

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/egoist/mygo/ui"
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
					app.welcomeStat(c, app.localized("连接总数"), fmt.Sprint(len(app.connectionProfiles)), actionIcons["folder"])
					app.welcomeStat(c, app.localized("已连接"), fmt.Sprint(connected), actionIcons["refresh"])
					app.welcomeStat(c, app.localized("数据库类型"), fmt.Sprint(len(drivers)), actionIcons["sparkle"])
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
	ui.Column(c).FillWidth().Padding(12).Gap(8).Background(theme.Surface).Border(1, theme.Danger).Radius(theme.Radius).Children(func() {
		ui.Row(c).FillWidth().Gap(10).AlignItems(ui.Center).Children(func() {
			ui.Icon(c, actionIcons["warning"]).Size(20, 20).TextColor(theme.Danger)
			ui.Column(c).Grow(1).Gap(3).Children(func() {
				ui.Text(c, app.localized("连接失败：")+app.welcomeConnectionProfileName).Bold().TextColor(theme.Danger)
				ui.Text(c, app.localized(app.errorSummary)).FillWidth()
			})
			retry := ui.Button(c, app.localized("重试连接"))
			retry.Disabled(app.busy || app.exporting || app.dialogOpen)
			if retry.Clicked() {
				app.retryWelcomeConnection()
			}
			close := iconButton(c, app.localized("关闭提示"), actionIcons["close"])
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
	ui.Column(c).Fill().Justify(ui.Center).AlignItems(ui.Center).Gap(12).Padding(24).Children(func() {
		ui.Text(c, app.localized("数据库已连接")).Bold().Role(ui.RoleHeading).Level(1)
		if app.selectedObject != "" {
			ui.Text(c, app.localized("单击左侧表名打开数据面板；选择对象后可查看详情。 ")).TextColor(theme.TextMuted)
		} else {
			ui.Text(c, app.localized("从左侧目录选择对象查看详情，或新建查询工作区。 ")).TextColor(theme.TextMuted)
		}
		newQuery := ui.PrimaryButton(c, app.localized("新建查询工作区"))
		newQuery.Disabled(app.busy || app.exporting || app.dialogOpen)
		if newQuery.Clicked() {
			app.addWorkspace()
		}
	})
}
