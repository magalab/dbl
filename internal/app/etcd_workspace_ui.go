package app

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/magalab/dbl/internal/connection"
	"github.com/magalab/dbl/internal/safeerror"
)

func (app *workbench) setEtcdSection(section string) {
	if app.etcdStore == nil {
		return
	}
	changed := app.etcdSection != section
	app.etcdSection = section
	switch section {
	case "keys":
		if !app.etcdKeysLoaded {
			app.refreshEtcd(true)
		}
	case "global-search":
		if !app.etcdKeysLoaded {
			app.refreshEtcd(true)
		}
	case "auth":
		if changed && app.etcdAuthLoaded {
			app.refreshEtcdAuth()
		} else {
			app.loadEtcdAuthOverview()
		}
	case "leases":
		if !app.etcdLeasesLoaded {
			app.refreshEtcdLeases()
		}
	case "dashboard":
		app.loadEtcdDashboard(changed)
	case "maintenance":
		app.loadEtcdDashboard(changed)
	}
}

func (app *workbench) etcdWorkspace(c *ui.Context) {
	ui.Column(c).Fill().Children(func() {
		app.etcdWorkspaceNavigation(c)
		switch app.etcdSection {
		case "auth":
			app.etcdAuthWorkspace(c)
		case "leases":
			app.etcdLeasesWorkspace(c)
		case "maintenance":
			app.etcdMaintenanceWorkspace(c)
		case "dashboard":
			app.etcdDashboardWorkspace(c)
		default:
			app.etcdKeysWorkspace(c)
		}
	})
}

func (app *workbench) etcdWorkspaceNavigation(c *ui.Context) {
	ui.Row(c).FillWidth().Padding(8, 12, 8, 12).Gap(8).AlignItems(ui.Center).Children(func() {
		ui.Row(c).Gap(2).Padding(2).Border(1, c.Theme().Border).Radius(c.Theme().Radius).Children(func() {
			app.etcdToolbarTab(c, "keys", "Key", treeItemIcons["key"], "keys")
			app.etcdToolbarTab(c, "global-search", "全局搜索", actionIcons["search"], "global-search")
		})
		app.etcdToolbarTab(c, "maintenance", "维护", treeItemIcons["maintenance"], "maintenance")
		app.etcdToolbarTab(c, "monitor", "监视器", actionIcons["activity"], "dashboard")
		app.etcdToolbarTab(c, "leases", "租约", treeItemIcons["key"], "leases")
		ui.Spacer(c)
		app.etcdToolbarAction(c, "导出", actionIcons["download"], app.etcdExportDisabled(), app.exportEtcdKeys)
		app.etcdToolbarAction(c, "删除", actionIcons["trash"], app.etcdDeleteDisabled(), func() {
			app.etcdDeletePrefix = false
			app.previewDeleteEtcd()
		})
		app.etcdToolbarAction(c, "导入", actionIcons["upload"], app.etcdImportDisabled(), app.importEtcdKeys)
		sync := ui.Button(c, "").Label(app.localized("同步")).Tooltip(app.localized("etcd 同步暂不可用"))
		sync.Disabled(true)
		sync.Children(func() {
			ui.Row(c).Gap(7).AlignItems(ui.Center).Children(func() {
				ui.Icon(c, actionIcons["sync"]).Size(16, 16).TextColor(c.Theme().TextMuted)
				ui.Text(c, app.localized("同步"))
			})
		})
		if app.openedReadOnly {
			ui.Text(c, app.localized("只读")).TextColor(c.Theme().Warning).Padding(0, 6)
		}
	})
	ui.Box(c).FillWidth().Height(1).Background(c.Theme().Border)
}

func etcdToolbarSectionFor(section string) string {
	if section == "dashboard" {
		return "monitor"
	}
	return section
}

func (app *workbench) etcdToolbarTab(c *ui.Context, selected, label string, icon *ui.SVG, section string) {
	button := ui.Button(c, "").Label(app.localized(label)).Tooltip(app.localized(label)).Padding(8, 10).Selected(app.etcdToolbarSection == selected)
	button.Children(func() {
		ui.Row(c).Gap(7).AlignItems(ui.Center).Children(func() {
			ui.Icon(c, icon).Size(16, 16).TextColor(c.Theme().TextMuted)
			ui.Text(c, app.localized(label))
		})
	})
	button.Disabled(app.busy || app.exporting || app.dialogOpen)
	if button.Clicked() {
		app.etcdToolbarSection = selected
		if selected == "global-search" {
			app.etcdGlobalSearch = true
			app.etcdPrefix = ""
			app.etcdUsePrefix = true
			app.etcdStartKey = ""
			app.etcdKeysLoaded = false
			app.setEtcdSection(section)
			return
		}
		if selected == "keys" {
			app.etcdGlobalSearch = false
		}
		app.setEtcdSection(section)
	}
}

