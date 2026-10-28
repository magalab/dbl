package app

import (
	"fmt"
	"time"

	"github.com/magalab/dbl/internal/connection"
)

func redisKeyTTLDeadlines(metadata map[string]connection.KeyInfo, now time.Time) map[string]time.Time {
	deadlines := make(map[string]time.Time)
	for key, info := range metadata {
		if info.TTL > 0 {
			deadlines[key] = now.Add(info.TTL)
		}
	}
	return deadlines
}

func (app *workbench) redisKeyTTLPresentation(key string, ttl time.Duration) (string, bool, bool) {
	if ttl == -2 {
		return app.localized("已过期"), false, true
	}
	if deadline, ok := app.redisKeyTTLDeadlines[key]; ok {
		now := app.redisTTLNow
		if now.IsZero() {
			now = time.Now()
		}
		remaining := deadline.Sub(now)
		if remaining <= 0 {
			return app.localized("已过期"), false, true
		}
		return redisCountdownLabel(remaining, app.language), remaining <= 2*time.Minute, false
	}
	if ttl > 0 {
		return redisCountdownLabel(ttl, app.language), ttl <= 2*time.Minute, false
	}
	return app.localized(redisTTLLabel(ttl)), false, false
}

func redisCountdownLabel(remaining time.Duration, language string) string {
	seconds := int64(remaining / time.Second)
	if remaining%time.Second > 0 {
		seconds++
	}
	if seconds < 1 {
		seconds = 1
	}
	days, hours := seconds/86400, seconds%86400/3600
	minutes, seconds := seconds%3600/60, seconds%60
	if language == "en" {
		switch {
		case days > 0 && hours > 0:
			return fmt.Sprintf("%dd %dh", days, hours)
		case days > 0:
			return fmt.Sprintf("%dd", days)
		case hours > 0 && minutes > 0:
			return fmt.Sprintf("%dh %dm", hours, minutes)
		case hours > 0:
			return fmt.Sprintf("%dh", hours)
		case minutes > 0 && seconds > 0:
			return fmt.Sprintf("%dm %ds", minutes, seconds)
		case minutes > 0:
			return fmt.Sprintf("%dm", minutes)
		default:
			return fmt.Sprintf("%ds", seconds)
		}
	}
	switch {
	case days > 0 && hours > 0:
		return fmt.Sprintf("%d天 %d小时", days, hours)
	case days > 0:
		return fmt.Sprintf("%d天", days)
	case hours > 0 && minutes > 0:
		return fmt.Sprintf("%d小时 %d分钟", hours, minutes)
	case hours > 0:
		return fmt.Sprintf("%d小时", hours)
	case minutes > 0 && seconds > 0:
		return fmt.Sprintf("%d分钟 %d秒", minutes, seconds)
	case minutes > 0:
		return fmt.Sprintf("%d分钟", minutes)
	default:
		return fmt.Sprintf("%d秒", seconds)
	}
}

func (app *workbench) hasLiveRedisTTL() bool {
	now := time.Now()
	for _, deadline := range app.redisKeyTTLDeadlines {
		if deadline.After(now) {
			return true
		}
	}
	return false
}

func (app *workbench) ensureRedisTTLRefresh() {
	if app.window == nil || app.redisTTLRefreshTimer != nil || !app.hasLiveRedisTTL() {
		return
	}
	app.redisTTLRefreshGeneration++
	generation := app.redisTTLRefreshGeneration
	window := app.window
	app.redisTTLRefreshTimer = time.AfterFunc(time.Second, func() {
		window.Update(func() {
			if app.redisTTLRefreshGeneration != generation {
				return
			}
			app.redisTTLRefreshTimer = nil
			app.redisTTLNow = time.Now()
			if app.activeQuery != nil && app.activeQuery.redisView && app.activeQuery.redisConnectionID == app.openedConnectionID {
				app.ensureRedisTTLRefresh()
			}
		})
	})
}

func (app *workbench) cancelRedisTTLRefresh() {
	if app.redisTTLRefreshTimer != nil {
		app.redisTTLRefreshTimer.Stop()
		app.redisTTLRefreshTimer = nil
	}
	app.redisTTLRefreshGeneration++
	app.redisTTLNow = time.Now()
}

func redisTTLLabel(ttl time.Duration) string {
	switch ttl {
	case -2:
		return "已过期"
	case -1:
		return "永不过期"
	default:
		if ttl < 0 {
			return "未知"
		}
		return ttl.Round(time.Second).String()
	}
}
