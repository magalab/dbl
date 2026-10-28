package app

import (
	"time"

	"github.com/egoist/mygo/ui"
)

func (app *workbench) redisWorkspace(c *ui.Context) {
	app.redisTTLNow = time.Now()
	app.ensureRedisTTLRefresh()
	if app.redisDeleteConfirmKey != "" && !app.dialogOpen {
		c.OnShortcut(0, ui.KeyEscape, app.cancelRedisDeleteConfirmation)
	}
	ui.Split(c, &app.redisKeyListWidth, func() {
		app.redisKeyListPane(c)
	}, func() {
		app.redisKeyDetailPane(c)
	}).Grow(1)
}
