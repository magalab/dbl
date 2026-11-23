package app

import (
	"fmt"
	"strings"

	"github.com/egoist/mygo/ui"
)

func (app *workbench) postgresRoleManagerWorkspace(c *ui.Context, workspace *queryWorkspace) {
	if !workspace.postgresRoleLoaded && !workspace.postgresRoleLoading {
		app.refreshPostgresRoles(workspace)
	}
	ui.Column(c).Fill().Children(func() {
		ui.Row(c).FillWidth().Padding(10, 14).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Icon(c, treeItemIcons["users"]).Size(20, 20).TextColor(treeItemIconColors["users"])
			ui.Text(c, app.localized("用户与权限")).Bold().Role(ui.RoleHeading).Level(2)
			ui.Text(c, app.openedLabel).TextColor(c.Theme().TextMuted).SingleLine().Grow(1).MinWidth(0)
			refresh := iconButton(c, app.localized("刷新角色列表"), actionIcons["refresh"])
			refresh.Disabled(app.busy || app.dialogOpen || workspace.postgresRoleLoading)
			if refresh.Clicked() {
				app.refreshPostgresRoleManager(workspace)
			}
			create := iconPrimaryButton(c, app.localized("新建角色"), actionIcons["add"])
			create.Disabled(app.busy || app.dialogOpen || app.openedReadOnly)
			if create.Clicked() {
				workspace.postgresRoleCreateOpen = !workspace.postgresRoleCreateOpen
				workspace.postgresRoleDraftName = ""
				workspace.postgresRoleDraftPassword = ""
				workspace.postgresRoleDraftLogin = true
				workspace.postgresRoleDraftSuperuser = false
				workspace.postgresRoleDraftCreateDB = false
				workspace.postgresRoleDraftCreateRole = false
			}
		})
		if workspace.postgresRoleLoading {
			ui.Row(c).FillWidth().Padding(10, 14).Gap(8).Children(func() {
				ui.Spinner(c).Size(16, 16).Label(app.localized("正在读取 PostgreSQL 角色…"))
				ui.Text(c, app.localized("正在读取 PostgreSQL 角色…")).TextColor(c.Theme().TextMuted)
			})
		}
		if workspace.postgresRoleError != "" {
			ui.Text(c, app.localized("读取 PostgreSQL 角色失败：")+workspace.postgresRoleError).Padding(10, 14).TextColor(c.Theme().Danger)
		}
		ui.Split(c, &workspace.postgresRoleListWidth, func() {
			app.postgresRoleListPane(c, workspace)
		}, func() {
			ui.Split(c, &workspace.postgresRoleEditorWidth, func() {
				app.postgresRoleDetailPane(c, workspace)
			}, func() {
				app.postgresRoleGrantPane(c, workspace)
			}).Grow(1)
		}).Grow(1)
	})
}

