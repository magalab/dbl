package app

import (
	"github.com/egoist/mygo/ui"
)

func (app *workbench) redisKeyEditorModal(c *ui.Context) {
	if ui.Modal(c, &app.showRedisKeyEditor, func() {
		ui.Column(c).Width(440).MaxHeightPercent(90).Padding(10).Gap(8).Children(func() {
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Text(c, app.localized("新增键")).Bold().Role(ui.RoleHeading).Level(2).Grow(1)
				close := iconButton(c, app.localized("关闭"), actionIcons["close"]).Width(30).Height(30).Padding(4)
				if close.Clicked() {
					app.showRedisKeyEditor = false
				}
			})

			usesEntries := app.redisNewType == "hash" || app.redisNewType == "list" || app.redisNewType == "set" || app.redisNewType == "zset" || app.redisNewType == "stream"
			needsScroll := usesEntries && len(app.redisNewEntries) > 4
			renderEntries := func() {
				removeEntryIndex := -1
				ui.Column(c).Gap(8).Children(func() {
					for i := range app.redisNewEntries {
						i := i
						ui.Row(c).Gap(6).Children(func() {
							switch app.redisNewType {
							case "hash", "stream":
								field := ui.TextInput(c, &app.redisNewEntries[i].Field).Placeholder(app.localized("字段名")).Label(app.localized("字段名")).Grow(1)
								field.Disabled(app.busy || app.dialogOpen)
							case "zset":
								score := ui.TextInput(c, &app.redisNewEntries[i].Score).Width(125).Placeholder("0").Label(app.localized("分数"))
								score.Disabled(app.busy || app.dialogOpen)
							}
							value := ui.TextInput(c, &app.redisNewEntries[i].Value).Placeholder(app.localized("值")).Label(app.localized("值")).Grow(1)
							value.Disabled(app.busy || app.dialogOpen)
							if len(app.redisNewEntries) > 1 {
								remove := iconButton(c, app.localized("移除条目"), actionIcons["trash"])
								remove.Disabled(app.busy || app.dialogOpen)
								if remove.Clicked() {
									removeEntryIndex = i
								}
							}
						})
					}
				})
				if removeEntryIndex >= 0 && removeEntryIndex < len(app.redisNewEntries) && len(app.redisNewEntries) > 1 {
					app.redisNewEntries = append(app.redisNewEntries[:removeEntryIndex], app.redisNewEntries[removeEntryIndex+1:]...)
				}
			}

			ui.Column(c).Gap(8).Padding(1, 12, 1, 1).Children(func() {
				key := ui.TextInput(c, &app.redisNewKey).Placeholder(app.localized("键名")).Label(app.localized("键名"))
				key.Disabled(app.busy || app.dialogOpen)
				typeLabels := []string{"String", "Hash", "List", "Set", "Sorted Set", "Stream", "JSON"}
				app.redisNewTypeChoice = app.redisNewTypeLabel(app.redisNewType)
				modeOptions := []string{app.localized("永不过期"), app.localized("TTL 秒数")}
				if app.redisTTLModeUsesSeconds(app.redisNewTTLMode) {
					app.redisNewTTLMode = app.localized("TTL 秒数")
				} else {
					app.redisNewTTLMode = app.localized("永不过期")
				}
				ui.Row(c).Gap(8).Children(func() {
					typeSelect := ui.Select(c, &app.redisNewTypeChoice, typeLabels).Label(app.localized("键类型")).Width(110)
					typeSelect.Disabled(app.busy || app.dialogOpen)
					if typeSelect.Changed() {
						app.redisNewType = app.redisNewTypeFromLabel(app.redisNewTypeChoice)
						if app.redisNewType == "json" {
							app.checkRedisJSONSupport()
						}
					}
					mode := ui.Select(c, &app.redisNewTTLMode, modeOptions).Label(app.localized("过期时间")).Width(122)
					mode.Disabled(app.busy || app.dialogOpen)
					if app.redisTTLModeUsesSeconds(app.redisNewTTLMode) {
						ttl := ui.TextInput(c, &app.redisNewTTLSeconds).Width(88).Placeholder(app.localized("正整数秒")).Label(app.localized("TTL 秒数"))
						ttl.Disabled(app.busy || app.dialogOpen)
					}
				})
				switch app.redisNewType {
				case "string":
					value := ui.TextArea(c, &app.redisNewValue).Lines(6, 12).FillWidth().Placeholder(app.localized("值")).Label(app.localized("值"))
					value.Disabled(app.busy || app.dialogOpen)
				case "json":
					if app.redisJSONChecking {
						ui.Text(c, app.localized("正在检测 RedisJSON…")).TextColor(c.Theme().TextMuted)
					} else if app.redisJSONSupportKnown && !app.redisJSONSupported {
						ui.Text(c, app.localized("该 Redis 服务器未加载或未授权 RedisJSON 模块。")).TextColor(c.Theme().Danger)
						if app.redisJSONSupportError != "" {
							ui.Text(c, app.redisJSONSupportError).TextColor(c.Theme().TextMuted).Selectable()
						}
					} else {
						ui.Text(c, app.localized("RedisJSON 可用；输入有效 JSON 文本。")).TextColor(c.Theme().Success)
					}
					jsonValue := ui.TextArea(c, &app.redisNewValue).Lines(6, 12).FillWidth().Placeholder(`{"name":"value"}`).Label(app.localized("JSON 值"))
					jsonValue.Disabled(app.busy || app.dialogOpen || app.redisJSONChecking || !app.redisJSONSupported)
				case "hash", "list", "set", "zset", "stream":
					if app.redisNewType == "stream" {
						ui.Text(c, app.localized("Stream ID；输入 * 由 Redis 自动生成。")).TextColor(c.Theme().TextMuted)
						streamID := ui.TextInput(c, &app.redisNewStreamID).Placeholder("*").Label(app.localized("条目 ID"))
						streamID.Disabled(app.busy || app.dialogOpen)
					}
					ui.Row(c).Gap(8).Children(func() {
						ui.Text(c, app.localized("条目")).Bold().Grow(1)
						add := ui.Button(c, "+ "+app.localized("添加条目"))
						add.Disabled(app.busy || app.dialogOpen)
						if add.Clicked() {
							app.redisNewEntries = append(app.redisNewEntries, redisEntryDraft{})
						}
					})
					if needsScroll {
						ui.Scroll(c).MaxHeight(320).MinHeight(0).Children(renderEntries)
					} else {
						renderEntries()
					}
				}
			})

			ui.Row(c).Gap(6).Children(func() {
				ui.Spacer(c).Grow(1)
				cancel := ui.Button(c, app.localized("取消")).Padding(5, 9)
				if cancel.Clicked() {
					app.showRedisKeyEditor = false
				}
				_, validationErr := app.redisNewMutations()
				add := ui.PrimaryButton(c, "+ "+app.localized("创建键")).Padding(5, 9)
				add.Disabled(app.busy || app.dialogOpen || app.openedReadOnly || validationErr != nil || app.redisNewType == "json" && (app.redisJSONChecking || !app.redisJSONSupported))
				if add.Clicked() {
					app.createRedisKey()
				}
			})
		})
	}).Dismissed() {
		app.showRedisKeyEditor = false
	}
}

func (app *workbench) redisNewTypeLabel(kind string) string {
	labels := map[string]string{
		"string": "String", "hash": "Hash", "list": "List",
		"set": "Set", "zset": "Sorted Set", "stream": "Stream", "json": "JSON",
	}
	if label := labels[kind]; label != "" {
		return label
	}
	return labels["string"]
}

func (app *workbench) redisNewTypeFromLabel(label string) string {
	for _, kind := range []string{"string", "hash", "list", "set", "zset", "stream", "json"} {
		if app.redisNewTypeLabel(kind) == label {
			return kind
		}
	}
	return "string"
}
