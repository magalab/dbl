package app

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/magalab/dbl/internal/connection"
	"github.com/magalab/dbl/internal/databaseicons"
	"github.com/magalab/dbl/internal/preferences"
	"github.com/magalab/dbl/internal/safeerror"
)

func (app *workbench) connectionTree(c *ui.Context) {
	filter := strings.ToLower(strings.TrimSpace(app.connectionSearch))
	profiles := app.connectionProfiles
	activeProfileShown := false

	for _, profile := range profiles {
		connected := app.profileIsActive(profile)
		if app.activeConnectionsOnly && !connected {
			continue
		}
		searchText := strings.ToLower(strings.Join([]string{profile.Name, profile.Driver, profile.Group, profile.Environment, profile.Host, profile.Database, profile.Path}, " "))
		if filter != "" && !strings.Contains(searchText, filter) {
			continue
		}
		selected := app.profileIsSelected(profile)
		activeProfileShown = activeProfileShown || selected
		open := app.connectionTreeState("profile:"+profile.ID, selected)
		profile := profile
		item := ui.TreeItem(c, app.profileTreeLabel(profile), open, func() {
			app.profileConnectionTreeResources(c, profile, 0)
		}).Tooltip(profile.Name).ContextMenu(func(menu *ui.Menu) {
			app.connectionContextMenu(menu, profile, connected)
		})
		item = connectionTreeDecorations(c, item, string(profile.Driver), "", 0, profile.Favorite, connected)
		theme := c.Theme()
		if connected || app.treeConnectionErrors[profile.ID] != "" {
			item.PaddingEnd(theme.Space(8))
		}
		if item.Hovered() || item.FocusWithin() {
			item.Children(func() {
				ui.Spacer(c)
				app.connectionRowActions(c, profile, connected)
			})
		}
		if message := app.treeConnectionErrors[profile.ID]; message != "" {
			item = item.Tooltip(profile.Name + " · " + message).DrawOver(func(p *ui.Painter, r ui.Rect) {
				size := c.Theme().Space(4)
				p.Icon(actionIcons["warning"], ui.Rect{X: r.X + r.W - c.Theme().Space(3) - size, Y: r.Y + (r.H-size)/2, W: size, H: size}, c.Theme().Warning)
			})
		}
		if item.Clicked() && (!selected || profile.Driver == preferences.DriverRedis || profile.Driver == preferences.DriverEtcd) {
			app.openSavedProfile(profile)
		}
		if *open && profile.Driver != preferences.DriverEtcd {
			app.ensureProfileTreeSnapshot(profile)
		}
	}

	if app.hasActiveConnection() && !activeProfileShown {
		label := app.activeConnectionTreeLabel()
		if filter == "" || strings.Contains(strings.ToLower(label), filter) {
			open := app.connectionTreeState("current", true)
			item := ui.TreeItem(c, label, open, func() { app.connectionTreeResources(c, 0) }).ContextMenu(func(menu *ui.Menu) {
				app.activeConnectionRootContextMenu(menu)
			})
			item = connectionTreeDecorations(c, item, string(app.openedDriver), "", 0, false, true)
			item.Clicked()
		}
	}

	if !app.hasActiveConnection() && len(profiles) == 0 {
		ui.Text(c, app.localized("尚无连接配置")).TextColor(c.Theme().TextMuted)
	}
}

func (app *workbench) connectionRowActions(c *ui.Context, profile preferences.ConnectionProfile, connected bool) {
	theme := c.Theme()
	action := func(label, icon string, disabled bool) ui.Element {
		button := iconButton(c, app.localized(label), actionIcons[icon]).Size(22, 22).Padding(3).
			BorderWidth(0).Background(ui.Transparent).Disabled(disabled)
		if button.Hovered() {
			button.Background(theme.SurfaceHover)
		}
		return button
	}
	busy := app.busy || app.exporting || app.dialogOpen
	if connected {
		if app.canOpenProfileQuery(profile, connected) && action("新建查询", "add", false).Clicked() {
			app.openProfileQuery(profile, "")
		}
		if action("刷新", "refresh", busy).Clicked() {
			app.refreshProfileConnection(profile)
		}
	}
	if action("实例信息", "info", app.dialogOpen).Clicked() {
		app.showInstanceInfo(profile)
	}
	if connected && action("关闭连接", "unplug", busy).Clicked() {
		app.closeProfileConnection(profile)
	}
}

