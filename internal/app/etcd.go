package app

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/magalab/dbl/internal/connection"
	etcdadapter "github.com/magalab/dbl/internal/etcd"
	"github.com/magalab/dbl/internal/preferences"
	"github.com/magalab/dbl/internal/safeerror"
)

func (app *workbench) openEtcd(profile preferences.ConnectionProfile) {
	if app.busy || app.exporting || app.dialogOpen || profile.Driver != preferences.DriverEtcd {
		return
	}
	app.stageActiveDataWorkspaceDraft()
	app.busy = true
	app.status = app.localized("正在连接 etcd…")
	oldCursor, oldCancel := app.cursor, app.cancelQuery
	if oldCursor != nil {
		app.updateHistory(app.activeQuery.activeHistoryID, time.Time{}, "已切换数据源，停止后续读取")
	}
	app.cursor, app.cancelQuery, app.queryContext = nil, nil, nil
	if oldCancel != nil {
		oldCancel()
	}
	if app.etcdCancel != nil {
		app.etcdCancel()
	}
	ctx, cancel := app.withTaskTimeout(25 * time.Second)
	go func() {
		if oldCursor != nil {
			_ = oldCursor.Close()
		}
		defer cancel()
		adapter, err := (etcdadapter.Connector{Profile: profile, Vault: app.vault}).Connect(ctx)
		var config connection.Configuration
		if err == nil {
			var ok bool
			config, ok = adapter.(connection.Configuration)
			if !ok {
				err = errors.New("etcd adapter does not provide configuration capabilities")
			}
		}
		if err != nil && adapter != nil {
			_ = adapter.Close()
		}
		app.window.Update(func() {
			app.busy = false
			if err != nil {
				if errors.Is(err, context.Canceled) {
					app.status = app.localized("etcd 连接已取消。")
					return
				}
				app.reportError("连接 etcd 失败", err)
				return
			}
			app.registerLiveConnection("etcd:"+profile.ID, config, profile)
			app.store, app.keyStore, app.documentStore, app.etcdStore = nil, nil, nil, config
			app.etcdAdmin, _ = config.(connection.EtcdAdmin)
			app.etcdSection = app.etcdPendingSection
			if app.etcdSection == "" {
				app.etcdSection = "keys"
			}
			app.etcdToolbarSection = etcdToolbarSectionFor(app.etcdSection)
			app.etcdSidebarSection = app.etcdSection
			app.etcdPendingSection = ""
			app.etcdGlobalSearch = app.etcdSection == "global-search"
			app.etcdKeysLoaded = false
			app.etcdDashboard, app.etcdDashboardError = connection.EtcdDashboard{}, ""
			app.etcdPreviousMetrics = connection.EtcdMetrics{}
			app.etcdMetricsRateAvailable = false
			app.etcdAuthOverview, app.etcdAuthError = connection.EtcdAuthOverview{}, ""
			app.etcdDashboardLoaded, app.etcdAuthLoaded = false, false
			app.resetEtcdAdminState()
			app.etcdPrefix, app.etcdUsePrefix = "", true
			app.etcdEntries, app.etcdStartKey = nil, ""
			app.etcdSelectedKey, app.etcdSelected = "", connection.EtcdEntry{}
			app.etcdLeaseInfo = connection.EtcdLeaseInfo{}
			app.etcdLeaseInfoErr, app.etcdLeaseInfoLoading = "", false
			app.etcdDeleteReady, app.etcdWriteReady = false, false
			app.etcdWriteConfirmation = etcdWriteConfirmation{}
			app.openedDriver, app.openedConnectionID = connection.DriverEtcd, "etcd:"+profile.ID
			app.openedLabel, app.openedReadOnly = profile.Name+" · "+profile.Host, profile.ReadOnly
			app.openedEnvironment = profile.Environment
			app.status = app.localized("etcd 已连接。")
			app.activateConnectionWorkspace(app.openedConnectionID)
			app.setEtcdSection(app.etcdSection)
		})
	}()
}

