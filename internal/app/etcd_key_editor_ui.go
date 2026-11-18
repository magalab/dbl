package app

import (
	"fmt"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
)

func (app *workbench) etcdCreateKeyModal(c *ui.Context) {
	if !app.etcdCreateKeyOpen {
		return
	}
	app.ensureEtcdNewValueEditor()
	if ui.Modal(c, &app.etcdCreateKeyOpen, func() {
		ui.Column(c).Width(920).MaxWidthPercent(92).MaxHeightPercent(92).
			Padding(16).Gap(12).Background(c.Theme().Surface).Border(1, c.Theme().Border).
			Radius(c.Theme().Radius + 4).Children(func() {
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Text(c, app.localized("新建 etcd 键")).Bold().Role(ui.RoleHeading).Level(2).Grow(1)
				close := iconButton(c, app.localized("关闭"), actionIcons["close"]).Width(30).Height(30).Padding(4)
				if close.Clicked() {
					app.closeEtcdCreateKeyModal()
				}
			})
			ui.Scroll(c).Grow(1).MinHeight(0).Children(func() {
				ui.Column(c).Gap(12).Padding(2, 2, 10, 2).Children(func() {
					key := ui.TextInput(c, &app.etcdNewKey).FillWidth().Placeholder(app.localized("键名，例如 /services/api")).Label(app.localized("Key"))
					key.Disabled(app.busy || app.dialogOpen || app.openedReadOnly)

					ui.Column(c).Gap(8).Padding(10, 12).Background(c.Theme().Background).Border(1, c.Theme().Border).Radius(c.Theme().Radius).Children(func() {
						ui.Text(c, app.localized("有效期策略")).Bold().Role(ui.RoleHeading).Level(3)
						ui.Row(c).FillWidth().Gap(8).Children(func() {
							app.etcdLeasePolicyCard(c, "permanent", "长期有效", "不绑定租约，Key 不自动过期")
							app.etcdLeasePolicyCard(c, "ttl", "设置 TTL", "按指定秒数自动过期")
							app.etcdLeasePolicyCard(c, "existing", "绑定租约", "使用已有 Lease ID")
						})
						if app.etcdNewLeasePolicy == "ttl" {
							ttl := ui.TextInput(c, &app.etcdNewLeaseTTL).Width(220).Placeholder("60").Label(app.localized("TTL 秒数"))
							ttl.Disabled(app.busy || app.dialogOpen || app.openedReadOnly)
						} else if app.etcdNewLeasePolicy == "existing" {
							lease := ui.TextInput(c, &app.etcdNewLease).Width(280).Placeholder(app.localized("输入已有 Lease ID")).Label(app.localized("Lease ID"))
							lease.Disabled(app.busy || app.dialogOpen || app.openedReadOnly)
							ui.Text(c, app.localized("可在顶栏租约页面创建租约并查看 Lease ID。")).TextColor(c.Theme().TextMuted)
						}
					})

					ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
						ui.Column(c).Grow(1).Gap(2).Children(func() {
							ui.Text(c, app.localized("Value 内容")).Bold()
							ui.Text(c, fmt.Sprintf(app.localized("%d / %d 字节"), len(app.etcdNewValueEditor.Buffer.String()), 1<<20)).TextColor(c.Theme().TextMuted).SingleLine()
						})
						format := []string{"Text", "Blob (Base64)", "JSON", "YAML", "XML", "SQL", "Properties", "Shell", "Dockerfile"}
						if !containsString(format, app.etcdNewValueFormat) {
							app.etcdNewValueFormat = format[0]
						}
						formatSelect := ui.Select(c, &app.etcdNewValueFormat, format).Width(190).Label(app.localized("值格式"))
						formatSelect.Disabled(app.busy || app.dialogOpen || app.openedReadOnly)
						if formatSelect.Changed() {
							app.etcdNewValueEditor.Buffer.SetLanguage(etcdEditorLanguage(app.etcdNewValueFormat))
						}
					})
					app.applyEditorFont(app.etcdNewValueEditor)
					value := editor.View(c, app.etcdNewValueEditor).FillWidth().Height(360).Label(app.localized("值编辑器"))
					value.Disabled(app.busy || app.dialogOpen || app.openedReadOnly)
					ui.Text(c, app.localized("格式仅控制语法高亮，值会按输入内容原样保存。")).TextColor(c.Theme().TextMuted).SingleLine()
					if len(app.etcdNewValueEditor.Buffer.String()) > 1<<20 {
						ui.Text(c, app.localized("etcd 值不能超过 1 MiB。")).TextColor(c.Theme().Danger)
					}
				})
			})
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Spacer(c)
				cancel := iconButton(c, app.localized("取消"), actionIcons["close"]).Size(34, 34).Padding(5)
				if cancel.Clicked() {
					app.closeEtcdCreateKeyModal()
				}
				create := iconPrimaryButton(c, app.localized("预览并创建"), actionIcons["add"]).Size(34, 34).Padding(5)
				create.Disabled(app.busy || app.dialogOpen || app.openedReadOnly)
				if create.Clicked() {
					app.etcdNewValue = app.etcdNewValueEditor.Buffer.String()
					app.confirmEtcdKeyCreation()
				}
			})
		})
	}).Dismissed() {
		app.closeEtcdCreateKeyModal()
	}
}

