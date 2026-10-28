package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/magalab/dbl/internal/connection"
	"github.com/magalab/dbl/internal/preferences"
	redisadapter "github.com/magalab/dbl/internal/redis"
)

func (app *workbench) openRedis(profile preferences.ConnectionProfile) {
	if app.busy || app.exporting || app.dialogOpen || profile.Driver != preferences.DriverRedis {
		return
	}
	app.cancelRedisDeleteConfirmation()
	app.cancelRedisTTLRefresh()
	app.stageActiveDataWorkspaceDraft()
	if app.activeQuery != nil && app.activeQuery.redisView {
		app.saveRedisWorkspaceState(app.activeQuery)
	}
	app.busy = true
	if app.etcdCancel != nil {
		app.etcdCancel()
		app.etcdCancel = nil
		app.etcdWatching = false
	}
	app.status = app.localized("正在连接 Redis…")
	match := redisMatchPattern(app.redisMatch)
	keyType := ""
	oldCursor, oldCancel := app.cursor, app.cancelQuery
	if oldCursor != nil {
		app.updateHistory(app.activeQuery.activeHistoryID, time.Time{}, "已切换数据库，停止后续读取")
		app.activeQuery.streamStopped = true
	}
	app.cursor, app.cancelQuery, app.queryContext = nil, nil, nil
	if oldCancel != nil {
		oldCancel()
	}
	ctx, cancel := app.withTaskTimeout(20 * time.Second)
	app.cancelRedis = cancel
	go func() {
		if oldCursor != nil {
			_ = oldCursor.Close()
		}
		defer cancel()
		adapter, err := (redisadapter.Connector{Profile: profile, Vault: app.vault}).Connect(ctx)
		var keys connection.KeyValue
		if err == nil {
			var ok bool
			keys, ok = adapter.(connection.KeyValue)
			if !ok {
				err = errors.New("Redis adapter does not provide key/value capabilities")
			}
		}
		var page connection.KeyPage
		var metadata map[string]connection.KeyInfo
		if err == nil {
			page, err = keys.ScanKeysByType(ctx, match, keyType, 0, 100)
			if err == nil {
				metadata = inspectRedisKeyMetadata(ctx, keys, page.Keys)
			}
		}
		if err != nil && adapter != nil {
			_ = adapter.Close()
		}
		app.window.Update(func() {
			app.busy = false
			app.cancelRedis = nil
			if err != nil {
				if errors.Is(err, context.Canceled) {
					app.status = app.localized("Redis 连接已取消。")
					return
				}
				app.reportError("连接 Redis 失败", err)
				return
			}
			sessionID := redisDatabaseConnectionID(profile, profile.RedisDB)
			app.registerLiveConnection(sessionID, keys, profile)
			app.store, app.keyStore, app.documentStore, app.etcdStore = nil, keys, nil, nil
			app.redisKeys, app.redisCursor, app.redisHasMore = page.Keys, page.NextCursor, page.NextCursor != 0
			app.replaceRedisKeyMetadata(metadata)
			app.redisKeySelection.Clear()
			app.redisEntryFilter = ""
			app.redisEntryEditorOpen = false
			app.redisSelectedKey = ""
			app.openedDriver = connection.DriverRedis
			app.openedConnectionID = sessionID
			app.openedLabel = fmt.Sprintf("%s · %s/%d", profile.Name, profile.Host, profile.RedisDB)
			app.openedEnvironment = profile.Environment
			app.openedReadOnly = profile.ReadOnly
			app.status = app.localized("Redis 已连接，已加载一页 SCAN 结果。")
			for _, workspace := range app.workspaces {
				if workspace.redisView && workspace.redisConnectionID == sessionID {
					workspace.redisStateLoaded = false
				}
			}
			app.openRedisWorkspace(sessionID)
			app.saveRedisWorkspaceState(app.activeQuery)
		})
	}()
}