func (app *workbench) resetEtcdAdminState() {
	app.etcdAuthTab = "users"
	app.etcdSelectedUser, app.etcdSelectedRole = "", ""
	app.etcdUserDetails, app.etcdRoleDetails = connection.EtcdUser{}, connection.EtcdRole{}
	app.closeEtcdCreateKeyModal()
	app.closeEtcdGrantLeaseModal()
	app.etcdLeases, app.etcdLeasesLoaded = nil, false
	app.etcdLeasesLoading, app.etcdLeaseListError = false, ""
	app.etcdSelectedLeaseID = 0
	app.etcdSelectedLeaseInfo = connection.EtcdLeaseInfo{}
	app.etcdLeaseDetailsLoading, app.etcdLeaseDetailsError = false, ""
	app.etcdCreateUserOpen, app.etcdCreateRoleOpen = false, false
	app.etcdGrantRoleOpen, app.etcdGrantPermissionOpen = false, false
	app.etcdNewUsername, app.etcdNewPassword, app.etcdChangePassword = "", "", ""
	app.etcdGrantRoleName, app.etcdNewRole = "", ""
	app.etcdPermissionKey = ""
	app.etcdPermissionType, app.etcdPermissionScope = "READWRITE", "exact"
}

func (app *workbench) refreshEtcd(reset bool) {
	if app.busy || app.etcdStore == nil {
		return
	}
	store := app.etcdStore
	prefix, usePrefix, start := app.etcdPrefix, app.etcdUsePrefix, app.etcdStartKey
	if app.etcdGlobalSearch {
		prefix, usePrefix = "", true
	}
	if reset {
		start = ""
	}
	app.busy = true
	app.status = app.localized("正在读取 etcd 键…")
	go func() {
		ctx, cancel := app.withTaskTimeout(20 * time.Second)
		defer cancel()
		page, err := store.RangeKeys(ctx, prefix, usePrefix, start, 100)
		app.window.Update(func() {
			app.busy = false
			if err != nil {
				app.reportError("读取 etcd 键失败", err)
				return
			}
			if reset {
				app.etcdEntries = page.Entries
				app.etcdKeysLoaded = true
				app.etcdSelectedKey, app.etcdSelected = "", connection.EtcdEntry{}
				app.etcdLeaseInfo = connection.EtcdLeaseInfo{}
				app.etcdLeaseInfoErr, app.etcdLeaseInfoLoading = "", false
				app.etcdWriteReady, app.etcdDeleteReady = false, false
				app.etcdWriteConfirmation = etcdWriteConfirmation{}
			} else {
				app.etcdEntries = append(app.etcdEntries, page.Entries...)
				app.etcdKeysLoaded = true
			}
			app.etcdStartKey = page.NextKey
			app.status = fmt.Sprintf("读取 %d 个 etcd 键。", len(page.Entries))
		})
	}()
}

func (app *workbench) selectEtcdKey(key string) {
	if app.busy || app.etcdStore == nil {
		return
	}
	store := app.etcdStore
	app.busy = true
	app.etcdLeaseInfo = connection.EtcdLeaseInfo{}
	app.etcdLeaseInfoErr = ""
	app.etcdLeaseInfoLoading = false
	go func() {
		ctx, cancel := app.withTaskTimeout(15 * time.Second)
		defer cancel()
		entry, err := store.GetKey(ctx, key)
		leaseInfo := connection.EtcdLeaseInfo{}
		leaseErr := ""
		if err == nil && entry.Lease > 0 {
			app.window.Update(func() { app.etcdLeaseInfoLoading = true })
			leaseInfo, err = store.LeaseTimeToLive(ctx, entry.Lease)
			if err != nil {
				leaseErr = safeerror.Display(err)
				err = nil
			}
		}
		app.window.Update(func() {
			app.busy = false
			if err != nil {
				app.reportError("读取 etcd 键详情失败", err)
				return
			}
			app.etcdSelectedKey, app.etcdSelected, app.etcdValue = key, entry, entry.Value
			app.etcdLeaseInfo, app.etcdLeaseInfoErr = leaseInfo, leaseErr
			app.etcdLeaseInfoLoading = false
			app.etcdLease = ""
			app.etcdWriteReady, app.etcdDeleteReady = false, false
			app.etcdWriteConfirmation = etcdWriteConfirmation{}
			app.status = app.localized("已加载 etcd 键详情。")
		})
	}()
}