func (app *workbench) postgresRoleListPane(c *ui.Context, workspace *queryWorkspace) {
	ui.Column(c).Fill().MinWidth(0).Children(func() {
		ui.Row(c).FillWidth().Padding(8).Gap(6).AlignItems(ui.Center).Children(func() {
			search := ui.TextInput(c, &workspace.postgresRoleSearch).Height(34).Padding(2, 8).
				Placeholder(app.localized("搜索角色")).Label(app.localized("搜索角色")).Grow(1)
			search.Disabled(app.busy || app.dialogOpen)
			ui.Text(c, fmt.Sprintf(app.localized("%d 个角色"), len(workspace.postgresRoles))).TextColor(c.Theme().TextMuted)
		})
		if workspace.postgresRoleCreateOpen {
			ui.Column(c).FillWidth().Padding(8).Gap(6).Background(c.Theme().Surface).Border(1, c.Theme().Border).Radius(c.Theme().Radius).Children(func() {
				ui.Text(c, app.localized("新建 PostgreSQL 角色")).Bold().Role(ui.RoleHeading).Level(3)
				name := ui.TextInput(c, &workspace.postgresRoleDraftName).Height(32).Placeholder(app.localized("角色名称")).Label(app.localized("角色名称"))
				name.Disabled(app.busy || app.dialogOpen || app.openedReadOnly)
				login := ui.Checkbox(c, &workspace.postgresRoleDraftLogin, app.localized("允许登录 (LOGIN)"))
				login.Disabled(app.busy || app.dialogOpen || app.openedReadOnly)
				if workspace.postgresRoleDraftLogin {
					password := ui.TextInput(c, &workspace.postgresRoleDraftPassword).Height(32).Placeholder(app.localized("密码（可留空）")).Label(app.localized("密码")).Password()
					password.Disabled(app.busy || app.dialogOpen || app.openedReadOnly)
				}
				superuser := ui.Checkbox(c, &workspace.postgresRoleDraftSuperuser, app.localized("超级用户"))
				superuser.Disabled(app.busy || app.dialogOpen || app.openedReadOnly)
				createDB := ui.Checkbox(c, &workspace.postgresRoleDraftCreateDB, app.localized("允许创建数据库"))
				createDB.Disabled(app.busy || app.dialogOpen || app.openedReadOnly)
				createRole := ui.Checkbox(c, &workspace.postgresRoleDraftCreateRole, app.localized("允许创建角色"))
				createRole.Disabled(app.busy || app.dialogOpen || app.openedReadOnly)
				ui.Row(c).Gap(6).Children(func() {
					create := iconPrimaryButton(c, app.localized("创建角色"), actionIcons["save"])
					create.Disabled(app.busy || app.dialogOpen || app.openedReadOnly)
					if create.Clicked() {
						app.createPostgresRole(workspace)
					}
					cancel := iconButton(c, app.localized("取消"), actionIcons["close"])
					if cancel.Clicked() {
						clearPostgresRoleSecret(&workspace.postgresRoleDraftPassword)
						workspace.postgresRoleCreateOpen = false
					}
				})
			})
		}
		ui.Scroll(c).FillWidth().Grow(1).MinHeight(0).Children(func() {
			ui.Column(c).FillWidth().Padding(4, 6).Gap(2).Children(func() {
				filter := strings.ToLower(strings.TrimSpace(workspace.postgresRoleSearch))
				for _, role := range workspace.postgresRoles {
					if filter != "" && !strings.Contains(strings.ToLower(role.Name), filter) {
						continue
					}
					role := role
					button := ui.Button(c, "").FillWidth().Padding(8).Selected(role.Name == workspace.postgresRoleSelected)
					button.Disabled(app.busy || app.dialogOpen)
					button.Children(func() {
						ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
							ui.Icon(c, treeItemIcons["users"]).Size(18, 18).Shrink(0).TextColor(treeItemIconColors["users"])
							ui.Text(c, role.Name).SingleLine().Ellipsis("…").Grow(1).MinWidth(0)
							label := app.localized("不可登录")
							if role.CanLogin {
								label = app.localized("可登录")
							}
							ui.Text(c, label).Padding(4, 8).Background(c.Theme().Surface).Border(1, c.Theme().Border).Radius(c.Theme().Radius).TextColor(c.Theme().TextMuted).SingleLine()
						})
					})
					if button.Clicked() {
						workspace.postgresRoleSelected = role.Name
						workspace.postgresRoleDraft = role
						workspace.postgresRoleDetails = nil
						workspace.postgresRolePrivileges = make([]bool, len(postgresRoleWorkspacePrivileges(workspace)))
						app.loadPostgresRoleDetails(workspace)
					}
				}
			})
		})
	})
}