func (app *workbench) etcdToolbarAction(c *ui.Context, label string, icon *ui.SVG, disabled bool, action func()) {
	button := ui.Button(c, "").Label(app.localized(label)).Tooltip(app.localized(label)).Padding(8, 10)
	button.Children(func() {
		ui.Row(c).Gap(7).AlignItems(ui.Center).Children(func() {
			ui.Icon(c, icon).Size(16, 16).TextColor(c.Theme().TextMuted)
			ui.Text(c, app.localized(label))
		})
	})
	button.Disabled(disabled)
	if button.Clicked() {
		action()
	}
}

func (app *workbench) etcdKeysWorkspace(c *ui.Context) {
	ui.Column(c).Fill().MinHeight(0).Children(func() {
		ui.Row(c).FillWidth().Padding(8, 12, 4, 12).Gap(8).AlignItems(ui.Center).Children(func() {
			count := fmt.Sprintf(app.localized("%d 个键"), len(app.etcdEntries))
			if app.etcdStartKey != "" {
				count = fmt.Sprintf(app.localized("已加载 %d 个键"), len(app.etcdEntries))
			}
			ui.Text(c, count).TextColor(c.Theme().TextMuted).Grow(1).SingleLine()
			refresh := iconButton(c, app.localized("刷新 etcd 键"), actionIcons["refresh"]).Size(32, 32).Padding(5)
			refresh.Disabled(app.busy || app.dialogOpen)
			if refresh.Clicked() {
				app.refreshEtcd(true)
			}
			add := iconPrimaryButton(c, app.localized("新建 etcd 键"), actionIcons["add"]).Size(32, 32).Padding(5)
			add.Disabled(app.busy || app.dialogOpen || app.openedReadOnly)
			if add.Clicked() {
				app.beginEtcdCreateKey()
			}
		})
		ui.Row(c).FillWidth().Padding(4, 12, 8, 12).Gap(8).AlignItems(ui.Center).Children(func() {
			placeholder := "搜索 Key 路径，例如 /app/ 或 service"
			label := "搜索 etcd 键"
			if app.etcdGlobalSearch {
				placeholder = "搜索所有 Key"
				label = "全局搜索 etcd 键"
			}
			input := ui.TextInput(c, &app.etcdPrefix).Height(34).Padding(5, 10).
				Placeholder(app.localized(placeholder)).Label(app.localized(label)).Grow(1)
			input.Disabled(app.busy || app.dialogOpen)
			if !app.etcdGlobalSearch {
				prefix := ui.Checkbox(c, &app.etcdUsePrefix, app.localized("前缀匹配"))
				prefix.Disabled(app.busy || app.dialogOpen)
			}
			search := iconButton(c, app.localized("搜索键"), actionIcons["search"]).Size(34, 34).Padding(5)
			search.Disabled(app.busy || app.dialogOpen)
			if search.Clicked() {
				app.refreshEtcd(true)
			}
		})
		ui.Split(c, &app.etcdPaneWidth, func() {
			app.etcdKeyListPane(c)
		}, func() {
			app.etcdKeyDetailPane(c)
		}).Grow(1)
	})
}

func (app *workbench) etcdKeyListPane(c *ui.Context) {
	ui.Column(c).Fill().MinWidth(0).Label(app.localized("etcd 键列表")).Children(func() {
		ui.Scroll(c).FillWidth().Grow(1).MinHeight(0).Children(func() {
			ui.Column(c).FillWidth().Padding(3, 6).Gap(2).Children(func() {
				if app.busy && !app.etcdKeysLoaded {
					ui.Text(c, app.localized("正在读取 etcd 键…")).TextColor(c.Theme().TextMuted).Padding(12, 8)
				} else if !app.etcdKeysLoaded {
					ui.Text(c, app.localized("输入键或前缀并搜索。 ")).TextColor(c.Theme().TextMuted).Padding(12, 8)
				} else if len(app.filteredEtcdEntries()) == 0 {
					ui.Text(c, app.localized("没有匹配的键。 ")).TextColor(c.Theme().TextMuted).Padding(12, 8)
				}
				for _, entry := range app.filteredEtcdEntries() {
					entry := entry
					button := ui.Button(c, entry.Key).FillWidth().Padding(6, 8).Selected(entry.Key == app.etcdSelectedKey).Tooltip(entry.Key)
					button.Disabled(app.busy || app.dialogOpen)
					if button.Clicked() {
						app.selectEtcdKey(entry.Key)
					}
				}
				if app.etcdStartKey != "" {
					more := iconButton(c, app.localized("继续读取下一页"), actionIcons["nextBatch"])
					more.FillWidth().Height(32)
					more.Disabled(app.busy || app.dialogOpen)
					if more.Clicked() {
						app.refreshEtcd(false)
					}
				}
			})
		})
	})
}