func (app *workbench) selectedEtcdEntry() connection.EtcdEntry { return app.etcdSelected }

func parseEtcdLeaseID(value string) (int64, error) {
	if strings.TrimSpace(value) == "" {
		return 0, nil
	}
	parsed, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	if err != nil || parsed < 0 {
		return 0, errors.New("Lease ID 必须是非负整数")
	}
	return parsed, nil
}

func (app *workbench) previewEtcdWrite() {
	if app.busy || app.etcdStore == nil || app.openedReadOnly || app.etcdSelectedKey == "" {
		return
	}
	app.etcdWriteReady = false
	app.etcdWriteConfirmation = etcdWriteConfirmation{}
	if _, err := parseEtcdLeaseID(app.etcdLease); err != nil {
		app.reportError("etcd 写入失败", err)
		return
	}
	app.etcdWriteConfirmation = etcdWriteConfirmation{
		Key: app.etcdSelectedKey, Value: app.etcdValue, LeaseID: strings.TrimSpace(app.etcdLease),
		ExistingLeaseID: app.selectedEtcdEntry().Lease,
	}
	app.etcdWriteReady = true
}

func (app *workbench) commitEtcdValue() {
	if app.busy || app.etcdStore == nil || app.openedReadOnly || app.etcdSelectedKey == "" || !app.etcdWriteReady {
		return
	}
	confirmation := app.etcdWriteConfirmation
	if !confirmation.matches(app.etcdSelectedKey, app.etcdValue, strings.TrimSpace(app.etcdLease), app.selectedEtcdEntry().Lease) {
		app.etcdWriteReady = false
		app.etcdWriteConfirmation = etcdWriteConfirmation{}
		app.status = app.localized("写入内容已更改，请重新预览并确认。")
		return
	}
	lease, err := parseEtcdLeaseID(confirmation.LeaseID)
	if err != nil {
		app.etcdWriteReady = false
		app.etcdWriteConfirmation = etcdWriteConfirmation{}
		app.reportError("etcd 写入失败", err)
		return
	}
	store, key, value := app.etcdStore, confirmation.Key, confirmation.Value
	app.busy = true
	go func() {
		ctx, cancel := app.withTaskTimeout(20 * time.Second)
		defer cancel()
		entry, err := store.PutKey(ctx, key, value, lease)
		leaseInfo := connection.EtcdLeaseInfo{}
		leaseErr := ""
		if err == nil && entry.Lease > 0 {
			leaseInfo, err = store.LeaseTimeToLive(ctx, entry.Lease)
			if err != nil {
				leaseErr = safeerror.Display(err)
				err = nil
			}
		}
		app.window.Update(func() {
			app.busy = false
			app.etcdWriteReady = false
			app.etcdWriteConfirmation = etcdWriteConfirmation{}
			if err != nil {
				app.reportError("写入 etcd 失败", err)
				return
			}
			app.etcdSelected, app.etcdValue = entry, entry.Value
			app.etcdLeaseInfo, app.etcdLeaseInfoErr = leaseInfo, leaseErr
			app.etcdLeaseInfoLoading = false
			app.status = fmt.Sprintf("etcd 写入成功，修改 revision %d。", entry.ModRevision)
		})
	}()
}

