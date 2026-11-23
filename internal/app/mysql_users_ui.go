package app

import (
	"fmt"
	"strings"

	"github.com/egoist/mygo/ui"
)

func (app *workbench) mysqlUserManagerWorkspace(c *ui.Context, workspace *queryWorkspace) {
	if !workspace.mysqlUserLoaded && !workspace.mysqlUserLoading {
		app.refreshMySQLUsers(workspace)
	}
	ui.Column(c).Fill().Children(func() {
		ui.Row(c).FillWidth().Padding(10, 14).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Icon(c, treeItemIcons["users"]).Size(20, 20).TextColor(treeItemIconColors["users"])
			ui.Text(c, app.localized("用户与权限")).Bold().Role(ui.RoleHeading).Level(2)
			ui.Text(c, app.openedLabel).TextColor(c.Theme().TextMuted).SingleLine().Grow(1).MinWidth(0)
			refresh := iconButton(c, app.localized("刷新用户列表"), actionIcons["refresh"])
			refresh.Disabled(app.busy || app.dialogOpen || workspace.mysqlUserLoading)
			if refresh.Clicked() {
				app.refreshMySQLAuth(workspace)
			}
			create := iconPrimaryButton(c, app.localized("新建用户"), actionIcons["add"])
			create.Disabled(app.busy || app.dialogOpen || app.openedReadOnly || !workspace.mysqlUserSQLModeKnown)
			if create.Clicked() {
				workspace.mysqlUserCreateOpen = !workspace.mysqlUserCreateOpen
				workspace.mysqlUserDraftName = ""
				workspace.mysqlUserDraftHost = "%"
				workspace.mysqlUserDraftPassword = ""
			}
		})
		if workspace.mysqlUserLoading {
			ui.Row(c).FillWidth().Padding(10, 14).Gap(8).Children(func() {
				ui.Spinner(c).Size(16, 16).Label(app.localized("正在读取 MySQL 用户…"))
				ui.Text(c, app.localized("正在读取 MySQL 用户…")).TextColor(c.Theme().TextMuted)
			})
		}
		if workspace.mysqlUserError != "" {
			ui.Text(c, app.localized("读取 MySQL 用户失败：")+workspace.mysqlUserError).Padding(10, 14).TextColor(c.Theme().Danger)
		}
		ui.Split(c, &workspace.mysqlUserListWidth, func() {
			app.mysqlUserListPane(c, workspace)
		}, func() {
			ui.Split(c, &workspace.mysqlUserEditorWidth, func() {
				app.mysqlUserDetailPane(c, workspace)
			}, func() {
				app.mysqlUserGrantPane(c, workspace)
			}).Grow(1)
		}).Grow(1)
	})
}

