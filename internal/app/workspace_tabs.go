package app

import (
	"github.com/egoist/mygo/ui"
	"github.com/magalab/dbl/internal/databaseicons"
)

func (workspace *queryWorkspace) showsTab() bool {
	return !workspace.implicitQuery
}

func (app *workbench) workspaceTabs(c *ui.Context) {
	activeID := uint64(0)
	if !app.redisOverview && !app.sqliteOverview && app.activeWorkspace >= 0 && app.activeWorkspace < len(app.workspaces) && app.workspaces[app.activeWorkspace].showsTab() {
		activeID = app.workspaces[app.activeWorkspace].id
	}
	revealActive := activeID != 0 && activeID != app.workspaceTabsLastActiveID
	if activeID != 0 {
		app.workspaceTabsLastActiveID = activeID
	}
	ui.Row(c).FillWidth().Padding(6, 10).Gap(4).AlignItems(ui.Center).Label(app.localized("工作区")).Children(func() {
		tabScroll := ui.ScrollHorizontal(c).Grow(1).MinWidth(0).Height(40).TrackScroll(&app.workspaceTabsScroll)
		tabScroll.Children(func() {
			ui.Row(c).Gap(4).Shrink(0).AlignItems(ui.Center).Children(func() {
				for index, workspace := range app.workspaces {
					if !workspace.showsTab() {
						continue
					}
					title := workspace.title
					if workspace.connectionView {
						if live, ok := app.liveConnections[workspace.connectionID]; ok {
							if workspace.redisView {
								title = app.redisDatabaseLabelForProfile(live.profile, live.profile.RedisDB)
							} else if live.profile.Name != "" {
								title = live.profile.Name
							}
						}
					}
					title = app.localized(title)
					if workspace.mysqlUserManager || workspace.postgresRoleManager {
						connectionName := ""
						if live, ok := app.liveConnections[workspace.queryConnectionID]; ok {
							connectionName = live.profile.Name
						}
						if connectionName == "" {
							for _, profile := range app.connectionProfiles {
								if profileConnectionID(profile) == workspace.queryConnectionID {
									connectionName = profile.Name
									break
								}
							}
						}
						if connectionName != "" {
							title += " · " + connectionName
						}
					}
					if workspace.dataView && (len(workspace.dataChanges) > 0 || workspace.dataCellEditing || app.dataInsertHasDraft(workspace)) {
						title += " *"
					}
					if workspace.schemaView && schemaWorkspaceDirty(workspace) {
						title += " *"
					}
					active := index == app.activeWorkspace && activeID != 0
					theme := c.Theme()
					background, foreground, border := theme.Surface, theme.Text, theme.Border
					if active {
						background, foreground, border = theme.Accent, theme.AccentText, theme.Accent
					}
					tab := ui.Row(c).Gap(0).Padding(2).Background(background).Border(1, border).Radius(theme.Radius).Shrink(0)
					tab.Children(func() {
						tabButton := ui.Button(c, "").Label(title).Role(ui.RoleTab).TextColor(foreground).Background(ui.Color{}).BorderWidth(0).Padding(4, 7).MaxWidth(190).MinWidth(56).Shrink(1)
						tabButton.Children(func() {
							var icon *ui.SVG
							iconColor := foreground
							switch {
							case workspace.connectionView:
								icon, _, _ = databaseicons.Icon(string(workspace.connectionDriver))
							case workspace.mysqlUserManager || workspace.postgresRoleManager:
								icon = treeItemIcons["users"]
								iconColor = treeItemIconColors["users"]
							case workspace.dataView:
								icon = treeItemIcons["table"]
								if workspace.dataObject.Type == "view" {
									icon = treeItemIcons["view"]
									iconColor = ui.Hex("#a855f7")
								} else {
									iconColor = treeItemIconColors["table"]
								}
							case workspace.schemaView:
								icon = treeItemIcons["schema"]
								iconColor = treeItemIconColors["schema"]
							default:
								icon = treeItemIcons["query"]
								iconColor = treeItemIconColors["query"]
							}
							if icon != nil {
								ui.Icon(c, icon).Size(14, 14).Shrink(0).TextColor(iconColor)
							}
							ui.Text(c, title).SingleLine().Ellipsis("…").MaxWidth(142).MinWidth(28).Shrink(1)
						})
						tabButton.Disabled(app.busy || app.exporting || app.dialogOpen)
						if tabButton.Clicked() {
							app.activateWorkspace(index)
						}
						closeButton := ui.Button(c, "").Label(app.localized("关闭") + " " + title).Tooltip(app.localized("关闭") + " " + title)
						closeButton.Children(func() { ui.Icon(c, actionIcons["close"]).Size(14, 14) })
						closeButton.Size(24, 24).Padding(0).Shrink(0).Background(ui.Color{}).BorderWidth(0).TextColor(foreground)
						closeButton.Disabled(app.busy || app.exporting || app.dialogOpen)
						if closeButton.Clicked() {
							app.closeWorkspace(index)
						}
					})
					if active && revealActive {
						tab.ScrollIntoView()
					}
				}
			})
		})
		newButton := iconButton(c, app.localized("新建查询"), actionIcons["add"])
		newButton.Shrink(0)
		newButton.Disabled(app.workspaceCount() >= maxWorkspaces || app.busy || app.exporting || app.dialogOpen || app.hasActiveConnection() && app.store == nil)
		if newButton.Clicked() {
			app.addWorkspace()
		}
	})
}
