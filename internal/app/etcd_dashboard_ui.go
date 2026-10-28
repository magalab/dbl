package app

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
)

func (app *workbench) etcdDashboardWorkspace(c *ui.Context) {
	ui.Column(c).Fill().Children(func() {
		ui.Row(c).FillWidth().Padding(8, 12).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Text(c, app.localized("集群概览")).Bold().Role(ui.RoleHeading).Level(2).Grow(1)
			if app.etcdDashboard.Metrics.SampledAt.IsZero() == false {
				ui.Text(c, fmt.Sprintf(app.localized("采集于 %s"), app.etcdDashboard.Metrics.SampledAt.Local().Format("15:04:05"))).TextColor(c.Theme().TextMuted)
			}
			refresh := iconButton(c, app.localized("刷新仪表盘"), actionIcons["refresh"])
			refresh.Disabled(app.etcdDashboardLoading || app.busy || app.dialogOpen)
			if refresh.Clicked() {
				app.refreshEtcdDashboard()
			}
		})
		ui.Scroll(c).Fill().Children(func() {
			ui.Column(c).FillWidth().Padding(14).Gap(12).Children(func() {
				ui.Text(c, app.localized("可访问成员数仅表示当前客户端能够读取的成员状态，不代表 etcd 集群 Quorum 健康检查。")).TextColor(c.Theme().TextMuted)
				if app.etcdDashboardLoading {
					ui.Text(c, app.localized("正在读取集群状态…")).TextColor(c.Theme().TextMuted)
					return
				}
				if app.etcdDashboardError != "" {
					ui.Text(c, app.localized("读取仪表盘失败：")+app.etcdDashboardError).TextColor(c.Theme().Danger)
					return
				}
				app.etcdDashboardSummary(c)
				app.etcdDashboardMembers(c)
				app.etcdDashboardAlarms(c)
				app.etcdDashboardMetrics(c)
			})
		})
	})
}

func (app *workbench) etcdDashboardSummary(c *ui.Context) {
	dashboard := app.etcdDashboard
	reachable := 0
	leader := "—"
	var version string
	for _, member := range dashboard.Members {
		if member.Reachable {
			reachable++
			if version == "" {
				version = member.Version
			}
			if member.ReportedMemberID == member.LeaderID && member.LeaderID != 0 {
				leader = fmt.Sprintf("%d", member.LeaderID)
			}
		}
	}
	if version == "" {
		version = "—"
	}
	if len(dashboard.Members) == 0 {
		leader = "—"
	}
	accessibleLabel := app.localized("客户端可访问成员")
	if dashboard.MemberListErr != "" {
		accessibleLabel = app.localized("客户端可访问端点")
	}
	ui.Row(c).FillWidth().Gap(8).Children(func() {
		app.etcdDashboardStat(c, accessibleLabel, fmt.Sprintf("%d / %d", reachable, len(dashboard.Members)))
		app.etcdDashboardStat(c, app.localized("Leader ID"), leader)
		app.etcdDashboardStat(c, app.localized("Key revision"), fmt.Sprintf("%d", dashboard.Revision))
		app.etcdDashboardStat(c, app.localized("告警数"), fmt.Sprintf("%d", len(dashboard.Alarms)))
	})
	ui.Row(c).FillWidth().Gap(8).Children(func() {
		app.etcdDashboardStat(c, app.localized("集群 ID"), fmt.Sprintf("%d", dashboard.ClusterID))
		app.etcdDashboardStat(c, app.localized("etcd 版本"), version)
		auth := app.localized("未启用")
		if dashboard.AuthEnabled {
			auth = app.localized("已启用")
		}
		if dashboard.AuthStatusErr != "" {
			auth = app.localized("不可读取")
		}
		app.etcdDashboardStat(c, app.localized("认证"), auth)
		app.etcdDashboardStat(c, app.localized("认证 revision"), fmt.Sprintf("%d", dashboard.AuthRevision))
	})
	if dashboard.MemberListErr != "" {
		ui.Text(c, app.localized("读取成员列表失败：")+dashboard.MemberListErr).TextColor(c.Theme().Warning)
	}
}

func (app *workbench) etcdDashboardStat(c *ui.Context, label, value string) {
	ui.Column(c).Grow(1).MinWidth(130).Padding(10).Gap(5).Background(c.Theme().Surface).Border(1, c.Theme().Border).Radius(c.Theme().Radius).Children(func() {
		ui.Text(c, label).TextColor(c.Theme().TextMuted).SingleLine()
		ui.Text(c, value).Bold().SingleLine().Tooltip(value)
	})
}