func (app *workbench) mysqlUserListPane(c *ui.Context, workspace *queryWorkspace) {
	ui.Column(c).Fill().MinWidth(0).Children(func() {
		ui.Row(c).FillWidth().Padding(8).Gap(6).AlignItems(ui.Center).Children(func() {
			search := ui.TextInput(c, &workspace.mysqlUserSearch).Height(34).Padding(2, 8).
				Placeholder(app.localized("搜索用户或 Host")).Label(app.localized("搜索用户或 Host")).Grow(1)
			search.Disabled(app.busy || app.dialogOpen)
			ui.Text(c, fmt.Sprintf(app.localized("%d 个用户"), len(workspace.mysqlUsers))).TextColor(c.Theme().TextMuted)
		})
		if workspace.mysqlUserCreateOpen {
			ui.Column(c).FillWidth().Padding(8).Gap(6).Background(c.Theme().Surface).Border(1, c.Theme().Border).Radius(c.Theme().Radius).Children(func() {
				ui.Text(c, app.localized("新建 MySQL 用户")).Bold().Role(ui.RoleHeading).Level(3)
				name := ui.TextInput(c, &workspace.mysqlUserDraftName).Height(32).Placeholder(app.localized("用户名")).Label(app.localized("用户名"))
				name.Disabled(app.busy || app.dialogOpen || app.openedReadOnly || !workspace.mysqlUserSQLModeKnown)
				host := ui.TextInput(c, &workspace.mysqlUserDraftHost).Height(32).Placeholder("%").Label(app.localized("主机"))
				host.Disabled(app.busy || app.dialogOpen || app.openedReadOnly || !workspace.mysqlUserSQLModeKnown)
				password := ui.TextInput(c, &workspace.mysqlUserDraftPassword).Height(32).Placeholder(app.localized("密码")).Label(app.localized("密码")).Password()
				password.Disabled(app.busy || app.dialogOpen || app.openedReadOnly || !workspace.mysqlUserSQLModeKnown)
				ui.Row(c).Gap(6).Children(func() {
					create := iconPrimaryButton(c, app.localized("创建用户"), actionIcons["save"])
					create.Disabled(app.busy || app.dialogOpen || app.openedReadOnly || !workspace.mysqlUserSQLModeKnown)
					if create.Clicked() {
						app.createMySQLUser(workspace)
					}
					cancel := iconButton(c, app.localized("取消"), actionIcons["close"])
					if cancel.Clicked() {
						clearMySQLUserSecret(&workspace.mysqlUserDraftPassword)
						workspace.mysqlUserCreateOpen = false
					}
				})
			})
		}
		ui.Scroll(c).FillWidth().Grow(1).MinHeight(0).Children(func() {
			ui.Column(c).FillWidth().Padding(4, 6).Gap(2).Children(func() {
				filter := strings.ToLower(strings.TrimSpace(workspace.mysqlUserSearch))
				for _, account := range workspace.mysqlUsers {
					if filter != "" && !strings.Contains(strings.ToLower(account.Name+"@"+account.Host+" "+account.Plugin), filter) {
						continue
					}
					account := account
					button := ui.Button(c, "").FillWidth().Padding(8).Selected(account.key() == workspace.mysqlUserSelected)
					button.Disabled(app.busy || app.dialogOpen)
					button.Children(func() {
						ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
							ui.Icon(c, treeItemIcons["users"]).Size(18, 18).Shrink(0).TextColor(treeItemIconColors["users"])
							ui.Column(c).Grow(1).MinWidth(0).Gap(2).Children(func() {
								ui.Text(c, account.Name+"@"+account.Host).SingleLine().Ellipsis("…")
								ui.Text(c, account.Plugin).SingleLine().Ellipsis("…").TextColor(c.Theme().TextMuted).FontSize(c.Theme().FontSize - 1)
							})
							if account.Locked {
								ui.Text(c, app.localized("已锁定")).TextColor(c.Theme().Warning)
							}
						})
					})
					if button.Clicked() {
						workspace.mysqlUserSelected = account.key()
						workspace.mysqlUserGrants = nil
						workspace.mysqlUserGrantsError = ""
						app.loadMySQLUserGrants(workspace)
					}
				}
			})
		})
	})
}