func (app *workbench) scanRedis(reset bool) {
	if app.busy || app.keyStore == nil {
		return
	}
	if reset {
		app.cancelRedisDeleteConfirmation()
		app.cancelRedisTTLRefresh()
	}
	keys := app.keyStore
	cursor := app.redisCursor
	if reset {
		cursor = 0
		app.redisKeys = nil
		app.redisHasMore = false
		app.redisSelectedKey = ""
		app.redisKeySelection.Clear()
		app.redisKeyMetadata = make(map[string]connection.KeyInfo)
		app.redisKeyTTLDeadlines = make(map[string]time.Time)
		app.redisInfo = connection.KeyInfo{}
		app.redisPage = connection.KeyValuePage{}
		app.redisValueCursor = ""
	}
	match := redisMatchPattern(app.redisMatch)
	keyType := ""
	app.busy = true
	app.status = app.localized("正在使用 SCAN 读取 Redis 键…")
	ctx, cancel := app.withTaskTimeout(15 * time.Second)
	app.cancelRedis = cancel
	go func() {
		defer cancel()
		page, err := keys.ScanKeysByType(ctx, match, keyType, cursor, 100)
		var metadata map[string]connection.KeyInfo
		if err == nil {
			metadata = inspectRedisKeyMetadata(ctx, keys, page.Keys)
		}
		app.window.Update(func() {
			app.busy = false
			app.cancelRedis = nil
			if app.keyStore != keys {
				return
			}
			if err != nil {
				if errors.Is(err, context.Canceled) {
					app.status = app.localized("Redis 键扫描已取消。")
					return
				}
				app.reportError("Redis 键扫描失败", err)
				return
			}
			if reset {
				app.redisKeys = nil
				app.redisKeySelection.Clear()
				app.redisKeyMetadata = make(map[string]connection.KeyInfo)
				app.redisKeyTTLDeadlines = make(map[string]time.Time)
			}
			seen := make(map[string]struct{}, len(app.redisKeys)+len(page.Keys))
			for _, key := range app.redisKeys {
				seen[key] = struct{}{}
			}
			for _, key := range page.Keys {
				if _, exists := seen[key]; exists {
					continue
				}
				app.redisKeys = append(app.redisKeys, key)
				seen[key] = struct{}{}
			}
			app.mergeRedisKeyMetadata(metadata)
			app.redisCursor, app.redisHasMore = page.NextCursor, page.NextCursor != 0
			app.status = fmt.Sprintf("已加载 %d 个键；SCAN 可继续读取。", len(app.redisKeys))
		})
	}()
}

func inspectRedisKeyMetadata(ctx context.Context, store connection.KeyValue, keys []string) map[string]connection.KeyInfo {
	metadata := make(map[string]connection.KeyInfo, len(keys))
	if bulk, ok := store.(connection.RedisBulkKeyInspector); ok {
		infos, err := bulk.InspectKeys(ctx, keys)
		if err == nil {
			for _, info := range infos {
				metadata[info.Key] = info
			}
			return metadata
		}
	}
	for _, key := range keys {
		if ctx.Err() != nil {
			break
		}
		info, err := store.InspectKey(ctx, key)
		if err == nil {
			metadata[key] = info
		}
	}
	return metadata
}

func (app *workbench) replaceRedisKeyMetadata(metadata map[string]connection.KeyInfo) {
	app.redisKeyMetadata = make(map[string]connection.KeyInfo, len(metadata))
	app.redisKeyTTLDeadlines = redisKeyTTLDeadlines(metadata, time.Now())
	for key, info := range metadata {
		app.redisKeyMetadata[key] = info
	}
	app.redisTTLNow = time.Now()
}

func (app *workbench) mergeRedisKeyMetadata(metadata map[string]connection.KeyInfo) {
	if app.redisKeyMetadata == nil {
		app.redisKeyMetadata = make(map[string]connection.KeyInfo)
	}
	if app.redisKeyTTLDeadlines == nil {
		app.redisKeyTTLDeadlines = make(map[string]time.Time)
	}
	now := time.Now()
	for key, info := range metadata {
		app.redisKeyMetadata[key] = info
		if info.TTL > 0 {
			app.redisKeyTTLDeadlines[key] = now.Add(info.TTL)
		} else {
			delete(app.redisKeyTTLDeadlines, key)
		}
	}
	app.redisTTLNow = now
}

func redisMatchPattern(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "*"
	}
	if strings.ContainsAny(value, "*?[") {
		return value
	}
	return value + "*"
}
