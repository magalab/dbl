package app

import (
	"fmt"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/magalab/dbl/internal/connection"
	"github.com/magalab/dbl/internal/safeerror"
)

func (app *workbench) openTableStructureEditor(object connection.Object) {
	if app.store == nil || app.busy || app.exporting || app.dialogOpen || object.Type != "table" {
		return
	}
	if app.queryWorkspaceRequested && app.workspaceCount() >= maxWorkspaces {
		return
	}
	var workspace *queryWorkspace
	if !app.queryWorkspaceRequested {
		app.queryWorkspaceRequested = true
		workspace = app.workspaces[app.activeWorkspace]
		workspace.implicitQuery = false
	} else {
		workspace = newQueryWorkspace(app.nextWorkspaceID)
		app.nextWorkspaceID++
		app.workspaces = append(app.workspaces, workspace)
		app.activateWorkspace(len(app.workspaces) - 1)
	}
	workspace.title = "结构 · " + object.Name
	app.selectedObject, app.selectedType, app.selectedSQL = object.Name, object.Type, object.SQL
	workspace.schemaView = true
	workspace.schemaObject = object
	workspace.schemaConnectionID = app.openedConnectionID
	workspace.schemaLoading = false
	workspace.schemaLoaded = false
	workspace.schemaLoadError = ""
	workspace.schemaError = ""
	app.errorSummary, app.errorDetails, app.showErrorDetails = "", "", false
	workspace.schemaBaseline = nil
	workspace.schemaColumns = nil
	workspace.schemaPreviewTab = false
	workspace.schemaApplyPending = false
	workspace.schemaCloseAfterApply = false
	workspace.columns = nil
	workspace.resultSQL = ""
	if app.activeQuery == workspace {
		app.loadSchemaWorkspaceColumns(workspace)
	}
}

func (app *workbench) loadSchemaWorkspaceColumns(workspace *queryWorkspace) {
	if workspace == nil || !workspace.schemaView || workspace.schemaLoading || workspace.schemaLoaded || app.store == nil || workspace.schemaConnectionID != app.openedConnectionID {
		return
	}
	store, connectionID, object := app.store, app.openedConnectionID, workspace.schemaObject
	workspace.schemaLoading = true
	workspace.schemaLoadError = ""
	go func() {
		ctx, cancel := app.withTaskTimeout(10 * time.Second)
		defer cancel()
		columns, err := store.Columns(ctx, object.Schema, object.Name)
		app.window.Update(func() {
			workspace.schemaLoading = false
			if app.store != store || app.openedConnectionID != connectionID || workspace.schemaConnectionID != connectionID {
				return
			}
			if err != nil {
				workspace.schemaLoadError = safeerror.Display(err)
				return
			}
			workspace.schemaBaseline = append([]connection.Column(nil), columns...)
			workspace.schemaLoaded = true
			workspace.schemaColumns = make([]schemaColumnDraft, 0, len(columns))
			for _, column := range columns {
				defaultValue := ""
				defaultSet := column.Default != nil
				if defaultSet {
					defaultValue = fmt.Sprint(column.Default)
				}
				workspace.schemaColumns = append(workspace.schemaColumns, schemaColumnDraft{
					OriginalName: column.Name,
					Name:         column.Name,
					Type:         column.Type,
					NotNull:      column.NotNull,
					Default:      defaultValue,
					DefaultSet:   defaultSet,
					PrimaryKey:   column.PrimaryKey,
					Hidden:       column.Hidden != 0,
				})
			}
			workspace.schemaLoadError = ""
			app.status = app.localized("表结构已载入，可在字段页编辑并预览 SQL。")
		})
	}()
}