func (app *workbench) mysqlUserDetailPane(c *ui.Context, workspace *queryWorkspace) {
	ui.Column(c).Fill().MinWidth(0).Children(func() {
		account, ok := selectedMySQLAccount(workspace)
		if !ok {
			ui.Column(c).Fill().Justify(ui.Center).AlignItems(ui.Center).Children(func() {
				ui.Text(c, app.localized("选择一个用户查看授权")).TextColor(c.Theme().TextMuted)
			})
			return
		}
		ui.Row(c).FillWidth().Padding(10, 12).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Icon(c, treeItemIcons["users"]).Size(20, 20).TextColor(treeItemIconColors["users"])
			ui.Text(c, account.Name+"@"+account.Host).Bold().SingleLine().Ellipsis("…").Grow(1).MinWidth(0)
			ui.Text(c, account.Plugin).Padding(4, 8).Background(c.Theme().Surface).Border(1, c.Theme().Border).Radius(c.Theme().Radius).TextColor(c.Theme().TextMuted).SingleLine()
		})
		ui.Row(c).FillWidth().Padding(8, 12).Gap(6).Children(func() {
			password := iconButton(c, app.localized("修改密码"), treeItemIcons["key"])
			password.Disabled(app.busy || app.dialogOpen || app.openedReadOnly || !workspace.mysqlUserSQLModeKnown)
			if password.Clicked() {
				workspace.mysqlUserPasswordOpen = !workspace.mysqlUserPasswordOpen
				workspace.mysqlUserNewPassword = ""
				workspace.mysqlUserHostOpen = false
			}
			host := iconButton(c, app.localized("修改主机"), actionIcons["edit"])
			host.Disabled(app.busy || app.dialogOpen || app.openedReadOnly || !workspace.mysqlUserSQLModeKnown)
			if host.Clicked() {
				workspace.mysqlUserHostOpen = !workspace.mysqlUserHostOpen
				workspace.mysqlUserNewHost = account.Host
				workspace.mysqlUserPasswordOpen = false
			}
			if workspace.mysqlUserLockSupported {
				label, icon := "锁定用户", actionIcons["close"]
				if account.Locked {
					label, icon = "解锁用户", actionIcons["check"]
				}
				lock := iconButton(c, app.localized(label), icon)
				lock.Disabled(app.busy || app.dialogOpen || app.openedReadOnly || !workspace.mysqlUserSQLModeKnown)
				if lock.Clicked() {
					app.toggleMySQLUserLock(workspace, account)
				}
			}
			remove := iconButton(c, app.localized("删除用户"), actionIcons["trash"])
			remove.Disabled(app.busy || app.dialogOpen || app.openedReadOnly || !workspace.mysqlUserSQLModeKnown)
			if remove.Clicked() {
				app.deleteMySQLUser(workspace, account)
			}
		})
		if workspace.mysqlUserPasswordOpen {
			ui.Row(c).FillWidth().Padding(8, 12).Gap(6).Children(func() {
				password := ui.TextInput(c, &workspace.mysqlUserNewPassword).Height(32).Placeholder(app.localized("新密码")).Label(app.localized("新密码")).Password().Grow(1)
				password.Disabled(app.busy || app.dialogOpen || app.openedReadOnly || !workspace.mysqlUserSQLModeKnown)
				apply := iconPrimaryButton(c, app.localized("更新密码"), actionIcons["save"])
				apply.Disabled(app.busy || app.dialogOpen || app.openedReadOnly || !workspace.mysqlUserSQLModeKnown)
				if apply.Clicked() {
					app.changeMySQLUserPassword(workspace, account)
				}
			})
		}
		if workspace.mysqlUserHostOpen {
			ui.Row(c).FillWidth().Padding(8, 12).Gap(6).Children(func() {
				host := ui.TextInput(c, &workspace.mysqlUserNewHost).Height(32).Placeholder(app.localized("主机")).Label(app.localized("主机")).Grow(1)
				host.Disabled(app.busy || app.dialogOpen || app.openedReadOnly || !workspace.mysqlUserSQLModeKnown)
				apply := iconPrimaryButton(c, app.localized("更新主机"), actionIcons["save"])
				apply.Disabled(app.busy || app.dialogOpen || app.openedReadOnly || !workspace.mysqlUserSQLModeKnown)
				if apply.Clicked() {
					app.changeMySQLUserHost(workspace, account)
				}
			})
		}
		ui.Row(c).FillWidth().Padding(12).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Icon(c, treeItemIcons["trigger"]).Size(18, 18).TextColor(treeItemIconColors["trigger"])
			ui.Text(c, app.localized("授权")).Bold().Role(ui.RoleHeading).Level(3)
			ui.Text(c, fmt.Sprintf(app.localized("%d 条授权语句"), len(workspace.mysqlUserGrants))).TextColor(c.Theme().TextMuted).Grow(1)
		})
		if workspace.mysqlUserGrantsLoading {
			ui.Row(c).Padding(12).Gap(8).Children(func() {
				ui.Spinner(c).Size(16, 16).Label(app.localized("正在读取授权…"))
				ui.Text(c, app.localized("正在读取授权…")).TextColor(c.Theme().TextMuted)
			})
		}
		if workspace.mysqlUserGrantsError != "" {
			ui.Text(c, app.localized("读取授权失败：")+workspace.mysqlUserGrantsError).Padding(12).TextColor(c.Theme().Warning)
		}
		ui.Scroll(c).Fill().Padding(10, 12).Children(func() {
			ui.Column(c).FillWidth().Gap(8).Children(func() {
				for _, grant := range workspace.mysqlUserGrants {
					ui.Text(c, grant).FillWidth().Padding(10).Background(c.Theme().Surface).Border(1, c.Theme().Border).Radius(c.Theme().Radius).Selectable()
				}
			})
		})
	})
}