func (app *workbench) profileConnectionTreeResources(c *ui.Context, profile preferences.ConnectionProfile, depth int) {
	if app.profileIsSelected(profile) {
		app.connectionTreeResources(c, depth)
		return
	}
	id := app.profileLiveSessionID(profile)
	live, ok := app.liveConnections[id]
	if !ok || live.connection == nil {
		if app.treeConnectionLoading[profile.ID] {
			app.connectionTreeLoadingStatus(c, "正在连接…")
		} else if app.treeConnectionErrors[profile.ID] != "" {
			ui.Text(c, app.localized("连接失败：")+app.treeConnectionErrors[profile.ID]).TextColor(c.Theme().Danger)
		} else {
			ui.Text(c, app.localized("连接后显示对象")).TextColor(c.Theme().TextMuted)
		}
		return
	}
	if live.connection.Driver() == connection.DriverEtcd {
		app.etcdNavigation(c, depth+1, id, profile)
		return
	}
	if !live.treeLoaded {
		if message := app.treeConnectionErrors[profile.ID]; message != "" {
			ui.Text(c, app.localized("加载对象失败：")+message).TextColor(c.Theme().Danger)
		} else {
			app.connectionTreeLoadingStatus(c, "正在加载对象…")
		}
		return
	}
	switch live.connection.Driver() {
	case connection.DriverPostgreSQL, connection.DriverMySQL, connection.DriverSQLite:
		app.profileSQLTree(c, profile, id, live.tree, depth)
	case connection.DriverMongoDB:
		for _, database := range live.tree.mongoDatabases {
			database := database
			item := ui.TreeItem(c, treeIconPlaceholder+database, nil, nil).Tooltip(database)
			item = connectionTreeDecorations(c, item, "", "database", depth+1, false, false)
			item.Disabled(app.busy || app.exporting || app.dialogOpen)
			if item.Clicked() {
				if !app.activateLiveConnectionForWorkspace(id) {
					return
				}
				app.activateConnectionWorkspace(id)
				if app.openedConnectionID == id && app.documentStore != nil {
					app.selectMongoDatabase(database)
				}
			}
		}
	case connection.DriverRedis:
		for database := 0; database < 16; database++ {
			database := database
			item := ui.TreeItem(c, treeIconPlaceholder+app.redisDatabaseLabelForProfile(profile, database), nil, nil).
				ContextMenu(func(menu *ui.Menu) {
					if app.profileIsSelected(profile) {
						app.redisDatabaseContextMenu(menu, database)
						return
					}
					if menu.Item("⇄  " + app.localized("切换到此 DB")).Disabled(app.busy || app.dialogOpen).Chosen() {
						app.openRedisDatabaseForProfile(profile, database)
					}
				})
			item = connectionTreeDecorations(c, item, "", "database", depth+1, false, false)
			if item.Clicked() {
				app.openRedisDatabaseForProfile(profile, database)
			}
		}
	default:
		ui.Text(c, app.localized("连接后显示对象")).TextColor(c.Theme().TextMuted)
	}
}

func (app *workbench) connectionTreeLoadingStatus(c *ui.Context, label string) {
	label = app.localized(label)
	ui.Row(c).Gap(6).AlignItems(ui.Center).Tooltip(label).Children(func() {
		ui.Spinner(c).Size(14, 14).Label(label)
		ui.Text(c, label).TextColor(c.Theme().TextMuted)
	})
}

func (app *workbench) profileSQLTree(c *ui.Context, profile preferences.ConnectionProfile, id string, snapshot connectionTreeSnapshot, depth int) {
	database := profile.Database
	if database == "" {
		database = snapshot.database
	}
	if profile.Driver == preferences.DriverPostgreSQL && database != "" {
		item := ui.TreeItem(c, treeIconPlaceholder+database, app.connectionTreeState("pg-db:"+id, false), func() {
			app.profileSQLNamespaces(c, profile, id, snapshot, depth+2)
		}).Selected(false)
		item = connectionTreeDecorations(c, item, "", "database", depth+1, false, false)
		item.Clicked()
		return
	}
	app.profileSQLNamespaces(c, profile, id, snapshot, depth+1)
}

