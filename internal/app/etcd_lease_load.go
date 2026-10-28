package app

import (
	"time"

	"github.com/magalab/dbl/internal/connection"
	"github.com/magalab/dbl/internal/safeerror"
)

func (app *workbench) refreshEtcdLeases() {
	store := app.etcdStore
	if store == nil || app.etcdLeasesLoading {
		return
	}
	admin, ok := store.(connection.EtcdLeaseAdmin)
	if !ok {
		app.etcdLeasesLoaded = true
		app.etcdLeaseListError = app.localized("当前连接不支持租约管理。")
		return
	}
	connectionID := app.openedConnectionID
	selectedID := app.etcdSelectedLeaseID
	app.etcdLeasesLoading = true
	app.etcdLeaseListError = ""
	go func() {
		ctx, cancel := app.withTaskTimeout(20 * time.Second)
		defer cancel()
		leases, err := admin.ListLeases(ctx)
		var detail connection.EtcdLeaseInfo
		var detailErr error
		if err == nil {
			if selectedID == 0 || !containsLeaseID(leases, selectedID) {
				selectedID = 0
				if len(leases) > 0 {
					selectedID = leases[0]
				}
			}
			if selectedID > 0 {
				detail, detailErr = admin.LeaseDetails(ctx, selectedID)
			}
		}
		app.window.Update(func() {
			if app.openedConnectionID != connectionID || app.etcdStore != store {
				return
			}
			app.etcdLeasesLoading = false
			app.etcdLeasesLoaded = true
			if err != nil {
				app.etcdLeaseListError = safeerror.Display(err)
				return
			}
			app.etcdLeases = leases
			app.etcdSelectedLeaseID = selectedID
			app.etcdSelectedLeaseInfo = detail
			app.etcdLeaseDetailsError = ""
			if detailErr != nil {
				app.etcdLeaseDetailsError = safeerror.Display(detailErr)
			}
		})
	}()
}

func (app *workbench) loadEtcdLeaseDetails(leaseID int64) {
	store := app.etcdStore
	admin, ok := store.(connection.EtcdLeaseAdmin)
	if !ok || store == nil || leaseID <= 0 || app.etcdLeaseDetailsLoading {
		return
	}
	connectionID := app.openedConnectionID
	app.etcdSelectedLeaseID = leaseID
	app.etcdSelectedLeaseInfo = connection.EtcdLeaseInfo{}
	app.etcdLeaseDetailsError = ""
	app.etcdLeaseDetailsLoading = true
	go func() {
		ctx, cancel := app.withTaskTimeout(15 * time.Second)
		defer cancel()
		detail, err := admin.LeaseDetails(ctx, leaseID)
		app.window.Update(func() {
			if app.openedConnectionID != connectionID || app.etcdStore != store || app.etcdSelectedLeaseID != leaseID {
				return
			}
			app.etcdLeaseDetailsLoading = false
			if err != nil {
				app.etcdLeaseDetailsError = safeerror.Display(err)
				return
			}
			app.etcdSelectedLeaseInfo = detail
		})
	}()
}

func containsLeaseID(leases []int64, wanted int64) bool {
	for _, leaseID := range leases {
		if leaseID == wanted {
			return true
		}
	}
	return false
}
