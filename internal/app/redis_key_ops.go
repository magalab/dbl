package app

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/magalab/dbl/internal/connection"
	redisadapter "github.com/magalab/dbl/internal/redis"
)

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func (app *workbench) selectedRedisKeys() []string {
	selected := make([]string, 0, app.redisKeySelection.Len())
	for _, key := range app.redisKeys {
		if app.redisKeySelection.Has(key) {
			selected = append(selected, key)
		}
	}
	return selected
}

func (app *workbench) selectAllRedisKeys() {
	if len(app.redisKeys) > 0 && app.redisKeySelection.Len() == len(app.redisKeys) {
		app.redisKeySelection.Clear()
		return
	}
	app.redisKeySelection.Clear()
	for _, key := range app.redisKeys {
		app.redisKeySelection.Add(key)
	}
}

func (app *workbench) confirmDeleteSelectedRedisKeys() {
	keys := app.selectedRedisKeys()
	if app.busy || app.dialogOpen || app.keyStore == nil || app.openedReadOnly || len(keys) == 0 {
		return
	}
	list := strings.Join(keys, "\n")
	if len(list) > 4096 {
		list = truncateText(list, 4096) + "\n…"
	}
	operation := fmt.Sprintf("即将删除 %d 个 Redis 键：\n%s\n此操作无法撤销。", len(keys), list)
	if app.language == "en" {
		operation = fmt.Sprintf("About to delete %d Redis keys:\n%s\nThis action cannot be undone.", len(keys), list)
	}
	detail := app.redisOperationConfirmationDetail(environmentLabel(app.openedEnvironment), app.openedLabel, operation)
	store, connectionID := app.keyStore, app.openedConnectionID
	app.requestConfirmation("批量删除 Redis 键", "确认删除所选键？", detail, []string{"取消", "删除"}, 1, func(choice int) {
		if choice != 1 || app.openedConnectionID != connectionID || app.keyStore != store {
			return
		}
		app.busy = true
		app.status = app.localized("正在批量删除 Redis 键…")
		go func() {
			ctx, cancel := app.withTaskTimeout(20 * time.Second)
			var err error
			if bulk, ok := store.(connection.RedisBulkKeyDeleter); ok {
				err = bulk.DeleteRedisKeys(ctx, keys)
			} else {
				for _, key := range keys {
					if err = store.DeleteKey(ctx, key); err != nil {
						break
					}
				}
			}
			cancel()
			app.window.Update(func() {
				app.busy = false
				if app.openedConnectionID != connectionID || app.keyStore != store {
					return
				}
				if err != nil {
					app.reportError("批量删除 Redis 键失败", err)
					return
				}
				for _, key := range keys {
					delete(app.redisKeyMetadata, key)
					delete(app.redisKeyTTLDeadlines, key)
					app.redisKeySelection.Remove(key)
				}
				if containsString(keys, app.redisSelectedKey) {
					app.redisSelectedKey = ""
					app.redisInfo = connection.KeyInfo{}
					app.redisPage = connection.KeyValuePage{}
				}
				app.scanRedis(true)
			})
		}()
	})
}

func (app *workbench) inspectRedisKey(key string) {
	if app.busy || app.keyStore == nil {
		return
	}
	keys := app.keyStore
	app.busy = true
	app.redisSelectedKey = key
	app.redisInfo = connection.KeyInfo{Key: key}
	app.redisPage = connection.KeyValuePage{}
	app.redisValueCursor = ""
	app.redisValueDraft = ""
	app.redisEntryField = ""
	app.redisEntryScore = ""
	app.status = app.localized("正在读取 Redis 键…")
	ctx, cancel := app.withTaskTimeout(15 * time.Second)
	app.cancelRedis = cancel
	go func() {
		defer cancel()
		info, err := keys.InspectKey(ctx, key)
		var value connection.KeyValuePage
		if err == nil {
			value, err = keys.ReadKey(ctx, key, "", 100)
		}
		app.window.Update(func() {
			app.busy = false
			app.cancelRedis = nil
			if app.keyStore != keys {
				return
			}
			if err != nil {
				if errors.Is(err, context.Canceled) {
					app.status = app.localized("Redis 键读取已取消。")
					return
				}
				app.reportError("读取 Redis 键失败", err)
				return
			}
			app.redisInfo, app.redisPage, app.redisValueCursor = info, value, value.NextCursor
			app.mergeRedisKeyMetadata(map[string]connection.KeyInfo{key: info})
			app.redisValueDraft = value.StringValue
			app.status = app.localized("Redis 键详情已更新。")
		})
	}()
}

func (app *workbench) readMoreRedisValue() {
	if app.busy || app.keyStore == nil || app.redisSelectedKey == "" || app.redisValueCursor == "" {
		return
	}
	keys, key, cursor := app.keyStore, app.redisSelectedKey, app.redisValueCursor
	app.busy = true
	ctx, cancel := app.withTaskTimeout(15 * time.Second)
	app.cancelRedis = cancel
	go func() {
		defer cancel()
		page, err := keys.ReadKey(ctx, key, cursor, 100)
		app.window.Update(func() {
			app.busy = false
			app.cancelRedis = nil
			if app.keyStore != keys {
				return
			}
			if err != nil {
				if errors.Is(err, context.Canceled) {
					app.status = app.localized("Redis 值读取已取消。")
					return
				}
				app.reportError("继续读取 Redis 值失败", err)
				return
			}
			app.redisPage.Entries = append(app.redisPage.Entries, page.Entries...)
			app.redisValueCursor = page.NextCursor
			app.status = app.localized("Redis 值已继续读取。")
		})
	}()
}