func (app *workbench) profileSQLNamespaces(c *ui.Context, profile preferences.ConnectionProfile, id string, snapshot connectionTreeSnapshot, depth int) {
	for _, namespace := range snapshot.namespaces {
		namespace := namespace
		open := app.connectionTreeState("namespace:"+id+":"+namespace, false)
		item := ui.TreeItem(c, treeIconPlaceholder+namespace, open, func() {
			if profile.Driver == preferences.DriverPostgreSQL {
				app.postgresSchemaObjectTree(c, id, namespace, snapshot.objects, depth+1)
				return
			}
			if profile.Driver == preferences.DriverMySQL {
				app.mysqlObjectTree(c, id, namespace, snapshot.objects, depth+1)
				return
			}
			if profile.Driver == preferences.DriverSQLite {
				app.sqliteObjectTree(c, id, namespace, snapshot.objects, depth+1)
				return
			}
			for _, object := range snapshot.objects {
				if !sqlObjectMatchesNamespace(string(profile.Driver), namespace, object) {
					continue
				}
				object := object
				app.sqlObjectTreeItem(c, id, object, depth+1)
			}
		}).ContextMenu(func(menu *ui.Menu) {
			app.schemaContextMenu(menu, id, connection.Driver(profile.Driver), namespace)
		})
		item = connectionTreeDecorations(c, item, "", "schema", depth, false, false)
		if item.Clicked() {
			*open = true
			if app.activateLiveConnectionForWorkspace(id) {
				app.openConnectionView(id)
			}
			if app.store != nil && namespace != app.sqlNamespace {
				app.switchSQLNamespace(namespace)
			}
		}
	}
	switch profile.Driver {
	case preferences.DriverPostgreSQL:
		app.postgresGlobalObjectTree(c, id, snapshot.namespace, snapshot.objects, depth)
		app.sqlUsersTreeItem(c, id, connection.DriverPostgreSQL, snapshot.namespace, depth)
	case preferences.DriverMySQL:
		app.sqlUsersTreeItem(c, id, connection.DriverMySQL, snapshot.namespace, depth)
	}
	if snapshot.objectsError != "" {
		ui.Text(c, app.localized("加载数据库对象失败：")+snapshot.objectsError).PaddingStart(18).TextColor(c.Theme().Warning)
	}
	if snapshot.objectsHasMore {
		if live, ok := app.liveConnections[id]; ok && live.treeLoading {
			app.connectionTreeLoadingStatus(c, "正在加载更多数据库对象…")
		} else {
			item := ui.TreeItem(c, treeIconPlaceholder+app.localized("加载更多数据库对象"), nil, nil)
			item = connectionTreeDecorations(c, item, "", "", depth, false, false)
			item.Disabled(app.busy || app.exporting || app.dialogOpen)
			if item.Clicked() {
				app.loadMoreProfileSQLObjects(id)
			}
		}
	}
}

func (app *workbench) redisDatabaseLabelForProfile(profile preferences.ConnectionProfile, database int) string {
	if alias := strings.TrimSpace(profile.RedisDatabaseAliases[database]); alias != "" {
		return alias
	}
	return fmt.Sprintf("db%d", database)
}

func (app *workbench) ensureProfileTreeSnapshot(profile preferences.ConnectionProfile) {
	id := app.profileLiveSessionID(profile)
	live, ok := app.liveConnections[id]
	if !ok || live.connection == nil {
		app.ensureProfileTreeConnection(profile)
		return
	}
	if live.treeLoaded || live.treeLoading {
		return
	}
	live.treeLoading = true
	app.liveConnections[id] = live
	conn := live.connection
	ctx, cancel := app.withTaskTimeout(10 * time.Second)
	go func() {
		defer cancel()
		snapshot := connectionTreeSnapshot{}
		var err error
		switch store := conn.(type) {
		case connection.SQL:
			page, pageErr := store.Objects(ctx, "", "", "", sqliteObjectPageSize)
			err = pageErr
			snapshot.objects = page.Objects
			snapshot.objectCursorSchema = page.NextSchema
			snapshot.objectCursorType = page.NextType
			snapshot.objectCursorName = page.NextName
			snapshot.objectsHasMore = page.HasMore
			if err == nil {
				snapshot.namespaces, _ = store.Namespaces(ctx)
				snapshot.namespace, _ = store.CurrentNamespace(ctx)
				if database, ok := store.(connection.CurrentDatabase); ok {
					snapshot.database, _ = database.CurrentDatabase(ctx)
				}
			}
		case connection.Documents:
			snapshot.mongoDatabases, err = store.Databases(ctx)
		case connection.KeyValue:
			// Redis databases are listed in the tree; keys belong in the workspace pane.
		case connection.Configuration:
			// etcd resources are navigated in their dedicated workspace.
		}
		app.window.Update(func() {
			live, exists := app.liveConnections[id]
			if !exists || live.connection != conn {
				return
			}
			live.treeLoading = false
			if err == nil {
				live.tree, live.treeLoaded = snapshot, true
				delete(app.treeConnectionErrors, profile.ID)
			} else {
				if app.treeConnectionErrors == nil {
					app.treeConnectionErrors = make(map[string]string)
				}
				app.treeConnectionErrors[profile.ID] = safeerror.Display(err)
				app.reportError("连接失败", err)
			}
			app.liveConnections[id] = live
		})
	}()
}

