package app

import (
	"fmt"

	"github.com/egoist/mygo/ui"
)

func (app *workbench) etcdLeasesWorkspace(c *ui.Context) {
	ui.Column(c).Fill().MinHeight(0).Padding(14, 12).Gap(12).Children(func() {
		ui.Row(c).FillWidth().Gap(12).AlignItems(ui.Center).Children(func() {
			ui.Column(c).Grow(1).Gap(4).Children(func() {
				ui.Text(c, app.localized("租约")).Bold().Role(ui.RoleHeading).Level(2)
				ui.Text(c, app.localized("租约到期后，其附着 Key 会自动删除。")).TextColor(c.Theme().TextMuted)
			})
			grant := iconPrimaryButton(c, app.localized("授予租约"), actionIcons["add"])
			grant.Disabled(app.busy || app.etcdLeasesLoading || app.dialogOpen || app.openedReadOnly)
			if grant.Clicked() {
				app.beginEtcdGrantLease()
			}
			refresh := iconButton(c, app.localized("刷新租约"), actionIcons["refresh"])
			refresh.Disabled(app.busy || app.etcdLeasesLoading || app.dialogOpen)
			if refresh.Clicked() {
				app.refreshEtcdLeases()
			}
		})
		ui.Split(c, &app.etcdPaneWidth, func() {
			app.etcdLeaseListPane(c)
		}, func() {
			app.etcdLeaseDetailPane(c)
		}).Grow(1)
	})
}

func (app *workbench) etcdLeaseListPane(c *ui.Context) {
	ui.Column(c).Fill().MinWidth(0).Label(app.localized("etcd 租约列表")).Padding(10).Gap(8).Background(c.Theme().Surface).Border(1, c.Theme().Border).Radius(c.Theme().Radius).Children(func() {
		ui.Text(c, fmt.Sprintf(app.localized("%d 个租约"), len(app.etcdLeases))).Bold()
		if app.etcdLeasesLoading && !app.etcdLeasesLoaded {
			ui.Text(c, app.localized("正在读取 etcd 租约…")).TextColor(c.Theme().TextMuted).Padding(8)
			return
		}
		if app.etcdLeaseListError != "" {
			ui.Text(c, app.localized("读取 etcd 租约失败：")+app.etcdLeaseListError).TextColor(c.Theme().Danger)
			return
		}
		if len(app.etcdLeases) == 0 {
			ui.Column(c).Fill().MinHeight(100).Justify(ui.Center).AlignItems(ui.Center).Border(1, c.Theme().Border).Radius(c.Theme().Radius).Children(func() {
				ui.Text(c, app.localized("当前会话没有已知 Lease")).TextColor(c.Theme().TextMuted)
			})
			return
		}
		ui.Scroll(c).FillWidth().Grow(1).MinHeight(0).Children(func() {
			ui.Column(c).FillWidth().Gap(4).Children(func() {
				for _, leaseID := range app.etcdLeases {
					leaseID := leaseID
					row := ui.Button(c, "").FillWidth().Padding(8, 10).Selected(leaseID == app.etcdSelectedLeaseID).Label(fmt.Sprintf(app.localized("Lease ID %d"), leaseID))
					row.Children(func() {
						ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
							ui.Icon(c, treeItemIcons["lease"]).Size(18, 18).TextColor(treeItemIconColors["lease"])
							ui.Text(c, fmt.Sprintf(app.localized("Lease ID %d"), leaseID)).Grow(1)
							if leaseID == app.etcdSelectedLeaseInfo.ID {
								ui.Text(c, app.etcdLeaseTTLText(app.etcdSelectedLeaseInfo.TTL)).TextColor(c.Theme().TextMuted)
							}
						})
					})
					row.Disabled(app.busy || app.etcdLeasesLoading || app.etcdLeaseDetailsLoading || app.dialogOpen)
					if row.Clicked() {
						app.loadEtcdLeaseDetails(leaseID)
					}
				}
			})
		})
	})
}