func (app *workbench) confirmDeleteRedisKey() {
	app.confirmDeleteRedisKeyNamed(app.redisSelectedKey)
}

func (app *workbench) confirmDeleteRedisKeyNamed(key string) {
	if app.busy || app.dialogOpen || app.keyStore == nil || key == "" || app.openedReadOnly {
		return
	}
	keys := app.keyStore
	connectionID := app.openedConnectionID
	operation := fmt.Sprintf("将删除 Redis 键：%s\n此操作无法在 DBL 中撤销。", key)
	if app.language == "en" {
		operation = fmt.Sprintf("Delete Redis key: %s\nThis cannot be undone in DBL.", key)
	}
	detail := app.redisOperationConfirmationDetail(environmentLabel(app.openedEnvironment), app.openedLabel, operation)
	app.requestConfirmation("删除 Redis 键", "确认删除这个 Redis 键？", detail, []string{"取消", "删除"}, 1, func(choice int) {
		if choice != 1 || app.openedConnectionID != connectionID || app.keyStore != keys {
			return
		}
		app.deleteRedisKeyConfirmed(key, keys, connectionID)
	})
}

func (app *workbench) armRedisDeleteConfirmation(key string) {
	if app.busy || app.dialogOpen || app.openedReadOnly || app.keyStore == nil || key == "" {
		return
	}
	app.cancelRedisDeleteConfirmation()
	app.redisDeleteConfirmKey = key
	generation := app.redisDeleteConfirmGeneration
	app.redisDeleteConfirmTimer = time.AfterFunc(5*time.Second, func() {
		app.window.Update(func() {
			if app.redisDeleteConfirmGeneration != generation {
				return
			}
			app.redisDeleteConfirmKey = ""
			app.redisDeleteConfirmTimer = nil
			app.redisDeleteConfirmGeneration++
		})
	})
}

func (app *workbench) cancelRedisDeleteConfirmation() {
	if app.redisDeleteConfirmTimer != nil {
		app.redisDeleteConfirmTimer.Stop()
		app.redisDeleteConfirmTimer = nil
	}
	app.redisDeleteConfirmGeneration++
	app.redisDeleteConfirmKey = ""
}

func (app *workbench) deleteRedisKeyConfirmed(key string, keys connection.KeyValue, connectionID string) {
	if app.busy || app.keyStore == nil || keys == nil || app.keyStore != keys || app.openedConnectionID != connectionID || app.openedReadOnly || key == "" {
		return
	}
	app.busy = true
	go func() {
		ctx, cancel := app.withTaskTimeout(10 * time.Second)
		err := keys.DeleteKey(ctx, key)
		cancel()
		app.window.Update(func() {
			app.busy = false
			if app.openedConnectionID != connectionID || app.keyStore != keys {
				return
			}
			if err != nil {
				app.reportError("删除 Redis 键失败", err)
				return
			}
			delete(app.redisKeyMetadata, key)
			delete(app.redisKeyTTLDeadlines, key)
			app.redisKeySelection.Remove(key)
			if app.redisSelectedKey == key {
				app.redisSelectedKey = ""
				app.redisInfo = connection.KeyInfo{}
				app.redisPage = connection.KeyValuePage{}
			}
			app.scanRedis(true)
			app.status = app.localized("Redis 键已删除。")
		})
	}()
}

func (app *workbench) confirmWriteRedisKey() {
	app.writeRedisKey()
}

func (app *workbench) writeRedisKey() {
	if app.busy || app.dialogOpen || app.keyStore == nil || app.redisSelectedKey == "" || app.openedReadOnly || app.redisInfo.Type == "" {
		return
	}
	if len(app.redisValueDraft) > redisadapter.MaxValueBytes || len(app.redisEntryField) > redisadapter.MaxValueBytes {
		app.reportError("写入 Redis 数据失败", errors.New("Redis 值和字段不能超过 1 MiB"))
		return
	}
	score := 0.0
	if app.redisInfo.Type == "zset" {
		parsed, err := strconv.ParseFloat(strings.TrimSpace(app.redisEntryScore), 64)
		if err != nil || math.IsNaN(parsed) || math.IsInf(parsed, 0) {
			app.reportError("写入 Redis 数据失败", errors.New("zset score 必须是有限数字"))
			return
		}
		score = parsed
	}
	mutation := connection.KeyMutation{
		Key: app.redisSelectedKey, Type: app.redisInfo.Type, Field: app.redisEntryField,
		Value: app.redisValueDraft, Score: score,
		PreserveTTL: true,
	}
	keys := app.keyStore
	connectionID := app.openedConnectionID
	app.busy = true
	app.status = app.localized("正在写入 Redis 数据…")
	go func() {
		ctx, cancel := app.withTaskTimeout(10 * time.Second)
		err := keys.WriteKey(ctx, mutation)
		cancel()
		app.window.Update(func() {
			app.busy = false
			if app.openedConnectionID != connectionID || app.keyStore != keys {
				return
			}
			if err != nil {
				app.reportError("写入 Redis 数据失败", err)
				return
			}
			app.redisEntryEditorOpen = false
			app.redisEntryField = ""
			app.redisEntryScore = ""
			app.inspectRedisKey(mutation.Key)
			app.status = app.localized("Redis 数据已写入。")
		})
	}()
}