func (app *workbench) previewDeleteEtcd() {
	key := app.etcdDeleteTarget()
	prefix := app.etcdDeletePrefix
	if app.busy || app.etcdStore == nil || app.openedReadOnly || key == "" || (prefix && app.etcdPrefix == "") {
		return
	}
	store := app.etcdStore
	app.busy = true
	go func() {
		ctx, cancel := app.withTaskTimeout(15 * time.Second)
		defer cancel()
		count, err := store.CountKeys(ctx, key, prefix)
		app.window.Update(func() {
			app.busy = false
			if err != nil {
				app.reportError("预览 etcd 删除失败", err)
				return
			}
			app.etcdDeleteCount, app.etcdDeleteReady = count, true
			app.etcdDeleteTargetKey, app.etcdDeleteTargetPrefix = key, prefix
		})
	}()
}

func (app *workbench) deleteEtcdKeys() {
	if app.busy || app.etcdStore == nil || app.openedReadOnly || !app.etcdDeleteReady || app.etcdDeleteTargetKey == "" {
		return
	}
	store, key, prefix := app.etcdStore, app.etcdDeleteTargetKey, app.etcdDeleteTargetPrefix
	app.busy = true
	go func() {
		ctx, cancel := app.withTaskTimeout(20 * time.Second)
		defer cancel()
		count, err := store.DeleteKeys(ctx, key, prefix)
		app.window.Update(func() {
			app.busy = false
			app.etcdDeleteReady = false
			if err != nil {
				app.reportError("删除 etcd 键失败", err)
				return
			}
			app.status = fmt.Sprintf("etcd 已删除 %d 项；预览计数可能因并发写入而变化。", count)
			if !prefix && app.etcdSelectedKey == key {
				app.etcdSelectedKey, app.etcdSelected = "", connection.EtcdEntry{}
				app.etcdLeaseInfo = connection.EtcdLeaseInfo{}
				app.etcdLeaseInfoErr, app.etcdLeaseInfoLoading = "", false
			}
			app.refreshEtcd(true)
		})
	}()
}

func (app *workbench) etcdDeleteTarget() string {
	if app.etcdDeletePrefix {
		return app.etcdPrefix
	}
	return app.etcdSelectedKey
}

func (app *workbench) startEtcdWatch() {
	if app.busy || app.etcdStore == nil || app.etcdWatching {
		return
	}
	revision := int64(0)
	if strings.TrimSpace(app.etcdWatchRevision) != "" {
		parsed, err := strconv.ParseInt(strings.TrimSpace(app.etcdWatchRevision), 10, 64)
		if err != nil || parsed < 0 {
			app.reportError("启动 Watch 失败", errors.New("revision 必须是非负整数"))
			return
		}
		revision = parsed
	}
	ctx, cancel := app.withTaskCancel()
	app.etcdCancel, app.etcdWatching = cancel, true
	app.etcdWatchEvents = nil
	key, prefix, store := app.etcdPrefix, app.etcdUsePrefix, app.etcdStore
	go func() {
		for event := range store.Watch(ctx, key, prefix, revision) {
			app.window.Update(func() {
				app.etcdWatchEvents = append(app.etcdWatchEvents, event)
				if len(app.etcdWatchEvents) > 100 {
					app.etcdWatchEvents = app.etcdWatchEvents[len(app.etcdWatchEvents)-100:]
				}
				if event.Err != nil {
					app.etcdWatching = false
					app.etcdCancel = nil
					app.status = app.localized("etcd Watch 已停止并返回错误。")
				}
			})
			if event.Err != nil {
				return
			}
		}
		app.window.Update(func() { app.etcdWatching = false; app.etcdCancel = nil })
	}()
}

func (app *workbench) stopEtcdWatch() {
	if app.etcdCancel != nil {
		app.etcdCancel()
	}
	app.etcdCancel = nil
	app.etcdWatching = false
	app.status = app.localized("etcd Watch 已停止。")
}