func (app *workbench) mysqlUserGrantPane(c *ui.Context, workspace *queryWorkspace) {
	ui.Column(c).Fill().MinWidth(250).Padding(12).Gap(8).Children(func() {
		ui.Text(c, app.localized("权限编辑")).Bold().Role(ui.RoleHeading).Level(3)
		ui.Text(c, app.localized("选择数据库和权限；授权会追加，不会回收该用户已有的权限。")).TextColor(c.Theme().TextMuted).Wrap()
		selectLabel := app.localized("选择数据库范围")
		globalLabel := app.localized("全局 (*.*)")
		databaseOptions := []string{selectLabel}
		displayToScope := map[string]string{selectLabel: mysqlNoGrantScope}
		scopeToDisplay := map[string]string{mysqlNoGrantScope: selectLabel}
		for _, namespace := range app.sqlNamespaces {
			display := namespace
			if _, exists := displayToScope[display]; exists {
				display = app.localized("数据库") + " · " + namespace
				for suffix := 2; ; suffix++ {
					if _, exists := displayToScope[display]; !exists {
						break
					}
					display = fmt.Sprintf("%s · %d", app.localized("数据库")+" · "+namespace, suffix)
				}
			}
			databaseOptions = append(databaseOptions, display)
			displayToScope[display] = namespace
			scopeToDisplay[namespace] = display
		}
		if _, exists := displayToScope[globalLabel]; exists {
			globalLabel = app.localized("全局") + " (*.*)"
			for suffix := 2; ; suffix++ {
				if _, exists := displayToScope[globalLabel]; !exists {
					break
				}
				globalLabel = fmt.Sprintf("%s (*.*) · %d", app.localized("全局"), suffix)
			}
		}
		databaseOptions = append(databaseOptions, globalLabel)
		displayToScope[globalLabel] = mysqlGlobalGrantScope
		scopeToDisplay[mysqlGlobalGrantScope] = globalLabel
		if workspace.mysqlUserGrantDatabase == "" {
			if workspace.queryNamespace != "" {
				workspace.mysqlUserGrantDatabase = workspace.queryNamespace
			} else if len(app.sqlNamespaces) > 0 {
				workspace.mysqlUserGrantDatabase = app.sqlNamespaces[0]
			}
		}
		legacyGlobalScope := workspace.mysqlUserGrantDatabase == "全局 (*.*)" || workspace.mysqlUserGrantDatabase == "Global (*.*)"
		if legacyGlobalScope && !containsString(app.sqlNamespaces, workspace.mysqlUserGrantDatabase) {
			workspace.mysqlUserGrantDatabase = mysqlGlobalGrantScope
		}
		if workspace.mysqlUserGrantDatabase == "" {
			workspace.mysqlUserGrantDatabase = mysqlNoGrantScope
		}
		databaseSelection, ok := scopeToDisplay[workspace.mysqlUserGrantDatabase]
		if !ok {
			if len(app.sqlNamespaces) > 0 {
				workspace.mysqlUserGrantDatabase = app.sqlNamespaces[0]
				databaseSelection = scopeToDisplay[workspace.mysqlUserGrantDatabase]
			} else {
				workspace.mysqlUserGrantDatabase = mysqlNoGrantScope
				databaseSelection = selectLabel
			}
		}
		selectDB := ui.Select(c, &databaseSelection, databaseOptions).Label(app.localized("数据库范围"))
		selectDB.Disabled(app.busy || app.dialogOpen || app.openedReadOnly)
		if selectDB.Changed() {
			workspace.mysqlUserGrantDatabase = displayToScope[databaseSelection]
		}
		ui.Scroll(c).FillWidth().Grow(1).MinHeight(0).Children(func() {
			ui.Column(c).FillWidth().Gap(2).Children(func() {
				for index, privilege := range mysqlDatabasePrivileges {
					if index >= len(workspace.mysqlUserPrivileges) {
						workspace.mysqlUserPrivileges = append(workspace.mysqlUserPrivileges, make([]bool, len(mysqlDatabasePrivileges)-len(workspace.mysqlUserPrivileges))...)
					}
					checkbox := ui.Checkbox(c, &workspace.mysqlUserPrivileges[index], app.localized(privilege))
					checkbox.Disabled(app.busy || app.dialogOpen || app.openedReadOnly)
				}
			})
		})
		grantOption := ui.Checkbox(c, &workspace.mysqlUserGrantOption, app.localized("允许继续授权 (WITH GRANT OPTION)"))
		grantOption.Disabled(app.busy || app.dialogOpen || app.openedReadOnly)
		ui.Text(c, app.localized("仅对授权语句生效，撤权时不生效。")).TextColor(c.Theme().TextMuted).FontSize(c.Theme().FontSize - 1)
		preview := mysqlGrantPreview(workspace)
		if preview == "" {
			preview = app.localized("选择一个用户、数据库和至少一项权限以生成授权语句。")
		}
		ui.Text(c, preview).FillWidth().MinHeight(84).Padding(10).Background(c.Theme().Surface).Border(1, c.Theme().Border).Radius(c.Theme().Radius).Selectable()
		apply := iconPrimaryButton(c, app.localized("预览并授权"), actionIcons["check"])
		apply.Disabled(app.busy || app.dialogOpen || app.openedReadOnly || !workspace.mysqlUserSQLModeKnown || preview == "" || len(workspace.mysqlUserSelected) == 0 || !mysqlUserHasSelectedPrivileges(workspace))
		if apply.Clicked() {
			_, ok := selectedMySQLAccount(workspace)
			if ok {
				statement := mysqlGrantPreview(workspace)
				app.runMySQLUserMutationAfter(workspace, statement, statement, "授权 MySQL 用户", "已更新 MySQL 用户授权。", func() {
					workspace.mysqlUserPrivileges = make([]bool, len(mysqlDatabasePrivileges))
					workspace.mysqlUserGrantOption = false
				})
			}
		}
	})
}

