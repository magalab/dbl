package app

import (
	"fmt"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/magalab/dbl/internal/connection"
)

func (app *workbench) etcdAuthWorkspace(c *ui.Context) {
	ui.Column(c).Fill().Children(func() {
		ui.Row(c).FillWidth().Padding(8, 12).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Text(c, app.localized("访问控制")).Bold().Role(ui.RoleHeading).Level(2).Grow(1)
			status := app.localized("认证状态未知")
			if app.etcdAuthOverview.Enabled {
				status = app.localized("认证已启用")
			} else if app.etcdAuthOverview.StatusError == "" {
				status = app.localized("认证未启用")
			}
			ui.Text(c, status).TextColor(c.Theme().TextMuted)
			refresh := iconButton(c, app.localized("刷新用户与角色"), actionIcons["refresh"])
			refresh.Disabled(app.etcdAuthLoading || app.busy || app.dialogOpen)
			if refresh.Clicked() {
				app.refreshEtcdAuth()
			}
			authLabel, authIcon := "启用认证", actionIcons["check"]
			if app.etcdAuthOverview.Enabled {
				authLabel, authIcon = "关闭认证", actionIcons["close"]
			}
			auth := iconButton(c, app.localized(authLabel), authIcon)
			auth.Disabled(app.busy || app.dialogOpen || app.openedReadOnly || app.etcdAuthOverview.StatusError != "")
			if auth.Clicked() {
				app.toggleEtcdAuth(!app.etcdAuthOverview.Enabled)
			}
		})
		if app.etcdAuthLoading {
			ui.Text(c, app.localized("正在读取 etcd 用户和角色…")).TextColor(c.Theme().TextMuted).Padding(12, 12)
			return
		}
		if app.etcdAuthError != "" {
			ui.Text(c, app.localized("读取用户与角色失败：")+app.etcdAuthError).TextColor(c.Theme().Danger).Padding(12, 12)
			return
		}
		if app.etcdAuthOverview.StatusError != "" {
			ui.Text(c, app.localized("读取认证状态失败：")+app.etcdAuthOverview.StatusError).TextColor(c.Theme().Warning).Padding(8, 12)
		}
		ui.Row(c).Padding(0, 12).Gap(6).Children(func() {
			users := ui.Button(c, app.localized("用户")).Selected(app.etcdAuthTab != "roles")
			if users.Clicked() {
				app.etcdAuthTab = "users"
			}
			roles := ui.Button(c, app.localized("角色")).Selected(app.etcdAuthTab == "roles")
			if roles.Clicked() {
				app.etcdAuthTab = "roles"
			}
		})
		if app.etcdAuthTab == "roles" {
			ui.Split(c, &app.etcdPaneWidth, func() { app.etcdRoleListPane(c) }, func() { app.etcdRoleDetailPane(c) }).Grow(1)
		} else {
			ui.Split(c, &app.etcdPaneWidth, func() { app.etcdUserListPane(c) }, func() { app.etcdUserDetailPane(c) }).Grow(1)
		}
	})
}

