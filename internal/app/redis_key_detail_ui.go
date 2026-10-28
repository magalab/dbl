package app

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/magalab/dbl/internal/connection"
)

func (app *workbench) redisKeyDetailPane(c *ui.Context) {
	ui.Column(c).Fill().MinWidth(0).Children(func() {
		if app.redisSelectedKey == "" {
			ui.Column(c).Fill().Justify(ui.Center).AlignItems(ui.Center).Padding(20).Children(func() {
				ui.Text(c, app.localized("选择一个键查看类型、TTL 和分页值内容。")).TextColor(c.Theme().TextMuted)
			})
			return
		}
		ui.Row(c).Padding(7, 10).Gap(6).AlignItems(ui.Center).Children(func() {
			ui.Text(c, app.redisSelectedKey).Bold().Role(ui.RoleHeading).Level(2).SingleLine().Grow(1)
			refresh := iconButton(c, app.localized("刷新键详情"), actionIcons["refresh"]).Width(32).Height(30).Padding(4)
			refresh.Disabled(app.busy || app.dialogOpen)
			if refresh.Clicked() {
				app.inspectRedisKey(app.redisSelectedKey)
			}
			remove := iconButton(c, app.localized("删除此键…"), actionIcons["trash"]).Width(32).Height(30).Padding(4)
			remove.Disabled(app.busy || app.dialogOpen || app.openedReadOnly)
			if remove.Clicked() {
				app.confirmDeleteRedisKey()
			}
		})
		ui.Row(c).Padding(0, 10, 7, 10).Gap(8).Children(func() {
			kind := app.redisInfo.Type
			if kind == "" {
				kind = app.localized("读取中")
			}
			ui.Text(c, strings.ToUpper(kind)).TextColor(redisTypeColor(c, kind))
			if app.redisInfo.Type != "" {
				ui.Text(c, app.localized("大小")+" "+app.localized(redisMemoryLabel(app.redisInfo.Length))).TextColor(c.Theme().TextMuted)
			}
			ttl, expiringSoon, expired := app.redisKeyTTLPresentation(app.redisSelectedKey, app.redisInfo.TTL)
			if app.redisInfo.Type != "" {
				color := c.Theme().TextMuted
				if expiringSoon || expired {
					color = c.Theme().Danger
				}
				ui.Text(c, ttl).TextColor(color)
			}
		})
		if app.redisInfo.Type == "string" || app.redisInfo.Type == "json" {
			app.redisStringValuePane(c)
			return
		}
		app.redisStructuredValuePane(c)
	})
}

func (app *workbench) redisStringValuePane(c *ui.Context) {
	ui.Column(c).Fill().Padding(0, 10, 10, 10).Gap(6).Children(func() {
		value := ui.TextArea(c, &app.redisValueDraft).Lines(2, 5).FillWidth().Placeholder(app.localized("值")).Label(app.localized("值"))
		value.Disabled(app.busy || app.dialogOpen || app.openedReadOnly)
		if app.redisPage.StringValueTruncated {
			ui.Text(c, app.localized("值超过 1 MiB，已截断显示。")).TextColor(c.Theme().Warning)
		}
		ui.Row(c).Children(func() {
			ui.Spacer(c).Grow(1)
			save := iconPrimaryButton(c, app.localized("保存值…"), actionIcons["save"]).Width(34).Height(32).Padding(4)
			save.Disabled(app.busy || app.dialogOpen || app.openedReadOnly || app.redisInfo.Type == "")
			if save.Clicked() {
				app.writeRedisKey()
			}
		})
	})
}