func (app *workbench) etcdLeasePolicyCard(c *ui.Context, policy, title, description string) {
	selected := app.etcdNewLeasePolicy == policy
	card := ui.Button(c, "").Label(app.localized(title)).Tooltip(app.localized(description)).Grow(1).Height(88).Padding(10, 12).Selected(selected)
	card.Children(func() {
		ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
			mark := "○"
			if selected {
				mark = "◉"
			}
			ui.Text(c, mark).TextColor(c.Theme().Accent)
			ui.Column(c).Gap(3).Children(func() {
				ui.Text(c, app.localized(title)).Bold()
				ui.Text(c, app.localized(description)).TextColor(c.Theme().TextMuted)
			})
		})
	})
	if card.Clicked() {
		app.etcdNewLeasePolicy = policy
	}
}

func (app *workbench) ensureEtcdNewValueEditor() {
	if app.etcdNewValueFormat == "" {
		app.etcdNewValueFormat = "Text"
	}
	if app.etcdNewLeasePolicy == "" {
		app.etcdNewLeasePolicy = "permanent"
	}
	if app.etcdNewLeaseTTL == "" {
		app.etcdNewLeaseTTL = "60"
	}
	if app.etcdNewValueEditor == nil {
		buffer := editor.NewBuffer(app.etcdNewValue)
		app.etcdNewValueEditor = &editor.State{Buffer: buffer, SoftWrap: true}
	}
	app.etcdNewValueEditor.Buffer.SetLanguage(etcdEditorLanguage(app.etcdNewValueFormat))
}

func (app *workbench) closeEtcdCreateKeyModal() {
	app.etcdCreateKeyOpen = false
	app.etcdNewKey, app.etcdNewValue, app.etcdNewLease = "", "", ""
	app.etcdNewLeaseTTL, app.etcdNewLeasePolicy, app.etcdNewValueFormat = "", "", ""
	app.etcdNewValueEditor = nil
}

func (app *workbench) beginEtcdCreateKey() {
	app.closeEtcdCreateKeyModal()
	app.etcdCreateKeyOpen = true
	app.ensureEtcdNewValueEditor()
}

func etcdEditorLanguage(format string) *editor.Language {
	name := strings.ToLower(format)
	switch format {
	case "Text", "Blob (Base64)":
		return nil
	case "Properties":
		name = "ini"
	case "Dockerfile":
		name = "dockerfile"
	}
	return editor.LanguageNamed(name)
}
