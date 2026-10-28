package app

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/magalab/dbl/internal/connection"
)

func (app *workbench) confirmEtcdKeyCreation() {
	key := app.etcdNewKey
	value := app.etcdNewValue
	if app.etcdNewValueEditor != nil {
		value = app.etcdNewValueEditor.Buffer.String()
	}
	if strings.TrimSpace(key) == "" {
		app.status = app.localized("请输入键名。")
		return
	}
	if len(value) > 1<<20 {
		app.status = app.localized("etcd 值不能超过 1 MiB。")
		return
	}
	policy := app.etcdNewLeasePolicy
	if policy == "" {
		policy = "permanent"
	}
	var lease int64
	var ttlSeconds int64
	switch policy {
	case "permanent":
	case "existing":
		parsed, err := parseEtcdLeaseID(app.etcdNewLease)
		if err != nil || parsed == 0 {
			app.status = app.localized("请输入有效的正整数 Lease ID。")
			return
		}
		lease = parsed
	case "ttl":
		parsed, err := strconv.ParseInt(strings.TrimSpace(app.etcdNewLeaseTTL), 10, 64)
		if err != nil || parsed <= 0 {
			app.status = app.localized("TTL 必须是正整数秒。")
			return
		}
		ttlSeconds = parsed
	default:
		app.status = app.localized("请选择有效期策略。")
		return
	}
	store := app.etcdStore
	if store == nil || app.openedReadOnly || app.busy || app.dialogOpen {
		return
	}
	creator, ok := store.(connection.EtcdKeyCreator)
	if !ok {
		app.reportError("创建 etcd 键失败", errors.New("etcd adapter does not support atomic key creation"))
		return
	}
	var ttlWriter connection.EtcdKeyTTLWriter
	if ttlSeconds > 0 {
		var supportsTTL bool
		ttlWriter, supportsTTL = store.(connection.EtcdKeyTTLWriter)
		if !supportsTTL {
			app.reportError("创建 etcd 键失败", errors.New("etcd adapter does not support expiring key creation"))
			return
		}
	}
	valuePreview := fmt.Sprintf(app.localized("值预览（%d 字节；最多显示 4096 字节）"), len(value))
	leaseDetail := app.localized("长期有效，不绑定租约")
	if ttlSeconds > 0 {
		leaseDetail = fmt.Sprintf(app.localized("新建临时租约，%d 秒后过期"), ttlSeconds)
	} else if lease > 0 {
		leaseDetail = fmt.Sprintf(app.localized("绑定已有 Lease ID：%d"), lease)
	}
	detail := fmt.Sprintf("%s: %s\n%s: %s\n%s", app.localized("键名"), key, valuePreview, truncateText(value, 4096), leaseDetail)
	app.requestConfirmation("创建 etcd 键", "确认将此键和值写入 etcd？", detail, []string{"取消", "创建"}, 1, func(choice int) {
		if choice != 1 {
			return
		}
		app.busy = true
		app.status = app.localized("正在创建 etcd 键…")
		go func() {
			ctx, cancel := app.withTaskTimeout(queryTimeout)
			defer cancel()
			var err error
			if ttlSeconds > 0 {
				_, err = ttlWriter.CreateKeyWithTTL(ctx, key, value, ttlSeconds)
			} else {
				_, err = creator.CreateKey(ctx, key, value, lease)
			}
			app.window.Update(func() {
				app.busy = false
				if err != nil {
					app.reportError("创建 etcd 键失败", err)
					return
				}
				app.closeEtcdCreateKeyModal()
				app.status = app.localized("etcd 键已创建。")
				app.refreshEtcd(true)
			})
		}()
	})
}

func (app *workbench) confirmEtcdAdminMutation(title, message, detail string, mutate func(context.Context, connection.EtcdAdmin) error, after func()) {
	admin := app.etcdAdmin
	if admin == nil || app.openedReadOnly || app.busy || app.dialogOpen {
		return
	}
	app.requestConfirmation(title, message, detail, []string{"取消", "确认"}, 1, func(choice int) {
		if choice != 1 {
			return
		}
		app.busy = true
		app.status = app.localized("正在应用 etcd 管理操作…")
		go func() {
			ctx, cancel := app.withTaskTimeout(queryTimeout)
			defer cancel()
			err := mutate(ctx, admin)
			app.window.Update(func() {
				app.busy = false
				if err != nil {
					app.reportError("etcd 管理操作失败", err)
					return
				}
				app.status = app.localized("etcd 管理操作已完成。")
				if after != nil {
					after()
				}
			})
		}()
	})
}

