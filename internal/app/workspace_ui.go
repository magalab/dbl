package app

import (
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
	"github.com/magalab/dbl/internal/connection"
)

func (app *workbench) workspace(c *ui.Context) {
	ui.Column(c).Fill().Children(func() {
		if app.queryWorkspaceRequested {
			app.workspaceTabs(c)
		}
		if app.activeQuery != nil && app.activeQuery.connectionView && !connectionViewAvailable(app, app.activeQuery) {
			ui.Column(c).Fill().Justify(ui.Center).AlignItems(ui.Center).Gap(10).Children(func() {
				ui.Text(c, app.activeQuery.title).Bold().Role(ui.RoleHeading).Level(1)
				ui.Text(c, app.localized("连接已关闭")).TextColor(c.Theme().TextMuted)
				for _, profile := range app.connectionProfiles {
					if app.activeQuery.connectionDriver == connection.DriverRedis && strings.HasPrefix(app.activeQuery.connectionID, "redis:"+profile.ID+":db:") {
						if database, err := strconv.Atoi(strings.TrimPrefix(app.activeQuery.connectionID, "redis:"+profile.ID+":db:")); err == nil {
							profile.RedisDB = database
						}
						reconnect := ui.PrimaryButton(c, app.localized("重新连接"))
						reconnect.Disabled(app.busy || app.exporting || app.dialogOpen)
						if reconnect.Clicked() {
							app.openRedis(profile)
						}
						break
					}
					if profileConnectionID(profile) == app.activeQuery.connectionID {
						reconnect := ui.PrimaryButton(c, app.localized("重新连接"))
						reconnect.Disabled(app.busy || app.exporting || app.dialogOpen)
						if reconnect.Clicked() {
							app.openSavedProfile(profile)
						}
						break
					}
				}
			})
			return
		}
		if app.aiFullPage {
			app.aiWorkspace(c)
			return
		}
		if app.documentStore != nil {
			app.ensureMongoQueryEditors()
			ui.Row(c).Padding(8, 12).Gap(10).Children(func() {
				ui.Text(c, app.localized("MongoDB 文档")).Bold().Role(ui.RoleHeading).Level(1)
				ui.Text(c, app.openedLabel+" · "+app.localized(environmentLabel(app.openedEnvironment))).TextColor(c.Theme().TextMuted).SingleLine()
			})
			ui.Scroll(c).Fill().Children(func() {
				ui.Column(c).Padding(12).Gap(8).Children(func() {
					if app.mongoSelectedColl == "" {
						ui.Text(c, app.localized("从左侧选择数据库和集合。文档结果按需加载。")).TextColor(c.Theme().TextMuted)
						return
					}
					ui.Text(c, app.mongoSelectedDB+" / "+app.mongoSelectedColl).Bold()
					mode := ui.Checkbox(c, &app.mongoUsePipeline, app.localized("使用聚合管道"))
					mode.Disabled(app.busy || app.dialogOpen)
					if app.mongoUsePipeline {
						app.mongoPipelineEditor.ReadOnly = app.busy || app.dialogOpen
						editor.View(c, app.mongoPipelineEditor).FillWidth().Label(app.localized("MongoDB aggregation pipeline JSON"))
					} else {
						app.mongoFilterEditor.ReadOnly = app.busy || app.dialogOpen
						app.mongoProjectionEditor.ReadOnly = app.busy || app.dialogOpen
						editor.View(c, app.mongoFilterEditor).FillWidth().Label(app.localized("MongoDB filter JSON"))
						editor.View(c, app.mongoProjectionEditor).FillWidth().Label(app.localized("MongoDB projection JSON"))
					}
					ui.Row(c).Gap(6).Children(func() {
						search := ui.PrimaryButton(c, app.localized("查询文档"))
						search.Disabled(app.busy || app.dialogOpen)
						if search.Clicked() {
							app.queryMongoDocuments(true)
						}
						if app.mongoHasMore {
							more := ui.Button(c, app.localized("加载下一页"))
							more.Disabled(app.busy || app.dialogOpen)
							if more.Clicked() {
								app.queryMongoDocuments(false)
							}
						}
						create := ui.Button(c, app.localized("新建文档"))
						create.Disabled(app.busy || app.dialogOpen || app.openedReadOnly)
						if create.Clicked() {
							app.newMongoDocument()
						}
					})
					if len(app.mongoDocuments) == 0 && app.mongoSelectedID == "" && !app.mongoNewDocument {
						ui.Text(c, app.localized("执行查询以加载文档。"))
					}
					for _, document := range app.mongoDocuments {
						label := document.IDJSON + "  " + document.PreviewJSON
						if document.Truncated {
							label += " …"
						}
						button := ui.Button(c, label).Selected(document.IDJSON == app.mongoSelectedID)
						button.Disabled(app.busy || app.dialogOpen)
						if button.Clicked() {
							app.inspectMongoDocument(document.IDJSON)
						}
					}
					if app.mongoEditor != nil {
						title := "文档详情"
						if app.mongoNewDocument {
							title = "新建文档 JSON"
						} else {
							ui.Text(c, app.localized("文档 ID：")+app.mongoSelectedID).TextColor(c.Theme().TextMuted)
						}
						ui.Text(c, title).Role(ui.RoleHeading).Level(2)
						app.mongoEditor.ReadOnly = app.mongoDocumentTruncated || app.busy || app.dialogOpen || (!app.mongoNewDocument && app.openedReadOnly)
						editor.View(c, app.mongoEditor).FillWidth().Label(app.localized("MongoDB 文档 JSON"))
						if !app.mongoDocumentTruncated {
							tree := ui.Checkbox(c, &app.mongoTreeVisible, app.localized("显示可折叠 JSON 字段树"))
							tree.Disabled(app.busy || app.dialogOpen)
							if app.mongoTreeVisible {
								app.renderMongoJSONTree(c, app.mongoEditor.Buffer.String())
							}
						}
						if app.mongoDocumentTruncated {
							ui.Text(c, app.localized("文档超过 1 MiB，已截断为只读预览，不能提交替换。"))
						}
						if !app.mongoDocumentTruncated && !app.openedReadOnly {
							preview := ui.Button(c, app.localized("预览文档 diff"))
							preview.Disabled(app.busy || app.dialogOpen)
							if preview.Clicked() {
								app.mongoShowDiff = true
							}
							commit := ui.PrimaryButton(c, app.localized("确认并提交文档…"))
							commit.Disabled(app.busy || app.dialogOpen || !app.mongoShowDiff)
							if commit.Clicked() {
								app.commitMongoDocument()
							}
						}
						if app.mongoShowDiff {
							ui.Text(c, app.localized("变更 diff")).Bold()
							ui.Text(c, app.localizedMongoDiffPreview(app.mongoOriginalJSON, app.mongoEditor.Buffer.String(), app.mongoNewDocument)).Selectable()
						}
					}
				})
			})
			return
		}
		if app.etcdStore != nil {
			app.etcdWorkspace(c)
			return
		}
		if app.keyStore != nil {
			app.redisWorkspace(c)
			return
		}
		if app.store != nil && !app.queryWorkspaceRequested {
			app.sqlConnectionOverview(c)
			return
		}
		if app.activeQuery == nil {
			if app.store != nil {
				app.sqlConnectionOverview(c)
			} else {
				app.connectionWelcome(c)
			}
			return
		}
		if app.activeQuery != nil && app.activeQuery.schemaView {
			if !app.activeQuery.schemaLoaded && !app.activeQuery.schemaLoading && app.activeQuery.schemaLoadError == "" && app.activeQuery.schemaConnectionID == app.openedConnectionID {
				app.loadSchemaWorkspaceColumns(app.activeQuery)
			}
			app.schemaWorkspace(c, app.activeQuery)
			return
		}
		if app.activeQuery != nil && app.activeQuery.dataView {
			app.dataWorkspace(c)
			return
		}
		ui.Row(c).Padding(8, 12).Gap(10).Children(func() {
			ui.Text(c, app.localized(app.activeQuery.title)).Bold().Role(ui.RoleHeading).Level(1)
			if app.store == nil {
				ui.Text(c, app.localized("未连接")).TextColor(c.Theme().TextMuted)
			} else {
				mode := ""
				if app.openedReadOnly {
					mode = " · " + app.localized("只读")
				}
				ui.Text(c, strings.ToUpper(string(app.openedDriver))+mode+" · "+app.openedLabel+" · "+app.localized(environmentLabel(app.openedEnvironment))).TextColor(c.Theme().TextMuted).SingleLine()
			}
		})
		ui.Row(c).FillWidth().Padding(4, 12).Gap(6).Children(func() {
			if app.sqlQueryTabActive() && !app.aiDraftPending {
				switch {
				case app.busy && app.cancelQuery != nil:
					cancel := iconButton(c, app.localized("取消查询"), actionIcons["stop"])
					cancel.Size(32, 32).Padding(5).TextColor(c.Theme().Danger)
					cancel.Disabled(app.dialogOpen)
					if cancel.Clicked() && app.cancelQuery != nil {
						app.cancelQuery()
					}
				case app.cursor != nil && !app.busy:
					// Result stream actions live in the results panel footer.
				default:
					executeButton := iconPrimaryButton(c, app.localized("执行 SQL"), actionIcons["play"])
					executeButton.Size(32, 32).Padding(5)
					executeButton.Disabled(app.busy || app.exporting || app.dialogOpen)
					if executeButton.Clicked() {
						app.executeSQL()
					}
				}
			}
			formatButton := iconButton(c, app.localized("格式化 SQL"), actionIcons["format"])
			formatButton.Size(32, 32).Padding(5)
			formatButton.Disabled(app.busy || app.exporting || app.dialogOpen || app.aiDraftPending)
			if formatButton.Clicked() {
				app.formatSQL()
			}
			ui.Box(c).Grow(1)
			if app.store != nil {
				if len(app.sqlNamespaces) > 0 {
					namespacePlaceholder := app.localized("选择数据库 / Schema…")
					namespaceChoice := app.activeQuery.queryNamespace
					if namespaceChoice == "" {
						namespaceChoice = app.sqlNamespaceChoice
					}
					if namespaceChoice == "" {
						namespaceChoice = app.sqlNamespace
					}
					namespaceOptions := app.sqlNamespaces
					if namespaceChoice == "" {
						namespaceChoice = namespacePlaceholder
						namespaceOptions = append([]string{namespacePlaceholder}, namespaceOptions...)
					}
					namespace := ui.Select(c, &namespaceChoice, namespaceOptions).Label(app.localized("数据库 / Schema"))
					namespace.Width(220)
					if app.openedDriver == connection.DriverPostgreSQL {
						namespace.Tooltip(app.localized("未限定表名、函数和类型会按此 schema 的 search_path 解析。选择共享 schema 前请确认其创建者可信。"))
					}
					namespace.Disabled(app.busy || app.exporting || app.dialogOpen || namespaceSelectorDisabled(app.sqlNamespace, app.sqlNamespaces))
					if namespace.Changed() {
						if namespaceChoice != namespacePlaceholder {
							app.switchSQLNamespace(namespaceChoice)
						}
					}
				} else {
					ui.Text(c, app.localized("数据库 / Schema：")+app.sqlNamespace).TextColor(c.Theme().TextMuted)
				}
			}
		})
		ui.SplitVertical(c, &app.queryEditorHeight, func() {
			app.bindSQLCompletions(app.activeQuery)
			editor.View(c, app.activeQuery.query).Label(app.localized("SQL 查询编辑器")).Grow(1).FillWidth()
		}, func() {
			ui.SplitVertical(c, &app.resultHeight, func() {
				app.resultTable(c)
			}, func() {
				ui.Column(c).Padding(10, 12).Children(func() {
					ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
						ui.Text(c, app.localized("消息")).Bold().Role(ui.RoleHeading).Level(2).Grow(1)
						if app.errorSummary == app.status && app.errorDetails != "" {
							label := "显示技术详情"
							icon := actionIcons["chevronDown"]
							if app.showErrorDetails {
								label = "隐藏技术详情"
								icon = actionIcons["chevronUp"]
							}
							details := iconButton(c, app.localized(label), icon)
							details.Size(30, 30).Padding(4).Selected(app.showErrorDetails)
							if details.Clicked() {
								app.showErrorDetails = !app.showErrorDetails
							}
						}
					})
					ui.Text(c, app.localized(app.status)).TextColor(c.Theme().TextMuted)
					if app.errorSummary == app.status && app.errorDetails != "" {
						if app.showErrorDetails {
							ui.Text(c, app.errorDetails).TextColor(c.Theme().TextMuted)
						}
					}
				})
			}).Grow(1).MinHeight(0)
		}).Grow(1).MinHeight(0)
	})
}
