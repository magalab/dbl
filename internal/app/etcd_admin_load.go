package app

import (
	"time"

	"github.com/magalab/dbl/internal/connection"
)

func (app *workbench) loadEtcdDashboard(force bool) {
	admin := app.etcdAdmin
	if admin == nil || app.etcdDashboardLoading || (!force && app.etcdDashboardLoaded) {
		return
	}
	connectionID := app.openedConnectionID
	app.etcdDashboardLoading = true
	app.etcdDashboardError = ""
	go func() {
		ctx, cancel := app.withTaskTimeout(25 * time.Second)
		defer cancel()
		dashboard, err := admin.Dashboard(ctx)
		app.window.Update(func() {
			if app.openedConnectionID != connectionID || app.etcdAdmin != admin {
				return
			}
			app.etcdDashboardLoading = false
			app.etcdDashboardLoaded = true
			if err != nil {
				app.etcdDashboardError = err.Error()
				return
			}
			previous := app.etcdPreviousMetrics
			app.etcdMetricsRateAvailable = false
			app.etcdGRPCRequestRate, app.etcdGRPCFailureRate = 0, 0
			if previous.Available && dashboard.Metrics.Available && !previous.SampledAt.IsZero() && !dashboard.Metrics.SampledAt.IsZero() {
				seconds := dashboard.Metrics.SampledAt.Sub(previous.SampledAt).Seconds()
				if seconds > 0 {
					requests := max(0, dashboard.Metrics.GRPCRequestsTotal-previous.GRPCRequestsTotal)
					failures := max(0, dashboard.Metrics.GRPCFailuresTotal-previous.GRPCFailuresTotal)
					app.etcdGRPCRequestRate = requests / seconds
					if requests > 0 {
						app.etcdGRPCFailureRate = failures / requests * 100
					}
					app.etcdMetricsRateAvailable = true
				}
			}
			app.etcdPreviousMetrics = dashboard.Metrics
			app.etcdDashboard, app.etcdDashboardError = dashboard, ""
		})
	}()
}

func (app *workbench) loadEtcdAuthOverview() {
	admin := app.etcdAdmin
	if admin == nil || app.etcdAuthLoading || app.etcdAuthLoaded {
		return
	}
	connectionID := app.openedConnectionID
	app.etcdAuthLoading = true
	app.etcdAuthError = ""
	go func() {
		ctx, cancel := app.withTaskTimeout(20 * time.Second)
		defer cancel()
		overview, err := admin.AuthOverview(ctx)
		app.window.Update(func() {
			if app.openedConnectionID != connectionID || app.etcdAdmin != admin {
				return
			}
			app.etcdAuthLoading = false
			app.etcdAuthLoaded = true
			if err != nil {
				app.etcdAuthError = err.Error()
				return
			}
			app.etcdAuthOverview, app.etcdAuthError = overview, ""
			if app.etcdSelectedUser != "" && !containsString(overview.Users, app.etcdSelectedUser) {
				app.etcdSelectedUser, app.etcdUserDetails = "", connection.EtcdUser{}
			}
			if app.etcdSelectedRole != "" && !containsString(overview.Roles, app.etcdSelectedRole) {
				app.etcdSelectedRole, app.etcdRoleDetails = "", connection.EtcdRole{}
			}
		})
	}()
}

func (app *workbench) loadEtcdUserDetails(name string) {
	admin := app.etcdAdmin
	if admin == nil || name == "" {
		return
	}
	connectionID := app.openedConnectionID
	go func() {
		ctx, cancel := app.withTaskTimeout(15 * time.Second)
		defer cancel()
		user, err := admin.UserDetails(ctx, name)
		app.window.Update(func() {
			if app.openedConnectionID != connectionID || app.etcdAdmin != admin || app.etcdSelectedUser != name {
				return
			}
			if err != nil {
				app.reportError("读取 etcd 用户失败", err)
				return
			}
			app.etcdUserDetails = user
		})
	}()
}

func (app *workbench) loadEtcdRoleDetails(name string) {
	admin := app.etcdAdmin
	if admin == nil || name == "" {
		return
	}
	connectionID := app.openedConnectionID
	go func() {
		ctx, cancel := app.withTaskTimeout(15 * time.Second)
		defer cancel()
		role, err := admin.RoleDetails(ctx, name)
		app.window.Update(func() {
			if app.openedConnectionID != connectionID || app.etcdAdmin != admin || app.etcdSelectedRole != name {
				return
			}
			if err != nil {
				app.reportError("读取 etcd 角色失败", err)
				return
			}
			app.etcdRoleDetails = role
		})
	}()
}

func (app *workbench) refreshEtcdAuth() {
	if app.etcdAuthLoading || app.etcdAdmin == nil {
		return
	}
	app.etcdAuthLoaded = false
	app.etcdAuthOverview = connection.EtcdAuthOverview{}
	app.loadEtcdAuthOverview()
}

func (app *workbench) refreshEtcdDashboard() {
	app.loadEtcdDashboard(true)
}