func (app *workbench) etcdUserListPane(c *ui.Context) {
	ui.Column(c).Fill().MinWidth(0).Label(app.localized("etcd 用户列表")).Children(func() {
		ui.Row(c).Padding(8, 8).Gap(6).AlignItems(ui.Center).Children(func() {
			ui.Text(c, fmt.Sprintf(app.localized("%d 个用户"), len(app.etcdAuthOverview.Users))).TextColor(c.Theme().TextMuted).Grow(1)
			add := iconButton(c, app.localized("新建 etcd 用户"), actionIcons["add"])
			add.Disabled(app.busy || app.dialogOpen || app.openedReadOnly)
			if add.Clicked() {
				app.etcdCreateUserOpen = !app.etcdCreateUserOpen
				app.etcdNewUsername, app.etcdNewPassword = "", ""
			}
		})
		if app.etcdAuthOverview.UsersError != "" {
			ui.Text(c, app.localized("读取用户失败：")+app.etcdAuthOverview.UsersError).TextColor(c.Theme().Warning).Padding(8, 8)
		}
		if app.etcdCreateUserOpen {
			ui.Column(c).FillWidth().Padding(8).Gap(5).Background(c.Theme().Surface).Border(1, c.Theme().Border).Radius(c.Theme().Radius).Children(func() {
				name := ui.TextInput(c, &app.etcdNewUsername).Height(32).Placeholder(app.localized("用户名")).Label(app.localized("用户名"))
				name.Disabled(app.busy || app.dialogOpen || app.openedReadOnly)
				password := ui.TextInput(c, &app.etcdNewPassword).Height(32).Placeholder(app.localized("密码")).Label(app.localized("密码")).Password()
				password.Disabled(app.busy || app.dialogOpen || app.openedReadOnly)
				ui.Row(c).Gap(5).Children(func() {
					create := iconPrimaryButton(c, app.localized("创建用户"), actionIcons["save"])
					create.Disabled(app.busy || app.dialogOpen || app.openedReadOnly)
					if create.Clicked() {
						app.createEtcdUser()
					}
					cancel := iconButton(c, app.localized("取消"), actionIcons["close"])
					if cancel.Clicked() {
						app.etcdCreateUserOpen = false
						app.etcdNewPassword = ""
					}
				})
			})
		}
		ui.Scroll(c).FillWidth().Grow(1).MinHeight(0).Children(func() {
			ui.Column(c).FillWidth().Padding(4, 6).Gap(2).Children(func() {
				for _, name := range app.etcdAuthOverview.Users {
					name := name
					button := ui.Button(c, name).FillWidth().Padding(7, 8).Selected(name == app.etcdSelectedUser)
					button.Disabled(app.busy || app.dialogOpen)
					if button.Clicked() {
						app.etcdSelectedUser, app.etcdUserDetails = name, connection.EtcdUser{}
						app.loadEtcdUserDetails(name)
					}
				}
			})
		})
	})
}

func (app *workbench) etcdUserDetailPane(c *ui.Context) {
	ui.Column(c).Fill().Padding(14).Gap(8).Children(func() {
		name := app.etcdSelectedUser
		if name == "" {
			ui.Column(c).Fill().Justify(ui.Center).AlignItems(ui.Center).Children(func() {
				ui.Text(c, app.localized("选择一个用户查看角色和操作")).TextColor(c.Theme().TextMuted)
			})
			return
		}
		ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Text(c, name).Bold().Role(ui.RoleHeading).Level(2).Grow(1)
			remove := iconButton(c, app.localized("删除用户"), actionIcons["trash"])
			remove.Disabled(app.busy || app.dialogOpen || app.openedReadOnly)
			if remove.Clicked() {
				app.deleteEtcdUser(name)
			}
		})
		if app.etcdUserDetails.Name == "" {
			ui.Text(c, app.localized("正在读取用户详情…")).TextColor(c.Theme().TextMuted)
			return
		}
		ui.Text(c, app.localized("已授予角色")).Bold().Role(ui.RoleHeading).Level(3)
		if len(app.etcdUserDetails.Roles) == 0 {
			ui.Text(c, app.localized("此用户尚未关联角色。 ")).TextColor(c.Theme().TextMuted)
		}
		for _, role := range app.etcdUserDetails.Roles {
			role := role
			ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Icon(c, treeItemIcons["key"]).Size(16, 16).TextColor(treeItemIconColors["key"])
				ui.Text(c, role).Grow(1)
				remove := iconButton(c, app.localized("撤销角色"), actionIcons["close"])
				remove.Disabled(app.busy || app.dialogOpen || app.openedReadOnly)
				if remove.Clicked() {
					app.revokeEtcdUserRole(name, role)
				}
			})
		}
		if !app.openedReadOnly {
			if app.etcdGrantRoleOpen {
				ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
					role := ui.Select(c, &app.etcdGrantRoleName, app.etcdAuthOverview.Roles).Label(app.localized("角色")).Grow(1)
					role.Disabled(app.busy || app.dialogOpen)
					grant := iconButton(c, app.localized("授予角色"), actionIcons["check"])
					grant.Disabled(app.busy || app.dialogOpen || app.etcdGrantRoleName == "")
					if grant.Clicked() {
						app.grantEtcdUserRole(name)
					}
					cancel := iconButton(c, app.localized("取消"), actionIcons["close"])
					if cancel.Clicked() {
						app.etcdGrantRoleOpen = false
					}
				})
			} else {
				grant := iconButton(c, app.localized("关联角色"), actionIcons["add"])
				grant.Disabled(app.busy || app.dialogOpen || len(app.etcdAuthOverview.Roles) == 0)
				if grant.Clicked() {
					app.etcdGrantRoleOpen = true
				}
			}
			ui.Text(c, app.localized("修改密码")).Bold().Role(ui.RoleHeading).Level(3)
			ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
				password := ui.TextInput(c, &app.etcdChangePassword).Password().Placeholder(app.localized("新密码")).Label(app.localized("新密码")).Grow(1)
				password.Disabled(app.busy || app.dialogOpen)
				change := iconButton(c, app.localized("修改密码"), actionIcons["save"])
				change.Disabled(app.busy || app.dialogOpen || app.etcdChangePassword == "")
				if change.Clicked() {
					app.changeEtcdUserPassword()
				}
			})
		}
	})
}

