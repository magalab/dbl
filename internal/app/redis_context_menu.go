package app

import (
	"fmt"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/magalab/dbl/internal/connection"
)

func (app *workbench) redisDatabaseContextMenu(menu *ui.Menu, database int) {
	current := database == app.currentRedisDB()
	if !current && menu.Item("⇄  "+app.localized("切换到此 DB")).Disabled(app.busy || app.dialogOpen).Chosen() {
		app.switchRedisDatabase(database)
	}
	if current && menu.Item("⌕  "+app.localized("扫描键")).Disabled(app.busy || app.dialogOpen).Chosen() {
		app.scanRedis(true)
	}
	if current && menu.Item("▣  "+app.localized("设为默认数据库")).Disabled(app.busy || app.dialogOpen || app.openedReadOnly).Chosen() {
		app.saveRedisDefaultDatabase(database)
	}
	if current && menu.Item("✎  "+app.localized("编辑数据库别名")).Disabled(app.busy || app.dialogOpen).Chosen() {
		profile, ok := app.activeSavedProfile()
		if ok {
			app.redisAliasDB = database
			app.redisAliasDraft = profile.RedisDatabaseAliases[database]
			app.showRedisAliasEditor = true
		}
	}
	menu.Separator()
	if menu.Item("⌫  " + app.localized("清空当前 DB…")).Disabled(!current || app.busy || app.dialogOpen || app.openedReadOnly || app.keyStore == nil).Chosen() {
		app.confirmClearRedisDatabase()
	}
}

func (app *workbench) saveRedisDatabaseAlias() {
	profile, ok := app.activeSavedProfile()
	if !ok || app.preferences == nil || app.busy || app.redisAliasDB < 0 {
		return
	}
	aliases := make(map[int]string, len(profile.RedisDatabaseAliases)+1)
	for database, alias := range profile.RedisDatabaseAliases {
		aliases[database] = alias
	}
	if alias := strings.TrimSpace(app.redisAliasDraft); alias != "" {
		aliases[app.redisAliasDB] = alias
	} else {
		delete(aliases, app.redisAliasDB)
	}
	profile.RedisDatabaseAliases = aliases
	app.preferenceRevision++
	revision, store := app.preferenceRevision, app.preferences
	app.busy = true
	go func() {
		saved, err := store.SaveConnectionProfile(profile, revision)
		settings := store.Settings()
		app.window.Update(func() {
			app.busy = false
			if err != nil {
				app.reportError("保存 Redis 数据库别名失败", err)
				return
			}
			app.connectionProfiles = settings.ConnectionProfiles
			for id, live := range app.liveConnections {
				if live.profile.ID == saved.ID {
					live.profile = saved
					app.liveConnections[id] = live
				}
			}
			app.showRedisAliasEditor = false
			app.status = app.localized("Redis 数据库别名已保存。")
		})
	}()
}

func cloneRedisDatabaseAliases(aliases map[int]string) map[int]string {
	if len(aliases) == 0 {
		return nil
	}
	copy := make(map[int]string, len(aliases))
	for database, alias := range aliases {
		copy[database] = alias
	}
	return copy
}

func (app *workbench) switchRedisDatabase(database int) {
	if app.busy || app.dialogOpen || app.openedDriver != connection.DriverRedis {
		return
	}
	live, ok := app.liveConnections[app.openedConnectionID]
	if !ok {
		return
	}
	profile := live.profile
	profile.RedisDB = database
	connectionID := redisDatabaseConnectionID(profile, database)
	reusedOverview := app.redisOverview && app.openedConnectionID == connectionID
	if !app.activateLiveConnectionForWorkspace(connectionID) {
		app.openRedis(profile, true)
		return
	}
	app.openRedisWorkspace(connectionID)
	if reusedOverview && !app.activeQuery.redisStateLoaded {
		app.scanRedis(true)
	}
}

func (app *workbench) saveRedisDefaultDatabase(database int) {
	profile, ok := app.activeSavedProfile()
	if !ok || app.preferences == nil || app.busy {
		return
	}
	profile.RedisDB = database
	app.preferenceRevision++
	revision, store := app.preferenceRevision, app.preferences
	app.busy = true
	go func() {
		saved, err := store.SaveConnectionProfile(profile, revision)
		settings := store.Settings()
		app.window.Update(func() {
			app.busy = false
			if err != nil {
				app.reportError("保存 Redis 默认数据库失败", err)
				return
			}
			app.connectionProfiles = settings.ConnectionProfiles
			app.status = fmt.Sprintf(app.localized("Redis 默认数据库已设为 db%d。"), saved.RedisDB)
		})
	}()
}

func (app *workbench) confirmClearRedisDatabase() {
	admin, ok := app.keyStore.(connection.RedisDatabaseAdmin)
	if !ok || app.busy || app.dialogOpen || app.openedReadOnly {
		return
	}
	profile, _ := app.activeSavedProfile()
	admin, id := admin, app.openedConnectionID
	app.busy = true
	app.status = app.localized("正在读取 Redis DB 键数量…")
	go func() {
		ctx, cancel := app.withTaskTimeout(10 * time.Second)
		count, err := admin.DatabaseSize(ctx)
		cancel()
		app.window.Update(func() {
			app.busy = false
			if app.openedConnectionID != id {
				return
			}
			if err != nil {
				app.reportError("清空 Redis DB 失败", err)
				return
			}
			detail := fmt.Sprintf("%s · db%d\n%s", profile.Name, profile.RedisDB, fmt.Sprintf(app.localized("将永久删除当前逻辑数据库中的 %d 个键。此操作无法撤销。"), count))
			app.requestConfirmation(app.localized("清空 Redis DB"), app.localized("确认清空当前 Redis DB？"), detail, []string{"取消", "清空"}, 1, func(choice int) {
				if choice != 1 || app.openedConnectionID != id {
					return
				}
				app.busy = true
				app.status = app.localized("正在清空 Redis DB…")
				go func() {
					ctx, cancel := app.withTaskTimeout(20 * time.Second)
					flushErr := admin.FlushDatabase(ctx)
					cancel()
					app.window.Update(func() {
						app.busy = false
						if app.openedConnectionID != id {
							return
						}
						if flushErr != nil {
							app.reportError("清空 Redis DB 失败", flushErr)
							return
						}
						app.redisKeys = nil
						app.redisSelectedKey = ""
						app.status = app.localized("Redis DB 已清空。")
						app.scanRedis(true)
					})
				}()
			})
		})
	}()
}
