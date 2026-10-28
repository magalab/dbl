package app

import (
	"fmt"
	"sort"
	"strings"

	"github.com/egoist/mygo/ui"
)

func (app *workbench) redisKeyListPane(c *ui.Context) {
	ui.Column(c).Fill().MinWidth(0).Label(app.localized("Redis 键列表")).Children(func() {
		ui.Row(c).Padding(8, 8, 5, 8).Gap(4).AlignItems(ui.Center).Children(func() {
			countLabel := fmt.Sprintf(app.localized("%d 个键"), len(app.redisKeys))
			if app.redisHasMore {
				countLabel = fmt.Sprintf(app.localized("已加载 %d 个键"), len(app.redisKeys))
			}
			ui.Text(c, countLabel).TextColor(c.Theme().TextMuted).SingleLine()
			if app.redisKeySelection.Len() > 0 {
				ui.Text(c, fmt.Sprintf(app.localized("已选择 %d 个键"), app.redisKeySelection.Len())).TextColor(c.Theme().TextMuted).SingleLine().Grow(1)
			} else {
				ui.Spacer(c).Grow(1)
			}
			selectAllLabel := app.localized("全选")
			if len(app.redisKeys) > 0 && app.redisKeySelection.Len() == len(app.redisKeys) {
				selectAllLabel = app.localized("取消全选")
			}
			selectAll := ui.Button(c, selectAllLabel).Padding(4, 7)
			selectAll.Disabled(app.busy || app.dialogOpen || len(app.redisKeys) == 0)
			if selectAll.Clicked() {
				app.selectAllRedisKeys()
			}
			if app.redisKeySelection.Len() > 0 && app.redisKeySelection.Len() < len(app.redisKeys) {
				clear := iconButton(c, app.localized("清除选择"), actionIcons["close"]).Width(30).Height(30).Padding(4)
				clear.Disabled(app.busy || app.dialogOpen)
				if clear.Clicked() {
					app.redisKeySelection.Clear()
				}
			}
			if app.redisKeySelection.Len() > 0 {
				remove := iconButton(c, fmt.Sprintf(app.localized("删除所选键 (%d)"), app.redisKeySelection.Len()), actionIcons["trash"]).Width(30).Height(30).Padding(4)
				remove.Disabled(app.busy || app.dialogOpen || app.openedReadOnly)
				if remove.Clicked() {
					app.confirmDeleteSelectedRedisKeys()
				}
			}
			refresh := iconButton(c, app.localized("刷新"), actionIcons["refresh"]).Width(30).Height(30).Padding(4)
			refresh.Disabled(app.busy || app.dialogOpen)
			if refresh.Clicked() {
				app.scanRedis(true)
			}
			add := iconButton(c, app.localized("新增 Redis 键"), actionIcons["add"]).Width(30).Height(30).Padding(4)
			add.Disabled(app.busy || app.dialogOpen || app.openedReadOnly)
			if add.Clicked() {
				app.beginRedisKeyCreation()
			}
		})
		ui.Row(c).Padding(0, 8, 8, 8).Gap(5).Children(func() {
			match := ui.TextInput(c, &app.redisMatch).Height(32).Padding(4, 8).Placeholder(app.localized("键前缀 / 匹配模式")).Label(app.localized("键前缀 / 匹配模式")).Grow(1)
			match.Disabled(app.busy || app.dialogOpen)
			search := iconButton(c, app.localized("搜索"), actionIcons["search"]).Width(30).Height(32).Padding(4)
			search.Disabled(app.busy || app.dialogOpen)
			if search.Clicked() {
				app.scanRedis(true)
			}
		})
		ui.Row(c).Padding(0, 8, 5, 8).Gap(4).AlignItems(ui.Center).Children(func() {
			flat := ui.Button(c, app.localized("平铺列表")).Padding(3, 7).Selected(!app.redisGroupByPrefix)
			flat.Disabled(app.busy || app.dialogOpen)
			if flat.Clicked() {
				app.cancelRedisDeleteConfirmation()
				app.redisGroupByPrefix = false
			}
			grouped := ui.Button(c, app.localized("前缀分组")).Padding(3, 7).Selected(app.redisGroupByPrefix)
			grouped.Disabled(app.busy || app.dialogOpen)
			if grouped.Clicked() {
				app.cancelRedisDeleteConfirmation()
				app.redisGroupByPrefix = true
			}
			if app.redisGroupByPrefix {
				separator := ui.TextInput(c, &app.redisGroupDelimiter).Width(48).Height(28).Padding(2, 5).Placeholder(":").Label(app.localized("分组分隔符"))
				separator.Disabled(app.busy || app.dialogOpen)
			}
		})
		ui.Scroll(c).FillWidth().Grow(1).MinHeight(0).Children(func() {
			ui.Column(c).FillWidth().Padding(2, 5).Gap(1).Children(func() {
				if len(app.redisKeys) == 0 && app.busy {
					ui.Text(c, app.localized("正在读取 Redis 键…")).TextColor(c.Theme().TextMuted).Padding(12, 8)
				} else if len(app.redisKeys) == 0 {
					ui.Text(c, app.localized("没有匹配的键。")).TextColor(c.Theme().TextMuted).Padding(12, 8)
				}
				if app.redisGroupByPrefix && app.redisGroupDelimiter != "" {
					for _, node := range buildRedisKeyTree(app.redisKeys, app.redisGroupDelimiter) {
						app.renderRedisKeyTreeNode(c, node, 0)
					}
				} else {
					for _, key := range app.redisKeys {
						app.redisKeyRow(c, key, key, 0)
					}
				}
				if app.redisHasMore {
					more := ui.Button(c, app.localized("继续扫描")).FillWidth()
					more.Disabled(app.busy || app.dialogOpen)
					if more.Clicked() {
						app.scanRedis(false)
					}
				}
			})
		})
	})
}