func (app *workbench) loadMoreProfileSQLObjects(connectionID string) {
	if app.busy || app.exporting || app.dialogOpen {
		return
	}
	live, ok := app.liveConnections[connectionID]
	if !ok || live.treeLoading || !live.tree.objectsHasMore {
		return
	}
	store, ok := live.connection.(connection.SQL)
	if !ok {
		return
	}
	afterSchema := live.tree.objectCursorSchema
	afterType := live.tree.objectCursorType
	afterName := live.tree.objectCursorName
	live.treeLoading = true
	live.tree.objectsError = ""
	app.liveConnections[connectionID] = live
	ctx, cancel := app.withTaskTimeout(5 * time.Second)
	go func() {
		defer cancel()
		page, err := store.Objects(ctx, afterSchema, afterType, afterName, sqliteObjectPageSize)
		app.window.Update(func() {
			current, exists := app.liveConnections[connectionID]
			if !exists || current.connection != live.connection {
				return
			}
			current.treeLoading = false
			if err != nil {
				current.tree.objectsError = safeerror.Display(err)
			} else {
				current.tree.objects = append(current.tree.objects, page.Objects...)
				current.tree.objectCursorSchema = page.NextSchema
				current.tree.objectCursorType = page.NextType
				current.tree.objectCursorName = page.NextName
				current.tree.objectsHasMore = page.HasMore && len(page.Objects) > 0
				current.tree.objectsError = ""
			}
			app.liveConnections[connectionID] = current
		})
	}()
}

func (app *workbench) ensureProfileTreeConnection(profile preferences.ConnectionProfile) {
	if app.busy || app.exporting || app.dialogOpen {
		return
	}
	if app.treeConnectionErrors[profile.ID] != "" {
		return
	}
	if app.treeConnectionLoading[profile.ID] {
		return
	}
	if app.treeConnectionLoading == nil {
		app.treeConnectionLoading = make(map[string]bool)
	}
	if app.treeConnectionErrors != nil {
		delete(app.treeConnectionErrors, profile.ID)
	}
	app.treeConnectionLoading[profile.ID] = true
	ctx, cancel := app.withTaskTimeout(25 * time.Second)
	go func() {
		defer cancel()
		conn, err := app.connectProfileForTree(ctx, profile)
		if err != nil && conn != nil {
			_ = conn.Close()
			conn = nil
		}
		app.window.Update(func() {
			delete(app.treeConnectionLoading, profile.ID)
			if err != nil {
				if app.treeConnectionErrors == nil {
					app.treeConnectionErrors = make(map[string]string)
				}
				app.treeConnectionErrors[profile.ID] = safeerror.Display(err)
				app.reportError("连接失败", err)
				return
			}
			delete(app.treeConnectionErrors, profile.ID)
			app.registerLiveConnection(profileConnectionID(profile), conn, profile)
		})
	}()
}

func (app *workbench) connectionTreeState(key string, initial bool) *bool {
	if app.connectionTreeExpanded == nil {
		app.connectionTreeExpanded = make(map[string]*bool)
	}
	if state := app.connectionTreeExpanded[key]; state != nil {
		return state
	}
	state := new(bool)
	*state = initial
	app.connectionTreeExpanded[key] = state
	return state
}

func (app *workbench) collapseConnectionTree(profileID, connectionID string) {
	if app.connectionTreeExpanded == nil {
		app.connectionTreeExpanded = make(map[string]*bool)
	}
	rootKey := "current"
	if profileID != "" {
		rootKey = "profile:" + profileID
	}
	if app.connectionTreeExpanded[rootKey] == nil {
		app.connectionTreeExpanded[rootKey] = new(bool)
	}
	*app.connectionTreeExpanded[rootKey] = false
	for key, expanded := range app.connectionTreeExpanded {
		collapse := key == rootKey
		if profileID != "" && (key == "profile:"+profileID || strings.Contains(key, profileID)) {
			collapse = true
		}
		if connectionID != "" && strings.Contains(key, connectionID) {
			collapse = true
		}
		if collapse && expanded != nil {
			*expanded = false
		}
	}
}