func (app *workbench) filteredEtcdEntries() []connection.EtcdEntry {
	query := strings.ToLower(strings.TrimSpace(app.etcdPrefix))
	entries := make([]connection.EtcdEntry, 0, len(app.etcdEntries))
	for _, entry := range app.etcdEntries {
		if query != "" && !strings.Contains(strings.ToLower(entry.Key), query) {
			continue
		}
		entries = append(entries, entry)
	}
	return entries
}

func (app *workbench) etcdKeyDetailPane(c *ui.Context) {
	ui.Column(c).Fill().Padding(12).Gap(8).Children(func() {
		if app.etcdSelectedKey == "" {
			ui.Column(c).Fill().Justify(ui.Center).AlignItems(ui.Center).Children(func() {
				ui.Text(c, app.localized("选择一个键查看值")).TextColor(c.Theme().TextMuted)
			})
			return
		}
		entry := app.selectedEtcdEntry()
		ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Text(c, entry.Key).Bold().Role(ui.RoleHeading).Level(2).Selectable().Grow(1)
			remove := iconButton(c, app.localized("删除此键…"), actionIcons["trash"])
			remove.Disabled(app.busy || app.dialogOpen || app.openedReadOnly)
			if remove.Clicked() {
				app.previewDeleteEtcd()
			}
		})
		ui.Text(c, fmt.Sprintf(app.localized("创建修订 %d · 修改修订 %d · 版本 %d · Lease ID %d"), entry.CreateRevision, entry.ModRevision, entry.Version, entry.Lease)).TextColor(c.Theme().TextMuted)
		if entry.Lease > 0 {
			if app.etcdLeaseInfoLoading {
				ui.Text(c, app.localized("正在读取租约 TTL…")).TextColor(c.Theme().TextMuted)
			} else if app.etcdLeaseInfoErr != "" {
				ui.Text(c, app.localized("租约 TTL 暂不可用：")+app.etcdLeaseInfoErr).TextColor(c.Theme().TextMuted)
			} else if app.etcdLeaseInfo.TTL < 0 {
				ui.Text(c, app.localized("租约已过期；键值等待服务端清理。 ")).TextColor(c.Theme().Warning)
			} else {
				ui.Text(c, fmt.Sprintf(app.localized("租约剩余 TTL %d 秒 · 授予 TTL %d 秒"), app.etcdLeaseInfo.TTL, app.etcdLeaseInfo.GrantedTTL)).TextColor(c.Theme().TextMuted)
			}
		}
		if entry.ValueTruncated {
			ui.Text(c, app.localized("值超过 1 MiB，编辑内容已截断。为避免覆盖，禁止保存。")).TextColor(c.Theme().Warning)
		}
		value := ui.TextInput(c, &app.etcdValue).FillWidth().Grow(1).Placeholder(app.localized("键值")).Label(app.localized("值"))
		value.Disabled(app.busy || app.dialogOpen || app.openedReadOnly || entry.ValueTruncated)
		lease := ui.TextInput(c, &app.etcdLease).Width(180).Placeholder("0").Label(app.localized("Lease ID（可选）"))
		lease.Disabled(app.busy || app.dialogOpen || app.openedReadOnly)
		if !app.openedReadOnly && !entry.ValueTruncated {
			ui.Row(c).Gap(6).Children(func() {
				preview := iconButton(c, app.localized("预览写入"), actionIcons["edit"])
				preview.Disabled(app.busy || app.dialogOpen)
				if preview.Clicked() {
					app.previewEtcdWrite()
				}
				if app.etcdWriteReady {
					confirmation := app.etcdWriteConfirmation
					if !confirmation.matches(app.etcdSelectedKey, app.etcdValue, strings.TrimSpace(app.etcdLease), app.selectedEtcdEntry().Lease) {
						ui.Text(c, app.localized("待确认内容已变化，请重新预览写入。 ")).TextColor(c.Theme().Warning)
					} else {
						ui.Text(c, app.localized("写入目标：")+confirmation.Key).TextColor(c.Theme().TextMuted)
						if confirmation.ExistingLeaseID > 0 && confirmation.LeaseID != strconv.FormatInt(confirmation.ExistingLeaseID, 10) {
							if confirmation.LeaseID == "" {
								ui.Text(c, fmt.Sprintf(app.localized("写入后将移除现有租约（Lease ID %d）"), confirmation.ExistingLeaseID)).TextColor(c.Theme().Warning)
							} else {
								ui.Text(c, fmt.Sprintf(app.localized("写入后将替换现有租约（Lease ID %d）为 Lease ID %s"), confirmation.ExistingLeaseID, confirmation.LeaseID)).TextColor(c.Theme().Warning)
							}
						}
						ui.Text(c, fmt.Sprintf(app.localized("待写入值（%d 字节）"), len(confirmation.Value))).TextColor(c.Theme().TextMuted)
						ui.Text(c, truncateText(confirmation.Value, 4096)).Selectable()
						commit := iconPrimaryButton(c, app.localized("确认写入"), actionIcons["save"])
						commit.Disabled(app.busy || app.dialogOpen)
						if commit.Clicked() {
							app.commitEtcdValue()
						}
					}
				}
			})
			ui.Row(c).Gap(6).Children(func() {
				deletePrefix := ui.Checkbox(c, &app.etcdDeletePrefix, app.localized("按前缀删除"))
				deletePrefix.Disabled(app.busy || app.dialogOpen)
				previewDelete := iconButton(c, app.localized("预览删除影响"), actionIcons["trash"])
				previewDelete.Disabled(app.busy || app.dialogOpen || app.etcdDeleteTarget() == "" || (app.etcdDeletePrefix && app.etcdPrefix == ""))
				if previewDelete.Clicked() {
					app.previewDeleteEtcd()
				}
			})
			if app.etcdDeleteReady {
				if app.etcdDeleteTargetPrefix {
					ui.Text(c, fmt.Sprintf(app.localized("将删除前缀 %q 匹配的 %d 个键。"), app.etcdDeleteTargetKey, app.etcdDeleteCount)).TextColor(c.Theme().Warning)
				} else {
					ui.Text(c, fmt.Sprintf(app.localized("将删除键 %q。"), app.etcdDeleteTargetKey)).TextColor(c.Theme().Warning)
				}
				remove := iconPrimaryButton(c, app.localized("确认删除"), actionIcons["trash"])
				remove.Disabled(app.busy || app.dialogOpen || app.openedReadOnly)
				if remove.Clicked() {
					app.deleteEtcdKeys()
				}
			}
		}
		app.etcdWatchControls(c)
	})
}