type redisKeyTreeNode struct {
	name     string
	path     string
	key      string
	hasKey   bool
	keys     []string
	children map[string]*redisKeyTreeNode
}

func buildRedisKeyTree(keys []string, delimiter string) []*redisKeyTreeNode {
	root := &redisKeyTreeNode{children: make(map[string]*redisKeyTreeNode)}
	for _, key := range keys {
		parts := strings.Split(key, delimiter)
		parent := root
		path := ""
		for index, part := range parts {
			if index == 0 {
				path = part
			} else {
				path += delimiter + part
			}
			node := parent.children[part]
			if node == nil {
				node = &redisKeyTreeNode{name: part, path: path, children: make(map[string]*redisKeyTreeNode)}
				parent.children[part] = node
			}
			node.keys = append(node.keys, key)
			if index == len(parts)-1 {
				node.key = key
				node.hasKey = true
			}
			parent = node
		}
	}
	return sortedRedisKeyTreeChildren(root)
}

func sortedRedisKeyTreeChildren(node *redisKeyTreeNode) []*redisKeyTreeNode {
	children := make([]*redisKeyTreeNode, 0, len(node.children))
	for _, child := range node.children {
		children = append(children, child)
	}
	sort.Slice(children, func(i, j int) bool { return children[i].name < children[j].name })
	return children
}

func (app *workbench) renderRedisKeyTreeNode(c *ui.Context, node *redisKeyTreeNode, depth int) {
	if len(node.children) == 0 {
		if node.hasKey {
			app.redisKeyRow(c, node.key, redisTreeNodeLabel(node.name), depth)
		}
		return
	}

	selectedCount := 0
	for _, key := range node.keys {
		if app.redisKeySelection.Has(key) {
			selectedCount++
		}
	}
	selected := selectedCount == len(node.keys) && len(node.keys) > 0
	rowActionClicked := false
	expanded, hasExpandedState := app.redisGroupExpanded[node.path]
	if !hasExpandedState {
		expanded = true
	}
	groupLabel := redisTreeNodeLabel(node.name)
	item := ui.Button(c, "").Label(fmt.Sprintf("%s (%d)", node.path, len(node.keys))).Tooltip(node.path).FillWidth().Padding(4, 6)
	item.Disabled(app.busy || app.dialogOpen)
	item.Children(func() {
		ui.Row(c).FillWidth().Gap(6).AlignItems(ui.Center).Children(func() {
			ui.Box(c).Width(float32(depth * 14)).Shrink(0)
			check := ui.Checkbox(c, &selected, "").Label(app.localized("选择分组") + " " + node.path)
			if selectedCount > 0 && selectedCount < len(node.keys) {
				check.Mixed()
			}
			check.Disabled(app.busy || app.dialogOpen)
			if check.Changed() {
				rowActionClicked = true
				for _, key := range node.keys {
					if selected {
						app.redisKeySelection.Add(key)
					} else {
						app.redisKeySelection.Remove(key)
					}
				}
			}
			arrow := "▸"
			if expanded {
				arrow = "▾"
			}
			ui.Text(c, arrow).TextColor(c.Theme().TextMuted).Width(12).SingleLine()
			ui.Icon(c, treeItemIcons["schema"]).Size(16, 16).TextColor(treeItemIconColors["schema"])
			ui.Text(c, groupLabel).SingleLine().Grow(1)
			ui.Text(c, fmt.Sprintf("(%d)", len(node.keys))).TextColor(c.Theme().TextMuted).SingleLine()
		})
	})
	if item.Clicked() && !rowActionClicked {
		if app.redisGroupExpanded == nil {
			app.redisGroupExpanded = make(map[string]bool)
		}
		app.redisGroupExpanded[node.path] = !expanded
	}
	if !expanded {
		return
	}
	if node.hasKey {
		app.redisKeyRow(c, node.key, redisTreeNodeLabel(node.name), depth+1)
	}
	for _, child := range sortedRedisKeyTreeChildren(node) {
		app.renderRedisKeyTreeNode(c, child, depth+1)
	}
}