func (app *workbench) schemaWorkspace(c *ui.Context, workspace *queryWorkspace) {
	object := workspace.schemaObject
	ui.Row(c).FillWidth().Padding(10, 14).Gap(10).AlignItems(ui.Center).Children(func() {
		if icon := treeItemIcons["table"]; icon != nil {
			ui.Icon(c, icon).Size(18, 18)
		}
		ui.Text(c, app.localized("表结构")).Bold().Role(ui.RoleHeading).Level(1)
		breadcrumb := app.schemaConnectionLabel(workspace) + " / " + object.Name
		if object.Schema != "" {
			breadcrumb = app.schemaConnectionLabel(workspace) + " / " + object.Schema + " / " + object.Name
		}
		ui.Text(c, breadcrumb).TextColor(c.Theme().TextMuted).SingleLine()
		ui.Spacer(c)
		refresh := ui.Button(c, app.localized("刷新结构"))
		refresh.Disabled(app.busy || app.exporting || app.dialogOpen || workspace.schemaConnectionID != app.openedConnectionID)
		if refresh.Clicked() {
			if schemaWorkspaceDirty(workspace) {
				app.confirmReloadSchemaWorkspace(workspace)
			} else {
				app.reloadSchemaWorkspace(workspace)
			}
		}
	})
	ui.Row(c).FillWidth().Padding(4, 12).Gap(8).Children(func() {
		fields := ui.Button(c, app.localized("字段"))
		fields.Selected(!workspace.schemaPreviewTab)
		if fields.Clicked() {
			workspace.schemaPreviewTab = false
		}
		ddl := ui.Button(c, app.localized("SQL 预览"))
		ddl.Selected(workspace.schemaPreviewTab)
		if ddl.Clicked() {
			workspace.schemaPreviewTab = true
		}
		ui.Spacer(c)
		if !workspace.schemaPreviewTab {
			add := ui.PrimaryButton(c, app.localized("新增字段"))
			add.Disabled(app.busy || app.dialogOpen || app.exporting || workspace.schemaLoading || workspace.schemaConnectionID != app.openedConnectionID || app.openedReadOnly)
			if add.Clicked() {
				app.addSchemaColumn(workspace)
			}
		}
		apply := ui.PrimaryButton(c, app.localized("应用结构变更"))
		apply.Disabled(app.busy || app.dialogOpen || app.exporting || app.openedReadOnly || workspace.schemaConnectionID != app.openedConnectionID || workspace.schemaLoading || !schemaWorkspaceDirty(workspace) || workspace.schemaLoadError != "")
		if apply.Clicked() {
			app.applySchemaWorkspaceChanges(workspace)
		}
	})
	if workspace.schemaConnectionID != app.openedConnectionID {
		ui.Text(c, app.localized("此结构编辑器属于另一个连接。切换回来源连接后才能刷新或应用。 ")).TextColor(c.Theme().Warning).Padding(8, 14)
	}
	if workspace.schemaError != "" {
		if _, err := app.schemaChangesSQL(workspace); err == nil {
			workspace.schemaError = ""
		} else {
			ui.Text(c, app.localized("无法生成 SQL：")+workspace.schemaError).TextColor(c.Theme().Danger).Padding(8, 14)
		}
	}
	if app.errorSummary != "" {
		ui.Text(c, app.localized(app.errorSummary)).TextColor(c.Theme().Danger).Padding(8, 14)
		if app.errorDetails != "" {
			ui.Text(c, app.errorDetails).TextColor(c.Theme().TextMuted).Selectable().Padding(4, 14)
		}
	}
	if workspace.schemaLoading {
		ui.Text(c, app.localized("正在读取表字段…")).TextColor(c.Theme().TextMuted).Padding(8, 14)
		return
	}
	if workspace.schemaLoadError != "" {
		ui.Text(c, app.localized("读取表字段失败：")+workspace.schemaLoadError).TextColor(c.Theme().Danger).Padding(8, 14)
		return
	}
	if workspace.schemaPreviewTab {
		app.schemaSQLPreview(c, workspace)
		return
	}
	app.schemaFieldGrid(c, workspace)
}

