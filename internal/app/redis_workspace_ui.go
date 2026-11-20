package app

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/magalab/dbl/internal/connection"
	"github.com/magalab/dbl/internal/databaseicons"
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

func (app *workbench) redisInstanceOverview(c *ui.Context) {
	id := app.openedConnectionID
	live := app.liveConnections[id]
	profile := live.profile
	if !live.redisInfoRead && !live.redisInfoLoading {
		app.refreshRedisInstance()
		live = app.liveConnections[id]
	}
	theme := c.Theme()
	ui.Column(c).Grow(1).FillWidth().MinHeight(0).AlignItems(ui.Center).Padding(16, 24).Children(func() {
		ui.Column(c).Grow(1).MinHeight(0).Width(760).MaxWidthPercent(100).Gap(10).Children(func() {
			ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Row(c).Grow(1).MinWidth(0).Gap(8).AlignItems(ui.Center).Children(func() {
					name, address := profile.Name, profileEndpoint(profile)
					if name == "" {
						name = "Redis"
					}
					if icon, color, ok := databaseicons.Icon(string(connection.DriverRedis)); ok {
						ui.Icon(c, icon).Size(24, 24).Shrink(0).TextColor(color)
					}
					ui.Text(c, name).MinWidth(0).SingleLine().Ellipsis("…").FontSize(theme.FontSize + 6).Bold().Role(ui.RoleHeading).Level(1)
					if address != "" {
						ui.Text(c, address).MinWidth(0).SingleLine().Ellipsis("…").TextColor(theme.TextMuted).Tooltip(address)
					}
				})
				ui.Text(c, app.localized("已连接")).TextColor(theme.Success)
				if iconButton(c, app.localized("刷新 Redis 指标"), actionIcons["refresh"]).BorderWidth(0).
					Disabled(live.redisInfoLoading).Clicked() {
					app.refreshRedisInstance()
				}
			})
			if live.redisInfoLoading {
				ui.Text(c, app.localized("正在读取 Redis 指标…")).TextColor(theme.TextMuted).Role(ui.RoleStatus)
			}
			if live.redisInfoError != "" {
				ui.Text(c, app.localized("无法读取 Redis 指标，请检查 INFO 权限。")).TextColor(theme.Warning)
				ui.Text(c, live.redisInfoError).TextColor(theme.TextMuted).Selectable()
			}
			if len(live.redisInfo) > 0 {
				app.redisInfoMetrics(c, live.redisInfo)
				app.redisInfoDetails(c, id, live.redisInfo)
			}
		})
	})
}

type redisMetric struct {
	label, value string
	icon         *ui.SVG
}

func redisMetrics(sections []connection.RedisInfoSection) []redisMetric {
	fields := make(map[string]string)
	keyCount := "—"
	var keys uint64
	for _, section := range sections {
		if strings.EqualFold(section.Name, "keyspace") {
			keyCount = "0"
			for _, field := range section.Fields {
				if !strings.HasPrefix(field.Name, "db") {
					continue
				}
				value, _, _ := strings.Cut(field.Value, ",")
				if count, ok := strings.CutPrefix(value, "keys="); ok {
					n, err := strconv.ParseUint(count, 10, 64)
					if err == nil {
						keys += n
					}
				}
			}
			keyCount = strconv.FormatUint(keys, 10)
		}
		for _, field := range section.Fields {
			fields[field.Name] = field.Value
		}
	}
	hitRate := "—"
	hits, hitErr := strconv.ParseUint(fields["keyspace_hits"], 10, 64)
	misses, missErr := strconv.ParseUint(fields["keyspace_misses"], 10, 64)
	if total := float64(hits) + float64(misses); hitErr == nil && missErr == nil && total > 0 {
		hitRate = fmt.Sprintf("%.2f%%", float64(hits)/total*100)
	}
	value := func(name string) string {
		if fields[name] != "" {
			return fields[name]
		}
		return "—"
	}
	return []redisMetric{
		{"主机总内存", value("total_system_memory_human"), actionIcons["system"]},
		{"Redis 已用内存", value("used_memory_human"), actionIcons["memory"]},
		{"累计命令数", value("total_commands_processed"), actionIcons["terminal"]},
		{"缓存命中率", hitRate, actionIcons["target"]},
		{"实例键数量", keyCount, treeItemIcons["key"]},
		{"客户端连接数", value("connected_clients"), treeItemIcons["users"]},
	}
}

func (app *workbench) redisInfoMetrics(c *ui.Context, sections []connection.RedisInfoSection) {
	theme := c.Theme()
	ui.Grid(c).Columns(3).GapX(8).GapY(8).FillWidth().Children(func() {
		for _, metric := range redisMetrics(sections) {
			label := app.localized(metric.label)
			ui.Row(c).FillWidth().MinHeight(36).Padding(6, 10).Gap(8).AlignItems(ui.Center).
				Background(theme.Surface).Border(1, theme.Border).Radius(theme.Radius).
				Role(ui.RoleGroup).Label(label).Tooltip(label).Children(func() {
				ui.Icon(c, metric.icon).Size(16, 16).Shrink(0).TextColor(theme.TextMuted)
				ui.Spacer(c)
				ui.Text(c, metric.value).SingleLine().Shrink(0).Bold().Selectable()
			})
		}
	})
}