func (app *workbench) etcdRoleListPane(c *ui.Context) {
	ui.Column(c).Fill().MinWidth(0).Label(app.localized("etcd 角色列表")).Children(func() {
		ui.Row(c).Padding(8, 8).Gap(6).AlignItems(ui.Center).Children(func() {
			ui.Text(c, fmt.Sprintf(app.localized("%d 个角色"), len(app.etcdAuthOverview.Roles))).TextColor(c.Theme().TextMuted).Grow(1)
			add := iconButton(c, app.localized("新建 etcd 角色"), actionIcons["add"])
			add.Disabled(app.busy || app.dialogOpen || app.openedReadOnly)
			if add.Clicked() {
				app.etcdCreateRoleOpen = !app.etcdCreateRoleOpen
				app.etcdNewRole = ""
			}
		})
		if app.etcdAuthOverview.RolesError != "" {
			ui.Text(c, app.localized("读取角色失败：")+app.etcdAuthOverview.RolesError).TextColor(c.Theme().Warning).Padding(8, 8)
		}
		if app.etcdCreateRoleOpen {
			ui.Row(c).FillWidth().Padding(8).Gap(5).Children(func() {
				name := ui.TextInput(c, &app.etcdNewRole).Height(32).Placeholder(app.localized("角色名称")).Label(app.localized("角色名称")).Grow(1)
				name.Disabled(app.busy || app.dialogOpen || app.openedReadOnly)
				create := iconButton(c, app.localized("创建角色"), actionIcons["save"])
				create.Disabled(app.busy || app.dialogOpen || app.openedReadOnly)
				if create.Clicked() {
					app.createEtcdRole()
				}
			})
		}
		ui.Scroll(c).FillWidth().Grow(1).MinHeight(0).Children(func() {
			ui.Column(c).FillWidth().Padding(4, 6).Gap(2).Children(func() {
				for _, name := range app.etcdAuthOverview.Roles {
					name := name
					button := ui.Button(c, name).FillWidth().Padding(7, 8).Selected(name == app.etcdSelectedRole)
					button.Disabled(app.busy || app.dialogOpen)
					if button.Clicked() {
						app.etcdSelectedRole, app.etcdRoleDetails = name, connection.EtcdRole{}
						app.loadEtcdRoleDetails(name)
					}
				}
			})
		})
	})
}