func (app *workbench) createEtcdUser() {
	name, password := strings.TrimSpace(app.etcdNewUsername), app.etcdNewPassword
	if name == "" || password == "" {
		app.status = app.localized("请输入用户名和密码。")
		return
	}
	app.etcdNewPassword = ""
	app.etcdCreateUserOpen = false
	app.confirmEtcdAdminMutation("创建 etcd 用户", "确认创建此用户？", app.localized("用户名：")+name+"\n"+app.localized("密码只会临时发送给 etcd，不会写入连接配置。"), func(ctx context.Context, admin connection.EtcdAdmin) error {
		return admin.CreateUser(ctx, name, password)
	}, func() {
		app.etcdNewUsername = ""
		app.refreshEtcdAuth()
	})
}

func (app *workbench) deleteEtcdUser(name string) {
	name = strings.TrimSpace(name)
	app.confirmEtcdAdminMutation("删除 etcd 用户", "确认删除此用户？", app.localized("用户名：")+name+"\n"+app.localized("删除后该用户不能再通过 etcd 认证。"), func(ctx context.Context, admin connection.EtcdAdmin) error {
		return admin.DeleteUser(ctx, name)
	}, func() {
		app.etcdSelectedUser = ""
		app.etcdUserDetails = connection.EtcdUser{}
		app.refreshEtcdAuth()
	})
}

func (app *workbench) changeEtcdUserPassword() {
	name, password := app.etcdSelectedUser, app.etcdChangePassword
	if name == "" || password == "" {
		app.status = app.localized("请输入新密码。")
		return
	}
	app.etcdChangePassword = ""
	app.confirmEtcdAdminMutation("修改 etcd 用户密码", "确认修改此用户的密码？", app.localized("用户名：")+name+"\n"+app.localized("密码只会临时发送给 etcd，不会写入连接配置。"), func(ctx context.Context, admin connection.EtcdAdmin) error {
		return admin.ChangeUserPassword(ctx, name, password)
	}, func() { app.loadEtcdUserDetails(name) })
}

func (app *workbench) grantEtcdUserRole(name string) {
	role := strings.TrimSpace(app.etcdGrantRoleName)
	if name == "" || role == "" {
		app.status = app.localized("请选择角色。")
		return
	}
	app.confirmEtcdAdminMutation("授予 etcd 角色", "确认将角色授予此用户？", fmt.Sprintf("%s%s\n%s%s", app.localized("用户："), name, app.localized("角色："), role), func(ctx context.Context, admin connection.EtcdAdmin) error {
		return admin.GrantUserRole(ctx, name, role)
	}, func() {
		app.etcdGrantRoleName = ""
		app.etcdGrantRoleOpen = false
		app.loadEtcdUserDetails(name)
	})
}

func (app *workbench) revokeEtcdUserRole(name, role string) {
	app.confirmEtcdAdminMutation("撤销 etcd 用户角色", "确认撤销此用户的角色？", fmt.Sprintf("%s%s\n%s%s", app.localized("用户："), name, app.localized("角色："), role), func(ctx context.Context, admin connection.EtcdAdmin) error {
		return admin.RevokeUserRole(ctx, name, role)
	}, func() { app.loadEtcdUserDetails(name) })
}

func (app *workbench) createEtcdRole() {
	name := strings.TrimSpace(app.etcdNewRole)
	if name == "" {
		app.status = app.localized("请输入角色名称。")
		return
	}
	app.confirmEtcdAdminMutation("创建 etcd 角色", "确认创建此角色？", app.localized("角色：")+name, func(ctx context.Context, admin connection.EtcdAdmin) error {
		return admin.CreateRole(ctx, name)
	}, func() {
		app.etcdNewRole = ""
		app.etcdCreateRoleOpen = false
		app.loadEtcdAuthOverview()
	})
}

func (app *workbench) deleteEtcdRole(name string) {
	app.confirmEtcdAdminMutation("删除 etcd 角色", "确认删除此角色？", app.localized("角色：")+name+"\n"+app.localized("已有用户的角色关联和权限将受影响。"), func(ctx context.Context, admin connection.EtcdAdmin) error {
		return admin.DeleteRole(ctx, name)
	}, func() {
		app.etcdSelectedRole = ""
		app.etcdRoleDetails = connection.EtcdRole{}
		app.loadEtcdAuthOverview()
	})
}

