package app

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/magalab/dbl/internal/connection"
)

func (app *workbench) beginEtcdGrantLease() {
	app.etcdGrantLeaseTTL = "60"
	app.etcdGrantLeaseOpen = true
}

func (app *workbench) closeEtcdGrantLeaseModal() {
	app.etcdGrantLeaseOpen = false
	app.etcdGrantLeaseTTL = ""
}

func (app *workbench) grantEtcdLease() {
	ttlSeconds, err := strconv.ParseInt(strings.TrimSpace(app.etcdGrantLeaseTTL), 10, 64)
	if err != nil || ttlSeconds <= 0 {
		app.status = app.localized("TTL 必须是正整数秒。")
		return
	}
	admin, ok := app.etcdStore.(connection.EtcdLeaseAdmin)
	if !ok || app.etcdStore == nil {
		app.reportError("授予 etcd 租约失败", errors.New(app.localized("当前连接不支持租约管理。")))
		return
	}
	detail := fmt.Sprintf(app.localized("将创建一个 %d 秒后到期的租约。"), ttlSeconds)
	app.requestConfirmation("授予 etcd 租约", "确认创建此租约？", detail, []string{"取消", "授予"}, 1, func(choice int) {
		if choice != 1 {
			return
		}
		app.closeEtcdGrantLeaseModal()
		app.busy = true
		app.status = app.localized("正在授予 etcd 租约…")
		connectionID := app.openedConnectionID
		go func() {
			ctx, cancel := app.withTaskTimeout(queryTimeout)
			defer cancel()
			lease, err := admin.GrantLease(ctx, ttlSeconds)
			app.window.Update(func() {
				if app.openedConnectionID != connectionID || app.etcdStore == nil {
					return
				}
				app.busy = false
				if err != nil {
					app.reportError("授予 etcd 租约失败", err)
					return
				}
				app.etcdSelectedLeaseID = lease.ID
				app.status = fmt.Sprintf(app.localized("etcd 租约已创建：Lease ID %d"), lease.ID)
				app.refreshEtcdLeases()
			})
		}()
	})
}

func (app *workbench) revokeEtcdLease() {
	leaseID := app.etcdSelectedLeaseID
	if leaseID <= 0 || app.openedReadOnly || app.busy || app.dialogOpen {
		return
	}
	admin, ok := app.etcdStore.(connection.EtcdLeaseAdmin)
	if !ok || app.etcdStore == nil {
		app.reportError("撤销 etcd 租约失败", errors.New(app.localized("当前连接不支持租约管理。")))
		return
	}
	info := app.etcdSelectedLeaseInfo
	lines := []string{
		fmt.Sprintf(app.localized("Lease ID：%d"), leaseID),
		fmt.Sprintf(app.localized("附着 Key 数：%d"), info.KeyCount),
		app.localized("撤销后，租约上的所有 Key 都会被立即删除。"),
	}
	for i, key := range info.Keys {
		if i >= 20 {
			break
		}
		lines = append(lines, key)
	}
	if info.KeyCount > 20 {
		lines = append(lines, fmt.Sprintf(app.localized("还有 %d 个 Key 未显示。"), info.KeyCount-20))
	}
	app.requestConfirmation("撤销 etcd 租约", "确认撤销此租约？", strings.Join(lines, "\n"), []string{"取消", "撤销租约"}, 1, func(choice int) {
		if choice != 1 {
			return
		}
		app.busy = true
		app.status = app.localized("正在撤销 etcd 租约…")
		connectionID := app.openedConnectionID
		go func() {
			ctx, cancel := app.withTaskTimeout(queryTimeout)
			defer cancel()
			err := admin.RevokeLease(ctx, leaseID)
			app.window.Update(func() {
				if app.openedConnectionID != connectionID || app.etcdStore == nil {
					return
				}
				app.busy = false
				if err != nil {
					app.reportError("撤销 etcd 租约失败", err)
					return
				}
				app.etcdSelectedLeaseID = 0
				app.etcdSelectedLeaseInfo = connection.EtcdLeaseInfo{}
				app.status = app.localized("etcd 租约已撤销。")
				app.refreshEtcdLeases()
			})
		}()
	})
}