func (app *workbench) etcdLeaseDetailPane(c *ui.Context) {
	ui.Column(c).Fill().MinWidth(0).Padding(14).Gap(12).Background(c.Theme().Surface).Border(1, c.Theme().Border).Radius(c.Theme().Radius).Children(func() {
		if app.etcdLeaseDetailsLoading {
			ui.Column(c).Fill().Justify(ui.Center).AlignItems(ui.Center).Children(func() {
				ui.Text(c, app.localized("正在读取租约详情…")).TextColor(c.Theme().TextMuted)
			})
			return
		}
		if app.etcdLeaseDetailsError != "" {
			ui.Text(c, app.localized("读取租约详情失败：")+app.etcdLeaseDetailsError).TextColor(c.Theme().Danger)
			return
		}
		if app.etcdSelectedLeaseID <= 0 {
			ui.Column(c).Fill().Justify(ui.Center).AlignItems(ui.Center).Children(func() {
				ui.Text(c, app.localized("选择 Lease 查看详情")).TextColor(c.Theme().TextMuted)
			})
			return
		}
		info := app.etcdSelectedLeaseInfo
		ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Text(c, fmt.Sprintf(app.localized("Lease ID %d"), app.etcdSelectedLeaseID)).Bold().Role(ui.RoleHeading).Level(2).Grow(1)
			revoke := iconButton(c, app.localized("撤销租约…"), actionIcons["trash"])
			revoke.Disabled(app.busy || app.dialogOpen || app.openedReadOnly)
			if revoke.Clicked() {
				app.revokeEtcdLease()
			}
		})
		ui.Row(c).FillWidth().Gap(8).Children(func() {
			app.etcdLeaseStat(c, app.localized("剩余 TTL"), app.etcdLeaseTTLText(info.TTL))
			app.etcdLeaseStat(c, app.localized("授予 TTL"), fmt.Sprintf(app.localized("%d 秒"), info.GrantedTTL))
			app.etcdLeaseStat(c, app.localized("附着 Key"), fmt.Sprintf("%d", info.KeyCount))
		})
		ui.Text(c, app.localized("附着的 Key")).Bold().Padding(4, 0)
		if len(info.Keys) == 0 {
			ui.Text(c, app.localized("此租约当前没有附着 Key。 ")).TextColor(c.Theme().TextMuted)
		} else {
			ui.Scroll(c).FillWidth().Grow(1).MinHeight(0).Children(func() {
				ui.Column(c).FillWidth().Gap(3).Children(func() {
					for _, key := range info.Keys {
						ui.Text(c, key).Selectable().Padding(5, 8).Background(c.Theme().Background).Border(1, c.Theme().Border).Radius(c.Theme().Radius)
					}
				})
			})
		}
		if info.KeysTruncated {
			ui.Text(c, fmt.Sprintf(app.localized("仅显示前 %d 个 Key。"), len(info.Keys))).TextColor(c.Theme().TextMuted)
		}
	})
}

func (app *workbench) etcdLeaseStat(c *ui.Context, label, value string) {
	ui.Column(c).Grow(1).MinWidth(100).Padding(10).Gap(5).Background(c.Theme().Background).Border(1, c.Theme().Border).Radius(c.Theme().Radius).Children(func() {
		ui.Text(c, label).TextColor(c.Theme().TextMuted)
		ui.Text(c, value).Bold()
	})
}

func (app *workbench) etcdLeaseTTLText(ttl int64) string {
	if ttl < 0 {
		return app.localized("已过期")
	}
	return fmt.Sprintf(app.localized("%d 秒"), ttl)
}

func (app *workbench) etcdGrantLeaseModal(c *ui.Context) {
	if !app.etcdGrantLeaseOpen {
		return
	}
	if app.etcdGrantLeaseTTL == "" {
		app.etcdGrantLeaseTTL = "60"
	}
	if ui.Modal(c, &app.etcdGrantLeaseOpen, func() {
		ui.Column(c).Width(520).MaxWidthPercent(92).Padding(16).Gap(12).Background(c.Theme().Surface).Border(1, c.Theme().Border).Radius(c.Theme().Radius + 4).Children(func() {
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Text(c, app.localized("授予 etcd 租约")).Bold().Role(ui.RoleHeading).Level(2).Grow(1)
				close := iconButton(c, app.localized("关闭"), actionIcons["close"]).Size(30, 30).Padding(4)
				if close.Clicked() {
					app.closeEtcdGrantLeaseModal()
				}
			})
			ui.Text(c, app.localized("创建一个独立的租约，之后可将新 Key 绑定到此 Lease ID。")).TextColor(c.Theme().TextMuted)
			ttl := ui.TextInput(c, &app.etcdGrantLeaseTTL).Placeholder("60").Label(app.localized("TTL 秒数"))
			ttl.Disabled(app.busy || app.dialogOpen || app.openedReadOnly)
			ui.Row(c).Gap(8).Children(func() {
				ui.Spacer(c)
				cancel := iconButton(c, app.localized("取消"), actionIcons["close"])
				if cancel.Clicked() {
					app.closeEtcdGrantLeaseModal()
				}
				grant := iconPrimaryButton(c, app.localized("授予租约"), actionIcons["add"])
				grant.Disabled(app.busy || app.dialogOpen || app.openedReadOnly)
				if grant.Clicked() {
					app.grantEtcdLease()
				}
			})
		})
	}).Dismissed() {
		app.closeEtcdGrantLeaseModal()
	}
}