func (app *workbench) redisInfoDetails(c *ui.Context, id string, sections []connection.RedisInfoSection) {
	theme := c.Theme()
	type detailState struct {
		id, section, search string
		list                ui.ListState
	}
	panel := ui.Column(c).Grow(1).FillWidth().MinHeight(0).Gap(8)
	state := ui.Local(panel, "redisInfoDetail", func() detailState { return detailState{} })
	if state.id != id {
		*state = detailState{id: id}
	}
	sectionsWithFields := make([]connection.RedisInfoSection, 0, len(sections))
	for _, section := range sections {
		if len(section.Fields) > 0 {
			sectionsWithFields = append(sectionsWithFields, section)
		}
	}
	if len(sectionsWithFields) == 0 {
		return
	}
	selected := 0
	for index, section := range sectionsWithFields {
		if section.Name == state.section {
			selected = index
			break
		}
		if strings.EqualFold(section.Name, "server") {
			selected = index
		}
	}
	panel.Children(func() {
		ui.Text(c, app.localized("Redis 指标详情")).Bold().Role(ui.RoleHeading).Level(2)
		if ui.SearchField(c, &state.search).FillWidth().Label(app.localized("搜索 Redis 指标")).
			Placeholder(app.localized("搜索 Redis 指标")).Changed() {
			state.list.ScrollTo(0, ui.Start)
		}
		query := strings.ToLower(strings.TrimSpace(state.search))
		const groupHeight, groupGap, groupPadding = 30, 2, 3
		detailHeight := float32(len(sectionsWithFields)*groupHeight + (len(sectionsWithFields)-1)*groupGap + 2*(groupPadding+1))
		// Two short sections still leave room for a usable metric viewport.
		ui.Row(c).Grow(1).MinHeight(0).MaxHeight(max(detailHeight, 112)).FillWidth().Gap(10).AlignItems(ui.Stretch).Children(func() {
			ui.Scroll(c).Width(156).MinHeight(0).Shrink(0).Padding(groupPadding).
				Border(1, theme.Border).Radius(theme.Radius).Clip().Children(func() {
				ui.Column(c).FillWidth().Gap(groupGap).Children(func() {
					for index, section := range sectionsWithFields {
						name := app.redisInfoSectionName(section.Name)
						active := query == "" && index == selected
						button := ui.Button(c, "").FillWidth().Height(groupHeight).Padding(3, 8).BorderWidth(0).
							Label(name).Selected(active)
						button.Children(func() {
							ui.Text(c, name).Grow(1).MinWidth(0).SingleLine().Ellipsis("…")
							count := ui.Text(c, fmt.Sprint(len(section.Fields)))
							if active {
								count.TextColor(theme.AccentText)
							} else {
								count.TextColor(theme.TextMuted)
							}
						})
						if button.Clicked() {
							state.section, state.search = section.Name, ""
							state.list.ScrollTo(0, ui.Start)
							selected = index
							query = ""
						}
					}
				})
			})
			app.redisInfoTable(c, sectionsWithFields, selected, query, &state.list)
		})
	})
}

func (app *workbench) redisInfoTable(c *ui.Context, sections []connection.RedisInfoSection, selected int, query string, list *ui.ListState) {
	theme := c.Theme()
	type metricRow struct {
		group string
		field connection.RedisInfoField
	}
	var rows []metricRow
	if query == "" {
		sections = sections[selected : selected+1]
	}
	for _, section := range sections {
		name := app.redisInfoSectionName(section.Name)
		groupMatches := strings.Contains(strings.ToLower(section.Name), query) || strings.Contains(strings.ToLower(name), query)
		for _, field := range section.Fields {
			if groupMatches || strings.Contains(strings.ToLower(field.Name), query) || strings.Contains(strings.ToLower(field.Value), query) {
				rows = append(rows, metricRow{name, field})
			}
		}
	}
	ui.Column(c).Grow(1).MinWidth(0).MinHeight(0).Border(1, theme.Border).Radius(theme.Radius).Clip().Children(func() {
		ui.Row(c).FillWidth().Height(30).Padding(0, 8).Gap(8).AlignItems(ui.Center).Background(theme.Surface).Children(func() {
			if query != "" {
				ui.Text(c, app.localized("分组")).WidthPercent(22).MinWidth(0).TextColor(theme.TextMuted)
			}
			ui.Text(c, app.localized("指标")).WidthPercent(41).MinWidth(0).TextColor(theme.TextMuted)
			ui.Text(c, app.localized("值")).Grow(1).MinWidth(0).TextColor(theme.TextMuted)
		})
		if len(rows) == 0 {
			ui.Text(c, app.localized("没有匹配的 Redis 指标")).Padding(12).TextColor(theme.TextMuted)
			return
		}
		ui.List(c, list, len(rows), func(i int) {
			entry := rows[i]
			row := ui.Row(c).FillWidth().Padding(5, 8).Gap(8).AlignItems(ui.Center)
			if i%2 == 1 {
				row.Background(theme.Surface.Mix(theme.Background, 0.5))
			}
			row.Children(func() {
				if query != "" {
					ui.Text(c, entry.group).WidthPercent(22).MinWidth(0).SingleLine().Ellipsis("…").TextColor(theme.TextMuted)
				}
				ui.Text(c, entry.field.Name).WidthPercent(41).MinWidth(0).SingleLine().Ellipsis("…").Tooltip(entry.field.Name)
				ui.Text(c, entry.field.Value).Grow(1).MinWidth(0).Selectable()
			})
		}).Grow(1).MinHeight(0).FillWidth().Clip()
	})
}
