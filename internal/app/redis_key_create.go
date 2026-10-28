package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/magalab/dbl/internal/connection"
	redisadapter "github.com/magalab/dbl/internal/redis"
	"github.com/magalab/dbl/internal/safeerror"
)

func (app *workbench) beginRedisKeyCreation() {
	if app.busy || app.dialogOpen || app.keyStore == nil || app.openedReadOnly {
		return
	}
	app.redisNewKey = ""
	app.redisNewType = "string"
	app.redisNewTTLMode = app.localized("永不过期")
	app.redisNewTTLSeconds = ""
	app.redisNewValue = ""
	app.redisNewStreamID = "*"
	app.redisNewEntries = []redisEntryDraft{{}}
	app.showRedisKeyEditor = true
}

func (app *workbench) checkRedisJSONSupport() {
	connectionID := app.openedConnectionID
	if app.keyStore == nil || app.redisJSONChecking || (app.redisJSONSupportKnown && app.redisJSONSupportConnectionID == connectionID) {
		return
	}
	store := app.keyStore
	app.redisJSONChecking = true
	ctx, cancel := app.withTaskTimeout(8 * time.Second)
	go func() {
		defer cancel()
		var err error
		if support, ok := store.(connection.RedisJSONSupport); ok {
			err = support.CheckJSONSupport(ctx)
		} else {
			err = errors.New("RedisJSON capability is unavailable")
		}
		app.window.Update(func() {
			app.redisJSONChecking = false
			if app.openedConnectionID != connectionID || app.keyStore != store {
				return
			}
			app.redisJSONSupportKnown = true
			app.redisJSONSupportConnectionID = connectionID
			app.redisJSONSupported = err == nil
			app.redisJSONSupportError = ""
			if err != nil {
				app.redisJSONSupportError = safeerror.Display(err)
			}
		})
	}()
}

func (app *workbench) createRedisKey() {
	if app.busy || app.dialogOpen || app.keyStore == nil || app.openedReadOnly {
		return
	}
	mutations, err := app.redisNewMutations()
	if err != nil {
		app.reportError("新增 Redis 键失败", err)
		return
	}
	key := mutations[0].Key
	store, connectionID := app.keyStore, app.openedConnectionID
	app.busy = true
	app.status = app.localized("正在新增 Redis 键…")
	go func() {
		ctx, cancel := app.withTaskTimeout(15 * time.Second)
		var writeErr error
		if _, inspectErr := store.InspectKey(ctx, key); inspectErr == nil {
			writeErr = errors.New("Redis 键已存在")
		} else if store.ClassifyError(inspectErr) != connection.ErrorNotFound {
			writeErr = inspectErr
		}
		if writeErr == nil {
			if batch, ok := store.(connection.RedisBulkKeyWriter); ok {
				writeErr = batch.WriteKeyBatch(ctx, mutations)
			} else {
				for _, mutation := range mutations {
					if writeErr = store.WriteKey(ctx, mutation); writeErr != nil {
						break
					}
				}
			}
		}
		var info connection.KeyInfo
		if writeErr == nil {
			info, writeErr = store.InspectKey(ctx, key)
		}
		cancel()
		app.window.Update(func() {
			app.busy = false
			if app.openedConnectionID != connectionID || app.keyStore != store {
				return
			}
			if writeErr != nil {
				app.reportError("新增 Redis 键失败", writeErr)
				return
			}
			app.showRedisKeyEditor = false
			app.redisNewEntries = nil
			if !containsString(app.redisKeys, key) {
				app.redisKeys = append(app.redisKeys, key)
			}
			app.mergeRedisKeyMetadata(map[string]connection.KeyInfo{key: info})
			app.redisSelectedKey = key
			app.redisKeySelection.Clear()
			app.inspectRedisKey(key)
		})
	}()
}

func (app *workbench) redisNewMutations() ([]connection.KeyMutation, error) {
	key := strings.TrimSpace(app.redisNewKey)
	if key == "" {
		return nil, errors.New("Redis 键名不能为空")
	}
	ttl := time.Duration(0)
	if app.redisTTLModeUsesSeconds(app.redisNewTTLMode) {
		seconds, err := strconv.ParseInt(strings.TrimSpace(app.redisNewTTLSeconds), 10, 64)
		if err != nil || seconds < 1 || seconds > 315360000 {
			return nil, errors.New("TTL 秒数必须在 1 到 315360000 之间")
		}
		ttl = time.Duration(seconds) * time.Second
	}
	newMutation := func() connection.KeyMutation {
		return connection.KeyMutation{Key: key, Type: app.redisNewType, TTL: ttl, ID: strings.TrimSpace(app.redisNewStreamID)}
	}
	if app.redisNewType == "string" || app.redisNewType == "json" {
		if app.redisNewType == "json" && (!app.redisJSONSupportKnown || !app.redisJSONSupported || app.redisJSONSupportConnectionID != app.openedConnectionID) {
			return nil, errors.New("此 Redis 服务器没有可用的 RedisJSON 模块")
		}
		value := app.redisNewValue
		if len(value) > redisadapter.MaxValueBytes {
			return nil, errors.New("Redis 值不能超过 1 MiB")
		}
		if app.redisNewType == "json" && !json.Valid([]byte(value)) {
			return nil, errors.New("请输入有效的 JSON")
		}
		mutation := newMutation()
		mutation.Value = value
		return []connection.KeyMutation{mutation}, nil
	}

	mutations := make([]connection.KeyMutation, 0, len(app.redisNewEntries))
	for _, entry := range app.redisNewEntries {
		field, value := strings.TrimSpace(entry.Field), entry.Value
		if field == "" && strings.TrimSpace(value) == "" && strings.TrimSpace(entry.Score) == "" {
			continue
		}
		mutation := newMutation()
		mutation.Field = field
		mutation.Value = value
		switch app.redisNewType {
		case "hash", "stream":
			if field == "" {
				return nil, errors.New("字段名不能为空")
			}
		case "list", "set":
			if value == "" {
				return nil, errors.New("列表项或集合成员不能为空")
			}
		case "zset":
			if value == "" {
				return nil, errors.New("Sorted Set 成员不能为空")
			}
			score, err := strconv.ParseFloat(strings.TrimSpace(entry.Score), 64)
			if err != nil || math.IsNaN(score) || math.IsInf(score, 0) {
				return nil, errors.New("Score 必须是有限数字")
			}
			mutation.Score = score
		default:
			return nil, fmt.Errorf("不支持的 Redis 键类型：%s", app.redisNewType)
		}
		if len(field) > redisadapter.MaxValueBytes || len(value) > redisadapter.MaxValueBytes {
			return nil, errors.New("Redis 值和字段不能超过 1 MiB")
		}
		mutations = append(mutations, mutation)
	}
	if len(mutations) == 0 {
		return nil, errors.New("至少填写一个条目")
	}
	return mutations, nil
}

func (app *workbench) redisTTLModeUsesSeconds(mode string) bool {
	return mode == "TTL 秒数" || mode == "TTL (seconds)"
}