func (app *workbench) postgresRoleDetailPane(c *ui.Context, workspace *queryWorkspace) {
	ui.Column(c).Fill().MinWidth(0).Children(func() {
		role, ok := selectedPostgresRole(workspace)
		if !ok {
			ui.Column(c).Fill().Justify(ui.Center).AlignItems(ui.Center).Children(func() {
				ui.Text(c, app.localized("选择一个角色查看属性和权限")).TextColor(c.Theme().TextMuted)
			})
			return
		}
		ui.Row(c).FillWidth().Padding(10, 12).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Icon(c, treeItemIcons["users"]).Size(20, 20).TextColor(treeItemIconColors["users"])
			ui.Text(c, role.Name).Bold().SingleLine().Ellipsis("…").Grow(1).MinWidth(0)
			loginLabel := app.localized("不可登录")
			if role.CanLogin {
				loginLabel = app.localized("可登录")
			}
			ui.Text(c, loginLabel).Padding(4, 8).Background(c.Theme().Surface).Border(1, c.Theme().Border).Radius(c.Theme().Radius).TextColor(c.Theme().TextMuted).SingleLine()
		})
		ui.Row(c).FillWidth().Padding(8, 12).Gap(6).Children(func() {
			password := iconButton(c, app.localized("修改密码"), treeItemIcons["key"])
			password.Disabled(app.busy || app.dialogOpen || app.openedReadOnly)
			if password.Clicked() {
				workspace.postgresRolePasswordOpen = !workspace.postgresRolePasswordOpen
				workspace.postgresRoleNewPassword = ""
			}
			loginTitle := "启用登录"
			loginIcon := actionIcons["check"]
			if role.CanLogin {
				loginTitle = "禁用登录"
				loginIcon = actionIcons["close"]
			}
			login := iconButton(c, app.localized(loginTitle), loginIcon)
			login.Disabled(app.busy || app.dialogOpen || app.openedReadOnly)
			if login.Clicked() {
				app.togglePostgresRoleLogin(workspace, role)
			}
			remove := iconButton(c, app.localized("删除角色"), actionIcons["trash"])
			remove.Disabled(app.busy || app.dialogOpen || app.openedReadOnly)
			if remove.Clicked() {
				app.dropPostgresRole(workspace, role)
			}
		})
		if workspace.postgresRolePasswordOpen {
			ui.Row(c).FillWidth().Padding(8, 12).Gap(6).Children(func() {
				password := ui.TextInput(c, &workspace.postgresRoleNewPassword).Height(32).Placeholder(app.localized("新密码")).Label(app.localized("新密码")).Password().Grow(1)
				password.Disabled(app.busy || app.dialogOpen || app.openedReadOnly)
				apply := iconPrimaryButton(c, app.localized("更新密码"), actionIcons["save"])
				apply.Disabled(app.busy || app.dialogOpen || app.openedReadOnly)
				if apply.Clicked() {
					app.changePostgresRolePassword(workspace)
				}
			})
		}
		ui.Row(c).FillWidth().Padding(12).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Text(c, app.localized("角色属性")).Bold().Role(ui.RoleHeading).Level(3).Grow(1)
			apply := iconPrimaryButton(c, app.localized("应用属性变更"), actionIcons["save"])
			apply.Disabled(app.busy || app.dialogOpen || app.openedReadOnly || !postgresRoleAttributesChanged(role, workspace.postgresRoleDraft))
			if apply.Clicked() {
				app.updatePostgresRoleAttributes(workspace)
			}
		})
		ui.Scroll(c).FillWidth().MaxHeight(220).Padding(8, 12).Children(func() {
			ui.Column(c).FillWidth().Gap(2).Children(func() {
				app.postgresRoleAttributeCheckbox(c, workspace, &workspace.postgresRoleDraft.CanLogin, "允许登录 (LOGIN)")
				app.postgresRoleAttributeCheckbox(c, workspace, &workspace.postgresRoleDraft.Superuser, "超级用户")
				app.postgresRoleAttributeCheckbox(c, workspace, &workspace.postgresRoleDraft.CreateDB, "允许创建数据库")
				app.postgresRoleAttributeCheckbox(c, workspace, &workspace.postgresRoleDraft.CreateRole, "允许创建角色")
				app.postgresRoleAttributeCheckbox(c, workspace, &workspace.postgresRoleDraft.Inherit, "继承成员角色权限 (INHERIT)")
				app.postgresRoleAttributeCheckbox(c, workspace, &workspace.postgresRoleDraft.Replication, "允许复制 (REPLICATION)")
				app.postgresRoleAttributeCheckbox(c, workspace, &workspace.postgresRoleDraft.BypassRLS, "绕过行级安全 (BYPASSRLS)")
			})
		})
		ui.Row(c).FillWidth().Padding(12).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Text(c, app.localized("角色成员关系")).Bold().Role(ui.RoleHeading).Level(3).Grow(1)
		})
		ui.Row(c).FillWidth().Padding(8, 12).Gap(6).AlignItems(ui.Center).Children(func() {
			options := make([]string, 0, len(workspace.postgresRoles))
			for _, candidate := range workspace.postgresRoles {
				if candidate.Name != role.Name {
					options = append(options, candidate.Name)
				}
			}
			if !containsPostgresRole(workspace.postgresRoles, workspace.postgresRoleMemberRole) || workspace.postgresRoleMemberRole == role.Name {
				workspace.postgresRoleMemberRole = firstOtherPostgresRole(workspace.postgresRoles, role.Name)
			}
			selectRole := ui.Select(c, &workspace.postgresRoleMemberRole, options).Label(app.localized("授予的成员角色")).Grow(1)
			selectRole.Disabled(app.busy || app.dialogOpen || app.openedReadOnly || len(options) == 0)
			admin := ui.Checkbox(c, &workspace.postgresRoleMemberAdmin, app.localized("管理员选项"))
			admin.Disabled(app.busy || app.dialogOpen || app.openedReadOnly || workspace.postgresRoleMemberRevoke)
		})
		ui.Row(c).FillWidth().Padding(8, 12).Gap(6).Children(func() {
			grant := ui.Button(c, app.localized("授予成员角色")).Selected(!workspace.postgresRoleMemberRevoke)
			grant.Disabled(app.busy || app.dialogOpen || app.openedReadOnly)
			if grant.Clicked() {
				workspace.postgresRoleMemberRevoke = false
			}
			revoke := ui.Button(c, app.localized("撤销成员角色")).Selected(workspace.postgresRoleMemberRevoke)
			revoke.Disabled(app.busy || app.dialogOpen || app.openedReadOnly)
			if revoke.Clicked() {
				workspace.postgresRoleMemberRevoke = true
			}
			apply := iconPrimaryButton(c, app.localized("应用"), actionIcons["check"])
			apply.Disabled(app.busy || app.dialogOpen || app.openedReadOnly || postgresRoleMembershipPreview(workspace) == "")
			if apply.Clicked() {
				statement := postgresRoleMembershipPreview(workspace)
				app.runPostgresRoleMutation(workspace, statement, statement, "更新 PostgreSQL 角色成员关系", "已更新 PostgreSQL 角色成员关系。", nil)
			}
		})
		ui.Row(c).FillWidth().Padding(12).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Text(c, app.localized("权限与成员关系")).Bold().Role(ui.RoleHeading).Level(3).Grow(1)
		})
		if workspace.postgresRoleDetailsLoading {
			ui.Row(c).Padding(12).Gap(8).Children(func() {
				ui.Spinner(c).Size(16, 16).Label(app.localized("正在读取角色权限…"))
				ui.Text(c, app.localized("正在读取角色权限…")).TextColor(c.Theme().TextMuted)
			})
		}
		if workspace.postgresRoleDetailsError != "" {
			ui.Text(c, app.localized("读取角色权限失败：")+workspace.postgresRoleDetailsError).Padding(12).TextColor(c.Theme().Warning)
		}
		ui.Scroll(c).Fill().Padding(10, 12).Children(func() {
			ui.Column(c).FillWidth().Gap(8).Children(func() {
				if len(workspace.postgresRoleDetails) == 0 && !workspace.postgresRoleDetailsLoading && workspace.postgresRoleDetailsError == "" {
					ui.Text(c, app.localized("当前数据库未发现有效授权或成员关系。")).TextColor(c.Theme().TextMuted)
				}
				for _, detail := range workspace.postgresRoleDetails {
					ui.Text(c, detail).FillWidth().Padding(10).Background(c.Theme().Surface).Border(1, c.Theme().Border).Radius(c.Theme().Radius).Selectable()
				}
			})
		})
	})
}