func (app *workbench) redisKeyRow(c *ui.Context, key, label string, depth int) {
	if label == "" {
		label = "∅"
	}
	chosen := app.redisKeySelection.Has(key)
	confirmingDelete := app.redisDeleteConfirmKey == key
	rowActionClicked := false
	item := ui.Button(c, "").Label(key).Tooltip(key).FillWidth().Height(36).Padding(4, 6).Selected(key == app.redisSelectedKey)
	hovered := item.Hovered()
	item.Disabled(app.busy || app.dialogOpen)
	item.Children(func() {
		ui.Row(c).FillWidth().Gap(6).AlignItems(ui.Center).Children(func() {
			ui.Box(c).Width(float32(depth * 14)).Shrink(0)
			ui.Box(c).Width(18).Height(18).Shrink(0).Center().Children(func() {
				if hovered || chosen || confirmingDelete {
					check := ui.Checkbox(c, &chosen, "").Label(app.localized("选择键") + " " + key).Width(18).Height(18).Padding(0)
					check.Disabled(app.busy || app.dialogOpen)
					if check.Changed() {
						rowActionClicked = true
						if chosen {
							app.redisKeySelection.Add(key)
						} else {
							app.redisKeySelection.Remove(key)
						}
					}
				} else {
					ui.Icon(c, treeItemIcons["key"]).Size(16, 16).TextColor(c.Theme().TextMuted)
				}
			})
			ui.Text(c, label).SingleLine().Grow(1)
			if info, ok := app.redisKeyMetadata[key]; ok {
				typeColor := redisTypeColor(c, info.Type)
				ui.Text(c, info.Type).TextColor(typeColor).Padding(2, 6).Border(1, typeColor.Alpha(0.35)).Radius(20).SingleLine()
				ttl, expiringSoon, expired := app.redisKeyTTLPresentation(key, info.TTL)
				ttlColor := c.Theme().TextMuted
				if expired {
					ttlColor = c.Theme().Danger
				} else if expiringSoon {
					ttlColor = c.Theme().Danger
				} else if info.TTL > 0 {
					ttlColor = c.Theme().Warning
				}
				ui.Text(c, app.localized(ttl)).TextColor(ttlColor).Padding(2, 6).Border(1, ttlColor.Alpha(0.35)).Radius(20).SingleLine()
			} else {
				ui.Text(c, "…").TextColor(c.Theme().TextMuted)
			}
			if confirmingDelete {
				ui.Row(c).Width(58).Height(26).Gap(4).Shrink(0).Children(func() {
					confirm := iconButton(c, app.localized("确认删除")+" "+key, actionIcons["trash"]).Width(26).Height(26).Padding(3)
					confirm.TextColor(c.Theme().Danger).Background(c.Theme().Danger.Alpha(0.12)).Border(1, c.Theme().Danger.Alpha(0.5))
					confirm.Disabled(app.busy || app.dialogOpen || app.openedReadOnly)
					if confirm.Clicked() {
						rowActionClicked = true
						store, connectionID := app.keyStore, app.openedConnectionID
						app.cancelRedisDeleteConfirmation()
						app.deleteRedisKeyConfirmed(key, store, connectionID)
					}
					cancel := iconButton(c, app.localized("取消删除")+" "+key, actionIcons["close"]).Width(26).Height(26).Padding(3)
					cancel.Disabled(app.busy || app.dialogOpen)
					if cancel.Clicked() {
						rowActionClicked = true
						app.cancelRedisDeleteConfirmation()
					}
				})
			} else if hovered && !app.openedReadOnly {
				remove := iconButton(c, app.localized("删除 Redis 键")+" "+key, actionIcons["trash"]).Width(26).Height(26).Padding(3)
				remove.Disabled(app.busy || app.dialogOpen)
				if remove.Clicked() {
					rowActionClicked = true
					app.armRedisDeleteConfirmation(key)
				}
			} else {
				ui.Box(c).Width(26).Height(26).Shrink(0)
			}
		})
	})
	if item.Clicked() && !rowActionClicked {
		if app.redisDeleteConfirmKey != "" {
			app.cancelRedisDeleteConfirmation()
		}
		app.inspectRedisKey(key)
	}
}

func redisTreeNodeLabel(label string) string {
	if label == "" {
		return "∅"
	}
	return label
}