func (app *workbench) schemaFieldGrid(c *ui.Context, workspace *queryWorkspace) {
	driver := app.openedDriver
	ui.Column(c).Fill().Padding(12).Gap(8).Children(func() {
		ui.Text(c, app.localized("字段变更会先生成 SQL 预览；执行前还会显示目标连接和完整语句供确认。主键定义保留现状。 ")).TextColor(c.Theme().TextMuted)
		if driver == connection.DriverMySQL {
			ui.Text(c, app.localized("MySQL 会逐条执行 DDL，若中途失败，先刷新结构再继续。 ")).TextColor(c.Theme().Warning)
		}
		ui.Row(c).FillWidth().Gap(8).Padding(4, 8).Children(func() {
			ui.Text(c, app.localized("字段名")).Bold().Width(180)
			ui.Text(c, app.localized("数据类型")).Bold().Grow(1)
			ui.Text(c, app.localized("非空")).Bold().Width(78)
			ui.Text(c, app.localized("默认值（SQL）")).Bold().Grow(1)
			ui.Text(c, app.localized("约束")).Bold().Width(110)
			ui.Text(c, "").Width(76)
		})
		ui.Scroll(c).Fill().Children(func() {
			ui.Column(c).FillWidth().Gap(4).Children(func() {
				visible := 0
				for index := range workspace.schemaColumns {
					column := &workspace.schemaColumns[index]
					visible++
					index := index
					background := c.Theme().Surface
					if column.Dropped {
						background = c.Theme().SurfaceHover
					}
					ui.Row(c).FillWidth().Gap(8).Padding(4, 6).AlignItems(ui.Center).Background(background).Border(1, c.Theme().Border).Radius(c.Theme().Radius).Children(func() {
						name := ui.TextInput(c, &column.Name).Placeholder(app.localized("字段名")).Label(app.localized("字段名")).Width(180)
						name.Disabled(app.busy || app.dialogOpen || app.openedReadOnly || workspace.schemaConnectionID != app.openedConnectionID || column.Hidden || column.Dropped)
						typeInput := ui.TextInput(c, &column.Type).Placeholder("TEXT").Label(app.localized("数据类型")).Grow(1)
						typeInput.Disabled(app.busy || app.dialogOpen || app.openedReadOnly || workspace.schemaConnectionID != app.openedConnectionID || column.Hidden || column.Dropped || (!column.IsNew && driver == connection.DriverSQLite))
						notNull := ui.Checkbox(c, &column.NotNull, app.localized("非空"))
						notNull.Disabled(app.busy || app.dialogOpen || app.openedReadOnly || workspace.schemaConnectionID != app.openedConnectionID || column.Hidden || column.Dropped || column.PrimaryKey || (!column.IsNew && driver == connection.DriverSQLite))
						defaultRow := ui.Row(c).Gap(4).AlignItems(ui.Center)
						defaultRow.Children(func() {
							setDefault := ui.Checkbox(c, &column.DefaultSet, app.localized("启用"))
							setDefault.Disabled(app.busy || app.dialogOpen || app.openedReadOnly || workspace.schemaConnectionID != app.openedConnectionID || column.Hidden || column.Dropped || (!column.IsNew && driver == connection.DriverSQLite))
							defaultInput := ui.TextInput(c, &column.Default).Placeholder(app.localized("例如 CURRENT_TIMESTAMP")).Label(app.localized("默认表达式")).Grow(1)
							defaultInput.Disabled(app.busy || app.dialogOpen || app.openedReadOnly || workspace.schemaConnectionID != app.openedConnectionID || column.Hidden || column.Dropped || !column.DefaultSet || (!column.IsNew && driver == connection.DriverSQLite))
						})
						defaultRow.Grow(1)
						constraint := ""
						if column.PrimaryKey {
							constraint = app.localized("主键")
						}
						if column.Hidden {
							constraint = app.localized("生成 / 自增")
						}
						ui.Text(c, constraint).TextColor(c.Theme().TextMuted).Width(110).SingleLine()
						removeLabel := app.localized("删除字段")
						if column.Dropped {
							removeLabel = app.localized("撤销删除")
						}
						remove := ui.Button(c, removeLabel)
						remove.Width(76).Disabled(app.busy || app.dialogOpen || app.openedReadOnly || workspace.schemaConnectionID != app.openedConnectionID || column.Hidden || (column.PrimaryKey && !column.IsNew))
						if remove.Clicked() {
							if column.IsNew {
								workspace.schemaColumns = append(workspace.schemaColumns[:index], workspace.schemaColumns[index+1:]...)
							} else {
								column.Dropped = !column.Dropped
							}
						}
					})
				}
				if visible == 0 {
					ui.Text(c, app.localized("此表目前没有可编辑字段。 ")).TextColor(c.Theme().TextMuted).Padding(12, 8)
				}
			})
		})
	})
}

func (app *workbench) schemaSQLPreview(c *ui.Context, workspace *queryWorkspace) {
	statement, err := app.schemaChangesSQL(workspace)
	ui.Column(c).Fill().Padding(14).Gap(10).Children(func() {
		ui.Text(c, app.localized("即将应用的 DDL")).Bold().Role(ui.RoleHeading).Level(2)
		if err != nil {
			ui.Text(c, app.localized("无法生成 SQL：")+err.Error()).TextColor(c.Theme().Danger)
			return
		}
		if statement == "" {
			ui.Text(c, app.localized("当前没有结构变更。 ")).TextColor(c.Theme().TextMuted)
			return
		}
		ui.Text(c, statement).Selectable().FillWidth()
		ui.Row(c).Gap(8).Children(func() {
			copyButton := ui.Button(c, app.localized("复制 SQL"))
			if copyButton.Clicked() {
				mygo.Clipboard.WriteText(statement)
				app.status = app.localized("结构 SQL 已复制。")
			}
			ui.Text(c, app.localized("检查影响后点击‘应用结构变更’；应用动作会再次要求确认。 ")).TextColor(c.Theme().TextMuted)
		})
	})
}

func (app *workbench) addSchemaColumn(workspace *queryWorkspace) {
	if workspace == nil {
		return
	}
	used := make(map[string]bool, len(workspace.schemaColumns))
	for _, column := range workspace.schemaColumns {
		if !column.Dropped {
			used[strings.ToLower(column.Name)] = true
		}
	}
	name := "column"
	for suffix := 1; used[strings.ToLower(name)]; suffix++ {
		name = fmt.Sprintf("column_%d", suffix)
	}
	workspace.schemaColumns = append(workspace.schemaColumns, schemaColumnDraft{Name: name, Type: "TEXT", IsNew: true})
}