func (app *workbench) postgresRoleAttributeCheckbox(c *ui.Context, workspace *queryWorkspace, value *bool, label string) {
	checkbox := ui.Checkbox(c, value, app.localized(label))
	checkbox.Disabled(app.busy || app.dialogOpen || app.openedReadOnly)
}

func (app *workbench) postgresRoleGrantPane(c *ui.Context, workspace *queryWorkspace) {
	ui.Column(c).Fill().MinWidth(250).Padding(12).Gap(8).Children(func() {
		ui.Text(c, app.localized("权限编辑")).Bold().Role(ui.RoleHeading).Level(3)
		ui.Text(c, app.localized("选择数据库、Schema 或表权限；授权和撤权会生成可确认的 SQL。 ")).TextColor(c.Theme().TextMuted).Wrap()
		ui.Row(c).Gap(6).Children(func() {
			database := ui.Button(c, app.localized("数据库")).Selected(workspace.postgresRoleGrantTargetType == "database")
			if database.Clicked() {
				workspace.postgresRoleGrantTargetType = "database"
				workspace.postgresRolePrivileges = make([]bool, 3)
			}
			schema := ui.Button(c, app.localized("模式")).Selected(workspace.postgresRoleGrantTargetType == "schema")
			if schema.Clicked() {
				workspace.postgresRoleGrantTargetType = "schema"
				workspace.postgresRolePrivileges = make([]bool, 2)
				if workspace.postgresRoleGrantSchema == "" && len(app.sqlNamespaces) > 0 {
					workspace.postgresRoleGrantSchema = app.sqlNamespaces[0]
				}
			}
			table := ui.Button(c, app.localized("表")).Selected(workspace.postgresRoleGrantTargetType == "table")
			if table.Clicked() {
				workspace.postgresRoleGrantTargetType = "table"
				if workspace.postgresRoleGrantTable == "" && len(workspace.postgresRoleTableTargets) > 0 {
					workspace.postgresRoleGrantTable = workspace.postgresRoleTableTargets[0]
				}
				workspace.postgresRoleGrantTableKind = workspace.postgresRoleTableKinds[workspace.postgresRoleGrantTable]
				workspace.postgresRolePrivileges = make([]bool, len(postgresRoleWorkspacePrivileges(workspace)))
			}
		})
		if workspace.postgresRoleGrantTargetType == "schema" {
			if workspace.postgresRoleGrantSchema == "" && len(app.sqlNamespaces) > 0 {
				workspace.postgresRoleGrantSchema = app.sqlNamespaces[0]
			}
			selectSchema := ui.Select(c, &workspace.postgresRoleGrantSchema, app.sqlNamespaces).Label(app.localized("目标 Schema"))
			selectSchema.Disabled(app.busy || app.dialogOpen || app.openedReadOnly || len(app.sqlNamespaces) == 0)
		} else if workspace.postgresRoleGrantTargetType == "table" {
			if !containsString(workspace.postgresRoleTableTargets, workspace.postgresRoleGrantTable) && len(workspace.postgresRoleTableTargets) > 0 {
				workspace.postgresRoleGrantTable = workspace.postgresRoleTableTargets[0]
			}
			if kind := workspace.postgresRoleTableKinds[workspace.postgresRoleGrantTable]; kind != workspace.postgresRoleGrantTableKind {
				workspace.postgresRoleGrantTableKind = kind
				workspace.postgresRolePrivileges = make([]bool, len(postgresRoleWorkspacePrivileges(workspace)))
			}
			selectTable := ui.Select(c, &workspace.postgresRoleGrantTable, workspace.postgresRoleTableTargets).Label(app.localized("目标表"))
			selectTable.Disabled(app.busy || app.dialogOpen || app.openedReadOnly || len(workspace.postgresRoleTableTargets) == 0)
			if selectTable.Changed() {
				workspace.postgresRoleGrantTableKind = workspace.postgresRoleTableKinds[workspace.postgresRoleGrantTable]
				workspace.postgresRolePrivileges = make([]bool, len(postgresRoleWorkspacePrivileges(workspace)))
			}
		} else {
			ui.Text(c, app.localized("当前数据库")+" · "+workspace.postgresRoleDatabase).Padding(8).TextColor(c.Theme().TextMuted)
		}
		ui.Row(c).Gap(6).Children(func() {
			grant := ui.Button(c, app.localized("授予")).Selected(!workspace.postgresRoleRevoke)
			if grant.Clicked() {
				workspace.postgresRoleRevoke = false
			}
			revoke := ui.Button(c, app.localized("撤销")).Selected(workspace.postgresRoleRevoke)
			if revoke.Clicked() {
				workspace.postgresRoleRevoke = true
				workspace.postgresRoleGrantOption = false
			}
		})
		privileges := postgresRoleWorkspacePrivileges(workspace)
		ui.Scroll(c).FillWidth().Grow(1).MinHeight(0).Children(func() {
			ui.Column(c).FillWidth().Gap(2).Children(func() {
				for index, privilege := range privileges {
					if index >= len(workspace.postgresRolePrivileges) {
						workspace.postgresRolePrivileges = append(workspace.postgresRolePrivileges, make([]bool, len(privileges)-len(workspace.postgresRolePrivileges))...)
					}
					checkbox := ui.Checkbox(c, &workspace.postgresRolePrivileges[index], app.localized(privilege))
					checkbox.Disabled(app.busy || app.dialogOpen || app.openedReadOnly)
				}
			})
		})
		if !workspace.postgresRoleRevoke {
			grantOption := ui.Checkbox(c, &workspace.postgresRoleGrantOption, app.localized("允许继续授权 (WITH GRANT OPTION)"))
			grantOption.Disabled(app.busy || app.dialogOpen || app.openedReadOnly)
		}
		preview := postgresRoleGrantPreview(workspace, workspace.postgresRoleDatabase, app.sqlNamespaces)
		if preview == "" {
			preview = app.localized("选择一个角色和至少一项权限以生成 SQL。")
		}
		ui.Text(c, preview).FillWidth().MinHeight(84).Padding(10).Background(c.Theme().Surface).Border(1, c.Theme().Border).Radius(c.Theme().Radius).Selectable()
		apply := iconPrimaryButton(c, app.localized("预览并应用"), actionIcons["check"])
		apply.Disabled(app.busy || app.dialogOpen || app.openedReadOnly || preview == "" || selectedPostgresRoleIsEmpty(workspace))
		if apply.Clicked() {
			statement := postgresRoleGrantPreview(workspace, workspace.postgresRoleDatabase, app.sqlNamespaces)
			app.runPostgresRoleMutation(workspace, statement, statement, "更新 PostgreSQL 角色权限", "已更新 PostgreSQL 角色权限。", func() {
				workspace.postgresRolePrivileges = make([]bool, len(postgresRoleWorkspacePrivileges(workspace)))
				workspace.postgresRoleGrantOption = false
			})
		}
	})
}

func selectedPostgresRoleIsEmpty(workspace *queryWorkspace) bool {
	_, ok := selectedPostgresRole(workspace)
	return !ok
}