func (app *workbench) etcdDashboardMembers(c *ui.Context) {
	dashboard := app.etcdDashboard
	ui.Column(c).FillWidth().Padding(10).Gap(7).Background(c.Theme().Surface).Border(1, c.Theme().Border).Radius(c.Theme().Radius).Children(func() {
		ui.Text(c, app.localized("端点 / 成员")).Bold().Role(ui.RoleHeading).Level(3)
		if len(dashboard.Members) == 0 {
			if dashboard.MemberListErr == "" {
				ui.Text(c, app.localized("没有返回成员信息。 ")).TextColor(c.Theme().TextMuted)
			}
			return
		}
		for _, member := range dashboard.Members {
			member := member
			name := member.Name
			if name == "" {
				name = fmt.Sprintf("member-%d", member.ID)
			}
			status := app.localized("不可访问")
			if member.Reachable {
				status = app.localized("可访问")
			}
			ui.Column(c).FillWidth().Padding(8).Gap(5).Border(1, c.Theme().Border).Radius(c.Theme().Radius).Children(func() {
				ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
					ui.Text(c, fmt.Sprintf("%s · ID %d", name, member.ID)).Bold().Grow(1).SingleLine().Tooltip(name)
					ui.Text(c, status).TextColor(c.Theme().TextMuted)
					if member.Reachable && !app.openedReadOnly && member.Endpoint != "" {
						compact := iconButton(c, app.localized("整理此成员后端数据库"), actionIcons["settings"]).Width(30).Height(30).Padding(4)
						compact.Disabled(app.busy || app.dialogOpen)
						if compact.Clicked() {
							app.confirmEtcdDefragment(member.Endpoint)
						}
					}
				})
				details := fmt.Sprintf(app.localized("版本 %s · Leader %d · revision %d · Raft term %d · index %d / applied %d"), member.Version, member.LeaderID, member.Revision, member.RaftTerm, member.RaftIndex, member.RaftAppliedIndex)
				ui.Text(c, details).TextColor(c.Theme().TextMuted).SingleLine().Tooltip(details)
				if member.Endpoint != "" {
					ui.Text(c, member.Endpoint).TextColor(c.Theme().TextMuted).SingleLine().Tooltip(member.Endpoint)
				}
				if member.BackendSize > 0 || member.BackendSizeQuota > 0 {
					ui.Text(c, fmt.Sprintf(app.localized("后端已用 %s / 数据库大小 %s / 配额 %s"), formatIECBytes(member.BackendSizeInUse), formatIECBytes(member.BackendSize), formatIECBytes(member.BackendSizeQuota))).TextColor(c.Theme().TextMuted)
				}
				if member.StatusError != "" {
					ui.Text(c, app.localized("读取状态失败：")+member.StatusError).TextColor(c.Theme().Warning)
				}
				for _, serverError := range member.ServerErrors {
					ui.Text(c, serverError).TextColor(c.Theme().Warning)
				}
				if len(member.ClientURLs) > 0 {
					ui.Text(c, app.localized("客户端 URL：")+strings.Join(member.ClientURLs, ", ")).TextColor(c.Theme().TextMuted)
				}
			})
		}
	})
}

func (app *workbench) etcdDashboardAlarms(c *ui.Context) {
	dashboard := app.etcdDashboard
	ui.Column(c).FillWidth().Padding(10).Gap(6).Background(c.Theme().Surface).Border(1, c.Theme().Border).Radius(c.Theme().Radius).Children(func() {
		ui.Text(c, app.localized("集群告警")).Bold().Role(ui.RoleHeading).Level(3)
		if dashboard.AlarmListErr != "" {
			ui.Text(c, app.localized("读取告警失败：")+dashboard.AlarmListErr).TextColor(c.Theme().Warning)
			return
		}
		if len(dashboard.Alarms) == 0 {
			ui.Text(c, app.localized("当前没有活动告警。 ")).TextColor(c.Theme().TextMuted)
			return
		}
		for _, alarm := range dashboard.Alarms {
			alarm := alarm
			ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Icon(c, actionIcons["warning"]).Size(16, 16).TextColor(c.Theme().Warning)
				ui.Text(c, fmt.Sprintf("%s · member %d", alarm.Type, alarm.MemberID)).Grow(1)
				if !app.openedReadOnly {
					disarm := iconButton(c, app.localized("请求解除告警"), actionIcons["check"])
					disarm.Disabled(app.busy || app.dialogOpen)
					if disarm.Clicked() {
						app.confirmEtcdAlarmDisarm(alarm)
					}
				}
			})
		}
	})
}