func (app *workbench) redisStructuredValuePane(c *ui.Context) {
	entries := make([]connection.KeyValueEntry, 0, len(app.redisPage.Entries))
	filter := strings.ToLower(strings.TrimSpace(app.redisEntryFilter))
	for _, entry := range app.redisPage.Entries {
		searchable := strings.ToLower(strings.Join([]string{entry.Field, entry.Value, fmt.Sprintf("%g", entry.Score)}, " "))
		if filter == "" || strings.Contains(searchable, filter) {
			entries = append(entries, entry)
		}
	}
	ui.Row(c).Padding(4, 10, 7, 10).Gap(6).Children(func() {
		search := ui.TextInput(c, &app.redisEntryFilter).Height(32).Padding(4, 8).Placeholder(app.localized("搜索字段或值")).Label(app.localized("搜索字段或值")).Grow(1)
		search.Disabled(app.busy || app.dialogOpen)
		ui.Text(c, fmt.Sprintf(app.localized("已加载 %d 项"), len(entries))).TextColor(c.Theme().TextMuted).SingleLine()
		add := iconButton(c, app.localized("添加条目"), actionIcons["add"]).Width(32).Height(30).Padding(4)
		add.Disabled(app.busy || app.dialogOpen || app.openedReadOnly || app.redisInfo.Type == "")
		if add.Clicked() {
			app.redisEntryEditorOpen = true
			app.redisEntryField = ""
			app.redisValueDraft = ""
			app.redisEntryScore = "0"
		}
	})
	if app.redisEntryEditorOpen {
		app.redisEntryComposer(c)
	}
	if len(entries) == 0 {
		message := "此页没有元素。可能需要继续分页或键已被并发修改。"
		if filter != "" {
			message = "没有匹配的条目。"
		}
		ui.Text(c, app.localized(message)).TextColor(c.Theme().TextMuted).Padding(12, 14)
	}
	columns := redisEntryColumns(app, app.redisInfo.Type)
	ui.Table(c, &app.redisEntryList, columns, len(entries), func(row, col int) {
		entry := entries[row]
		switch app.redisInfo.Type {
		case "hash":
			switch col {
			case 0:
				ui.Text(c, entry.Field).SingleLine()
			case 1:
				ui.Text(c, entry.Value).SingleLine()
			case 2:
				ttl, expiringSoon, expired := app.redisKeyTTLPresentation(app.redisSelectedKey, app.redisInfo.TTL)
				color := c.Theme().TextMuted
				if expiringSoon || expired {
					color = c.Theme().Danger
				}
				ui.Text(c, ttl).TextColor(color).SingleLine()
			case 3:
				edit := iconButton(c, app.localized("编辑条目"), actionIcons["edit"]).Width(32).Height(28).Padding(4)
				edit.Disabled(app.busy || app.dialogOpen || app.openedReadOnly)
				if edit.Clicked() {
					app.redisEntryEditorOpen = true
					app.redisEntryField = entry.Field
					app.redisValueDraft = entry.Value
				}
			}
		case "list":
			if col == 0 {
				ui.Text(c, strconv.Itoa(row+1)).TextColor(c.Theme().TextMuted)
			} else {
				ui.Text(c, entry.Value).SingleLine()
			}
		case "set":
			ui.Text(c, entry.Value).SingleLine()
		case "zset":
			if col == 0 {
				ui.Text(c, entry.Value).SingleLine()
			} else {
				ui.Text(c, fmt.Sprintf("%g", entry.Score)).SingleLine()
			}
		case "stream":
			if col == 0 {
				ui.Text(c, entry.Field).SingleLine()
			} else {
				ui.Text(c, entry.Value).SingleLine()
			}
		default:
			ui.Text(c, entry.Value).SingleLine()
		}
	}).Grow(1)
	if app.redisValueCursor != "" {
		more := ui.Button(c, app.localized("加载更多值")).Padding(8, 14)
		more.Disabled(app.busy || app.dialogOpen)
		if more.Clicked() {
			app.readMoreRedisValue()
		}
	}
}

func (app *workbench) redisEntryComposer(c *ui.Context) {
	ui.Row(c).Padding(0, 14, 8, 14).Gap(6).Children(func() {
		switch app.redisInfo.Type {
		case "hash":
			field := ui.TextInput(c, &app.redisEntryField).Height(34).Placeholder(app.localized("字段名")).Label(app.localized("字段名")).Grow(1)
			field.Disabled(app.busy || app.dialogOpen || app.openedReadOnly)
		case "zset":
			score := ui.TextInput(c, &app.redisEntryScore).Height(34).Width(110).Placeholder(app.localized("分数")).Label(app.localized("分数"))
			score.Disabled(app.busy || app.dialogOpen || app.openedReadOnly)
		case "stream":
			field := ui.TextInput(c, &app.redisEntryField).Height(34).Placeholder(app.localized("字段名")).Label(app.localized("字段名")).Grow(1)
			field.Disabled(app.busy || app.dialogOpen || app.openedReadOnly)
		}
		value := ui.TextInput(c, &app.redisValueDraft).Height(34).Placeholder(app.localized("值")).Label(app.localized("值")).Grow(1)
		value.Disabled(app.busy || app.dialogOpen || app.openedReadOnly)
		save := iconPrimaryButton(c, app.localized("保存…"), actionIcons["save"]).Width(34).Height(32).Padding(4)
		save.Disabled(app.busy || app.dialogOpen || app.openedReadOnly)
		if save.Clicked() {
			app.writeRedisKey()
		}
		cancel := iconButton(c, app.localized("取消"), actionIcons["close"]).Width(32).Height(32).Padding(4)
		cancel.Disabled(app.busy || app.dialogOpen)
		if cancel.Clicked() {
			app.redisEntryEditorOpen = false
			app.redisEntryField = ""
			app.redisValueDraft = ""
		}
	})
}

func redisEntryColumns(app *workbench, kind string) []ui.TableColumn {
	switch kind {
	case "hash":
		return []ui.TableColumn{{Title: app.localized("字段"), Width: 180, MinWidth: 80}, {Title: app.localized("值"), MinWidth: 100}, {Title: "TTL", Width: 100, Fixed: true}, {Title: app.localized("操作"), Width: 64, Fixed: true}}
	case "list":
		return []ui.TableColumn{{Title: "#", Width: 54, Fixed: true}, {Title: app.localized("值"), MinWidth: 100}}
	case "set":
		return []ui.TableColumn{{Title: app.localized("成员"), MinWidth: 100}}
	case "zset":
		return []ui.TableColumn{{Title: app.localized("成员"), MinWidth: 100}, {Title: app.localized("分数"), Width: 120}}
	case "stream":
		return []ui.TableColumn{{Title: "ID", Width: 180, MinWidth: 100}, {Title: app.localized("字段 / 值"), MinWidth: 100}}
	default:
		return []ui.TableColumn{{Title: app.localized("值"), MinWidth: 100}}
	}
}

func redisTypeColor(c *ui.Context, kind string) ui.Color {
	switch kind {
	case "string":
		return c.Theme().Success
	case "hash":
		return c.Theme().Warning
	case "list":
		return ui.Hex("#3b82f6")
	case "set":
		return ui.Hex("#a855f7")
	case "zset":
		return ui.Hex("#f97316")
	case "stream":
		return ui.Hex("#14b8a6")
	case "json":
		return ui.Hex("#ec4899")
	default:
		return c.Theme().TextMuted
	}
}