func (app *workbench) etcdWatchControls(c *ui.Context) {
	ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
		ui.Text(c, app.localized("Watch")).Bold()
		revision := ui.TextInput(c, &app.etcdWatchRevision).Width(200).Placeholder(app.localized("留空从当前开始；或输入起始 revision")).Label(app.localized("起始 revision"))
		revision.Disabled(app.busy || app.dialogOpen || app.etcdWatching)
		if app.etcdWatching {
			stop := iconButton(c, app.localized("停止 Watch"), actionIcons["stop"])
			if stop.Clicked() {
				app.stopEtcdWatch()
			}
		} else {
			start := iconButton(c, app.localized("开始 Watch 当前键/前缀"), actionIcons["play"])
			start.Disabled(app.busy || app.dialogOpen)
			if start.Clicked() {
				app.startEtcdWatch()
			}
		}
	})
	ui.Scroll(c).Height(150).Children(func() {
		ui.Column(c).FillWidth().Gap(3).Children(func() {
			for _, event := range app.etcdWatchEvents {
				line := fmt.Sprintf("%s · revision %d · %s", event.Type, event.Revision, event.Key)
				if event.Err != nil {
					line = app.localized("Watch 错误：") + safeerror.Display(event.Err)
				} else {
					line += " → " + event.Value
					if event.ValueTruncated {
						line += app.localized("…（值已截断，超过 1 MiB）")
					}
				}
				ui.Text(c, line).Selectable()
			}
		})
	})
}