func selectedMySQLAccount(workspace *queryWorkspace) (mysqlUserAccount, bool) {
	if workspace == nil {
		return mysqlUserAccount{}, false
	}
	for _, account := range workspace.mysqlUsers {
		if account.key() == workspace.mysqlUserSelected {
			return account, true
		}
	}
	return mysqlUserAccount{}, false
}

func mysqlUserHasSelectedPrivileges(workspace *queryWorkspace) bool {
	for _, selected := range workspace.mysqlUserPrivileges {
		if selected {
			return true
		}
	}
	return false
}

func mysqlGrantPreview(workspace *queryWorkspace) string {
	account, ok := selectedMySQLAccount(workspace)
	if !ok {
		return ""
	}
	var privileges []string
	for index, privilege := range mysqlDatabasePrivileges {
		if index < len(workspace.mysqlUserPrivileges) && workspace.mysqlUserPrivileges[index] {
			privileges = append(privileges, privilege)
		}
	}
	if len(privileges) == 0 {
		return ""
	}
	target := ""
	if workspace.mysqlUserGrantDatabase == mysqlGlobalGrantScope {
		target = "*.*"
	} else if workspace.mysqlUserGrantDatabase != "" {
		target = mysqlSQLIdentifier(workspace.mysqlUserGrantDatabase) + ".*"
	}
	if target == "" {
		return ""
	}
	statement := "GRANT " + strings.Join(privileges, ", ") + " ON " + target + " TO " + mysqlAccountSQL(account, workspace.mysqlUserSQLMode)
	if workspace.mysqlUserGrantOption {
		statement += " WITH GRANT OPTION"
	}
	return statement + ";"
}

func (app *workbench) toggleMySQLUserLock(workspace *queryWorkspace, account mysqlUserAccount) {
	verb, state := "LOCK", "锁定"
	if account.Locked {
		verb, state = "UNLOCK", "解锁"
	}
	statement := "ALTER USER " + mysqlAccountSQL(account, workspace.mysqlUserSQLMode) + " ACCOUNT " + verb
	app.runMySQLUserMutation(workspace, statement, statement+";", state+" MySQL 用户", "已"+state+" MySQL 用户。")
}

func (app *workbench) deleteMySQLUser(workspace *queryWorkspace, account mysqlUserAccount) {
	statement := "DROP USER " + mysqlAccountSQL(account, workspace.mysqlUserSQLMode)
	app.runMySQLUserMutation(workspace, statement, statement+";", "删除 MySQL 用户", "已删除 MySQL 用户。")
}

func (app *workbench) changeMySQLUserHost(workspace *queryWorkspace, account mysqlUserAccount) {
	newHost := strings.TrimSpace(workspace.mysqlUserNewHost)
	if newHost == "" || strings.ContainsAny(newHost, "\x00\r\n") {
		app.status = app.localized("Host 不能为空，且不能包含换行符。")
		return
	}
	newAccount := mysqlUserAccount{Name: account.Name, Host: newHost}
	statement := "RENAME USER " + mysqlAccountSQL(account, workspace.mysqlUserSQLMode) + " TO " + mysqlAccountSQL(newAccount, workspace.mysqlUserSQLMode)
	app.runMySQLUserMutationAfter(workspace, statement, statement+";", "修改 MySQL 用户 Host", "已修改 MySQL 用户 Host。", func() {
		workspace.mysqlUserSelected = newAccount.key()
		workspace.mysqlUserHostOpen = false
	})
}

func clearMySQLUserSecret(value *string) {
	if value == nil {
		return
	}
	*value = ""
}