func (app *workbench) etcdDashboardMetrics(c *ui.Context) {
	metrics := app.etcdDashboard.Metrics
	ui.Column(c).FillWidth().Padding(10).Gap(8).Background(c.Theme().Surface).Border(1, c.Theme().Border).Radius(c.Theme().Radius).Children(func() {
		ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
			ui.Text(c, app.localized("Prometheus 运行指标")).Bold().Role(ui.RoleHeading).Level(3).Grow(1)
			if metrics.Endpoint != "" {
				ui.Text(c, metrics.Endpoint).TextColor(c.Theme().TextMuted).SingleLine().Tooltip(metrics.Endpoint)
			}
		})
		if !metrics.Available {
			message := app.localized("指标端点暂不可用。 ")
			if metrics.Error != "" {
				message += metrics.Error
			}
			ui.Text(c, message).TextColor(c.Theme().TextMuted)
			return
		}
		ui.Text(c, app.localized("指标来自 etcd /metrics；速率类数据需比较两次采样。 ")).TextColor(c.Theme().TextMuted)
		ui.Row(c).FillWidth().Gap(8).Children(func() {
			app.etcdDashboardStat(c, app.localized("运行时长"), formatDuration(metrics.UptimeSeconds))
			app.etcdDashboardStat(c, app.localized("后端使用率"), percent(metrics.BackendSizeInUse, metrics.BackendQuota))
			app.etcdDashboardStat(c, app.localized("驻留内存"), formatIECBytes(int64(metrics.ResidentMemoryBytes)))
			app.etcdDashboardStat(c, app.localized("Goroutine"), compactFloat(metrics.Goroutines))
		})
		ui.Row(c).FillWidth().Gap(8).Children(func() {
			app.etcdDashboardStat(c, app.localized("Leader 变更累计"), compactFloat(metrics.LeaderChanges))
			app.etcdDashboardStat(c, app.localized("待处理 Proposal"), compactFloat(metrics.ProposalsPending))
			app.etcdDashboardStat(c, app.localized("失败 Proposal"), compactFloat(metrics.ProposalsFailed))
			app.etcdDashboardStat(c, app.localized("gRPC 失败累计"), compactFloat(metrics.GRPCFailuresTotal))
		})
		requestRate, failureRate := "—", "—"
		if app.etcdMetricsRateAvailable {
			requestRate = fmt.Sprintf("%.1f/s", app.etcdGRPCRequestRate)
			failureRate = fmt.Sprintf("%.2f%%", app.etcdGRPCFailureRate)
		}
		ui.Row(c).FillWidth().Gap(8).Children(func() {
			app.etcdDashboardStat(c, app.localized("gRPC 请求速率"), requestRate)
			app.etcdDashboardStat(c, app.localized("gRPC 失败率"), failureRate)
			app.etcdDashboardStat(c, app.localized("已提交 Proposal"), compactFloat(metrics.ProposalsCommitted))
			app.etcdDashboardStat(c, app.localized("已应用 Proposal"), compactFloat(metrics.ProposalsApplied))
		})
		if metrics.Error != "" {
			ui.Text(c, metrics.Error).TextColor(c.Theme().Warning)
		}
	})
}

func formatDuration(seconds float64) string {
	if math.IsNaN(seconds) || math.IsInf(seconds, 0) || seconds < 0 {
		return "—"
	}
	duration := time.Duration(seconds * float64(time.Second))
	hours := int64(duration.Hours())
	if hours >= 24 {
		return fmt.Sprintf("%dd %dh", hours/24, (hours % 24))
	}
	return fmt.Sprintf("%dh %dm", hours, int64(duration.Minutes())%60)
}

func percent(used, quota float64) string {
	if quota <= 0 || math.IsNaN(used) || math.IsInf(used, 0) {
		return "—"
	}
	return fmt.Sprintf("%.1f%%", used/quota*100)
}

func compactFloat(value float64) string {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return "—"
	}
	if math.Abs(value) >= 1_000_000 {
		return fmt.Sprintf("%.1fM", value/1_000_000)
	}
	if math.Abs(value) >= 10_000 {
		return fmt.Sprintf("%.1fk", value/1_000)
	}
	return fmt.Sprintf("%.0f", value)
}

func formatIECBytes(value int64) string {
	if value < 0 {
		return "—"
	}
	units := []string{"B", "KiB", "MiB", "GiB", "TiB"}
	amount := float64(value)
	unit := 0
	for amount >= 1024 && unit < len(units)-1 {
		amount /= 1024
		unit++
	}
	if unit == 0 {
		return fmt.Sprintf("%d %s", value, units[unit])
	}
	return fmt.Sprintf("%.1f %s", amount, units[unit])
}