func (app *workbench) grantEtcdRolePermission() {
	role := app.etcdSelectedRole
	key := app.etcdPermissionKey
	scope := app.etcdPermissionScope
	permission := app.etcdPermissionType
	if role == "" || permission == "" || (scope != "all" && key == "") {
		app.status = app.localized("请填写权限范围和权限类型。")
		return
	}
	var rangeEnd string
	switch scope {
	case "all":
		key, rangeEnd = "", "\x00"
	case "prefix":
		rangeEnd = app.etcdPrefixRangeEnd(key)
	case "exact":
		rangeEnd = ""
	default:
		app.status = app.localized("请选择权限范围。")
		return
	}
	displayKey := key
	if scope == "all" {
		displayKey = app.localized("所有键")
	} else if scope == "prefix" {
		displayKey += "*"
	}
	app.confirmEtcdAdminMutation("授予 etcd 键权限", "确认授予此权限？", fmt.Sprintf("%s%s\n%s%s\n%s%s", app.localized("角色："), role, app.localized("范围："), displayKey, app.localized("权限："), permission), func(ctx context.Context, admin connection.EtcdAdmin) error {
		return admin.GrantRolePermission(ctx, role, key, rangeEnd, permission)
	}, func() {
		app.etcdPermissionKey = ""
		app.etcdGrantPermissionOpen = false
		app.loadEtcdRoleDetails(role)
	})
}

func (app *workbench) revokeEtcdRolePermission(role string, permission connection.EtcdPermission) {
	app.confirmEtcdAdminMutation("撤销 etcd 键权限", "确认撤销此权限？", fmt.Sprintf("%s%s\n%s%q\n%s%s", app.localized("角色："), role, app.localized("键起点："), permission.Key, app.localized("权限："), permission.Type), func(ctx context.Context, admin connection.EtcdAdmin) error {
		return admin.RevokeRolePermission(ctx, role, permission.Key, permission.RangeEnd)
	}, func() { app.loadEtcdRoleDetails(role) })
}

func (app *workbench) toggleEtcdAuth(enabled bool) {
	detail := app.localized("关闭认证后，etcd API 将不再验证用户名和密码。")
	message := "确认关闭 etcd 认证？"
	if enabled {
		message = "确认启用 etcd 认证？"
		detail = app.localized("启用前请确认 root 用户、密码及 root 角色已配置。认证启用后，当前连接可能失去访问权限；请准备可用的认证配置后重新连接。")
	}
	app.confirmEtcdAdminMutation("修改 etcd 认证状态", message, detail, func(ctx context.Context, admin connection.EtcdAdmin) error {
		return admin.SetAuthEnabled(ctx, enabled)
	}, func() {
		app.refreshEtcdAuth()
		app.refreshEtcdDashboard()
	})
}

func (app *workbench) confirmEtcdDefragment(endpoint string) {
	if strings.TrimSpace(endpoint) == "" {
		return
	}
	app.confirmEtcdAdminMutation("整理 etcd 后端数据库", "确认对该成员执行数据库碎片整理？", app.localized("端点：")+endpoint+"\n"+app.localized("整理期间此成员可能暂时无法服务请求。"), func(ctx context.Context, admin connection.EtcdAdmin) error {
		return admin.Defragment(ctx, endpoint)
	}, func() { app.loadEtcdDashboard(true) })
}

func (app *workbench) confirmEtcdAlarmDisarm(alarm connection.EtcdAlarm) {
	app.confirmEtcdAdminMutation("解除 etcd 告警", "确认请求解除此告警？", fmt.Sprintf("%s%d\n%s%s", app.localized("成员 ID："), alarm.MemberID, app.localized("告警："), alarm.Type), func(ctx context.Context, admin connection.EtcdAdmin) error {
		return admin.DisarmAlarm(ctx, alarm.MemberID, alarm.Type)
	}, func() { app.loadEtcdDashboard(true) })
}

func (app *workbench) etcdPrefixRangeEnd(prefix string) string {
	if prefix == "" {
		return "\x00"
	}
	end := []byte(prefix)
	for i := len(end) - 1; i >= 0; i-- {
		if end[i] < 0xff {
			end[i]++
			return string(end[:i+1])
		}
	}
	return "\x00"
}