func (app *workbench) etcdRoleDetailPane(c *ui.Context) {
	ui.Column(c).Fill().Padding(14).Gap(8).Children(func() {
		name := app.etcdSelectedRole
		if name == "" {
			ui.Column(c).Fill().Justify(ui.Center).AlignItems(ui.Center).Children(func() {
				ui.Text(c, app.localized("选择一个角色查看权限")).TextColor(c.Theme().TextMuted)
			})
			return
		}
		ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Text(c, name).Bold().Role(ui.RoleHeading).Level(2).Grow(1)
			remove := iconButton(c, app.localized("删除角色"), actionIcons["trash"])
			remove.Disabled(app.busy || app.dialogOpen || app.openedReadOnly || name == "root")
			if remove.Clicked() {
				app.deleteEtcdRole(name)
			}
		})
		if app.etcdRoleDetails.Name == "" {
			ui.Text(c, app.localized("正在读取角色详情…")).TextColor(c.Theme().TextMuted)
			return
		}
		ui.Text(c, app.localized("键权限")).Bold().Role(ui.RoleHeading).Level(3)
		if len(app.etcdRoleDetails.Permissions) == 0 {
			ui.Text(c, app.localized("此角色没有显式键权限。 ")).TextColor(c.Theme().TextMuted)
		}
		for _, permission := range app.etcdRoleDetails.Permissions {
			permission := permission
			ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Text(c, permission.Type).Bold()
				rangeLabel := permission.Key
				if permission.RangeEnd == "\x00" {
					rangeLabel = app.localized("所有键")
				} else if permission.RangeEnd != "" {
					rangeLabel += " … < " + strings.ReplaceAll(permission.RangeEnd, "\x00", "\\x00")
				}
				ui.Text(c, rangeLabel).Grow(1).SingleLine().Tooltip(rangeLabel)
				remove := iconButton(c, app.localized("撤销键权限"), actionIcons["close"])
				remove.Disabled(app.busy || app.dialogOpen || app.openedReadOnly)
				if remove.Clicked() {
					app.revokeEtcdRolePermission(name, permission)
				}
			})
		}
		if app.openedReadOnly {
			return
		}
		if !app.etcdGrantPermissionOpen {
			grant := iconButton(c, app.localized("添加键权限"), actionIcons["add"])
			grant.Disabled(app.busy || app.dialogOpen)
			if grant.Clicked() {
				app.etcdGrantPermissionOpen = true
				app.etcdPermissionScope = "exact"
				app.etcdPermissionType = "READWRITE"
			}
			return
		}
		ui.Column(c).FillWidth().Padding(10).Gap(6).Background(c.Theme().Surface).Border(1, c.Theme().Border).Radius(c.Theme().Radius).Children(func() {
			scopeOptions := []string{app.localized("精确键"), app.localized("键前缀"), app.localized("所有键")}
			scopeChoice := scopeOptions[0]
			switch app.etcdPermissionScope {
			case "prefix":
				scopeChoice = scopeOptions[1]
			case "all":
				scopeChoice = scopeOptions[2]
			}
			ui.Row(c).Gap(6).Children(func() {
				selectScope := ui.Select(c, &scopeChoice, scopeOptions).Label(app.localized("范围")).Width(150)
				selectScope.Disabled(app.busy || app.dialogOpen)
				key := ui.TextInput(c, &app.etcdPermissionKey).Placeholder(app.localized("键或前缀")).Label(app.localized("键或前缀")).Grow(1)
				key.Disabled(app.busy || app.dialogOpen || scopeChoice == "所有键")
				permission := ui.Select(c, &app.etcdPermissionType, []string{"READ", "WRITE", "READWRITE"}).Label(app.localized("权限")).Width(150)
				permission.Disabled(app.busy || app.dialogOpen)
			})
			if scopeChoice == scopeOptions[1] {
				app.etcdPermissionScope = "prefix"
			} else if scopeChoice == scopeOptions[2] {
				app.etcdPermissionScope = "all"
			} else {
				app.etcdPermissionScope = "exact"
			}
			ui.Row(c).Gap(6).Children(func() {
				grant := iconPrimaryButton(c, app.localized("授予权限"), actionIcons["check"])
				grant.Disabled(app.busy || app.dialogOpen || (scopeChoice != "所有键" && strings.TrimSpace(app.etcdPermissionKey) == ""))
				if grant.Clicked() {
					app.grantEtcdRolePermission()
				}
				cancel := iconButton(c, app.localized("取消"), actionIcons["close"])
				if cancel.Clicked() {
					app.etcdGrantPermissionOpen = false
				}
			})
		})
	})
}