func (app *workbench) collapseActiveConnectionTree(connectionID string) {
	profileID := ""
	for _, profile := range app.connectionProfiles {
		if app.profileIsSelected(profile) {
			profileID = profile.ID
			break
		}
	}
	app.collapseConnectionTree(profileID, connectionID)
}

func (app *workbench) activeSavedProfile() (preferences.ConnectionProfile, bool) {
	for _, profile := range app.connectionProfiles {
		if app.profileIsSelected(profile) {
			return profile, true
		}
	}
	return preferences.ConnectionProfile{}, false
}

func (app *workbench) profileIsActive(profile preferences.ConnectionProfile) bool {
	if app.liveConnections != nil {
		if profile.Driver == preferences.DriverRedis {
			prefix := "redis:" + profile.ID + ":db:"
			for id := range app.liveConnections {
				if strings.HasPrefix(id, prefix) {
					return true
				}
			}
		}
		if _, connected := app.liveConnections[profileConnectionID(profile)]; connected {
			return true
		}
	}
	if app.openedDriver != connection.DriverSQLite && app.openedConnectionID == string(profile.Driver)+":"+profile.ID {
		return true
	}
	if profile.Driver != preferences.DriverSQLite || app.openedDriver != connection.DriverSQLite || profile.Path != app.openedPath {
		return false
	}
	return app.selectedProfileID == "" || app.selectedProfileID == profile.ID
}

func (app *workbench) profileIsSelected(profile preferences.ConnectionProfile) bool {
	if profile.Driver == preferences.DriverRedis {
		return strings.HasPrefix(app.openedConnectionID, "redis:"+profile.ID+":db:")
	}
	return app.openedConnectionID == profileConnectionID(profile)
}

func (app *workbench) profileLiveSessionID(profile preferences.ConnectionProfile) string {
	if profile.Driver != preferences.DriverRedis {
		return profileConnectionID(profile)
	}
	prefix := "redis:" + profile.ID + ":db:"
	if strings.HasPrefix(app.openedConnectionID, prefix) {
		return app.openedConnectionID
	}
	for id := range app.liveConnections {
		if strings.HasPrefix(id, prefix) {
			return id
		}
	}
	return profileConnectionID(profile)
}

func profileConnectionID(profile preferences.ConnectionProfile) string {
	if profile.Driver == preferences.DriverRedis {
		return redisDatabaseConnectionID(profile, profile.RedisDB)
	}
	if profile.Driver == preferences.DriverSQLite {
		return "sqlite:" + profile.Path
	}
	return profile.Driver + ":" + profile.ID
}

func redisDatabaseConnectionID(profile preferences.ConnectionProfile, database int) string {
	return fmt.Sprintf("redis:%s:db:%d", profile.ID, database)
}

func (app *workbench) profileTreeLabel(profile preferences.ConnectionProfile) string {
	label := treeIconPlaceholder + profile.Name
	if profile.Favorite {
		label = "★ " + label
	}
	return label
}

func (app *workbench) activeConnectionTreeLabel() string {
	name := app.openedLabel
	if app.openedDriver == connection.DriverSQLite {
		name = filepath.Base(app.openedPath)
	}
	if name == "" {
		name = strings.ToUpper(string(app.openedDriver))
	}
	return treeIconPlaceholder + name
}

const treeIconPlaceholder = "\u2002\u2002\u2002\u2002"

func connectionTreeDecorations(c *ui.Context, item ui.Element, driver, iconName string, depth int, favorite, active bool) ui.Element {
	theme := c.Theme()
	icon, color, hasIcon := databaseicons.Icon(driver)
	if iconName != "" {
		icon, hasIcon = treeItemIcons[iconName]
		color = treeItemIconColors[iconName]
	}
	iconSize := theme.Space(4)
	iconOffset := theme.Space(6.5 + 4*float32(depth))
	if favorite {
		iconOffset += theme.Space(4)
	}
	dotSize := theme.Space(1.5)
	return item.DrawOver(func(p *ui.Painter, r ui.Rect) {
		if item.FocusVisible() {
			p.FocusRing(r, [4]float32{theme.Radius, theme.Radius, theme.Radius, theme.Radius})
		}
		if hasIcon {
			p.Icon(icon, ui.Rect{
				X: r.X + iconOffset,
				Y: r.Y + (r.H-iconSize)/2,
				W: iconSize,
				H: iconSize,
			}, color)
		}
		if active {
			p.Fill(ui.Rect{
				X: r.X + r.W - theme.Space(3) - dotSize,
				Y: r.Y + (r.H-dotSize)/2,
				W: dotSize,
				H: dotSize,
			}, theme.Success, dotSize/2)
		}
	})
}
