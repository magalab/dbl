package app

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/magalab/dbl/internal/credentials"
	"github.com/magalab/dbl/internal/preferences"
)

func aiProviderUsesKey(providers []preferences.AIProvider, ref string) bool {
	for _, p := range providers {
		if p.APIKeyRef == ref {
			return true
		}
	}
	return false
}

func (app *workbench) aiProviderNameDirty() bool {
	for _, p := range app.aiProviders {
		if p.ID == app.aiProviderID {
			return strings.TrimSpace(app.aiProviderName) != p.Name
		}
	}
	return strings.TrimSpace(app.aiProviderName) != ""
}

func (app *workbench) beginAIProviderAdd() {
	// Keep the saved selection visible while editing a separate new-provider draft.
	app.aiProviderNew = true
	app.aiProviderName = ""
	app.aiSettings = preferences.AISettings{}
	app.aiEnabled = false
	app.aiEndpoint, app.aiModel, app.aiAPIKey = "", "", ""
	app.aiRemoveKey = false
	app.aiModelsError = ""
	app.invalidateSettingsWindow()
}

func (app *workbench) loadAIProvider(p preferences.AIProvider) {
	app.aiModelsError = ""
	app.aiProviderID, app.aiProviderName, app.aiProviderNew = p.ID, p.Name, false
	app.aiProviderNameEditing = false
	app.aiSettings, app.aiEnabled = p.AISettings, p.Enabled
	app.aiEndpoint, app.aiModel, app.aiAPIKey, app.aiRemoveKey = p.Endpoint, p.Model, "", false
	app.invalidateSettingsWindow()
}

func aiProviderDisplayNames(providers []preferences.AIProvider) []string {
	counts := make(map[string]int, len(providers))
	for _, p := range providers {
		counts[strings.TrimSpace(p.Name)]++
	}
	result := make([]string, len(providers))
	used := make(map[string]int)
	for i, p := range providers {
		name := strings.TrimSpace(p.Name)
		if counts[name] > 1 {
			host := ""
			if endpoint, err := url.Parse(p.Endpoint); err == nil {
				host = endpoint.Hostname()
			}
			if host != "" {
				name += " · " + host
			}
		}
		used[name]++
		if used[name] > 1 {
			name += fmt.Sprintf(" (%d)", used[name])
		}
		result[i] = name
	}
	return result
}

func (app *workbench) aiProviderPicker(c *ui.Context, settings bool) {
	if !settings {
		if len(app.aiProviders) == 0 {
			ui.Text(c, app.localized("添加 provider 后开始对话")).TextColor(c.Theme().TextMuted)
		} else {
			options := aiProviderDisplayNames(app.aiProviders)
			selected := ""
			for i, p := range app.aiProviders {
				if p.ID == app.aiDefaultProviderID {
					selected = options[i]
				}
			}
			previous := selected
			picker := ui.Select(c, &selected, options).Label(app.localized("AI provider")).Grow(1).MinWidth(120)
			picker.Disabled(app.aiRunning || app.busy || app.exporting || app.dialogOpen || app.aiSettingsDirty())
			if selected != previous {
				for i, option := range options {
					if option == selected {
						p := app.aiProviders[i]
						if app.persistAIProviders(app.aiProviders, p.ID) {
							app.loadAIProvider(p)
						}
						break
					}
				}
			}
		}
		if len(app.aiProviders) > 0 && !app.aiSettings.Enabled {
			ui.Text(c, app.localized("AI 助手未启用")).TextColor(c.Theme().TextMuted)
		}
		if app.aiSettingsDirty() {
			ui.Text(c, app.localized("请先保存或取消 provider 修改")).TextColor(c.Theme().TextMuted)
		}
		return
	}

	ui.Column(c).FillWidth().Gap(5).Children(func() {
		ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(6).Children(func() {
			ui.Text(c, app.localized("AI provider")).Role(ui.RoleHeading).Level(3).Grow(1)
			add := iconButton(c, app.localized("添加 provider"), actionIcons["add"]).Size(30, 30).Padding(4)
			add.Disabled(app.aiRunning || app.busy || app.exporting || app.dialogOpen || app.aiSettingsDirty() || app.aiProviderNew)
			if add.Clicked() {
				app.beginAIProviderAdd()
			}
			if app.aiSettingsDirty() {
				cancel := iconButton(c, app.localized("取消修改"), actionIcons["close"]).Size(30, 30).Padding(4)
				cancel.Disabled(app.aiRunning || app.busy || app.exporting || app.dialogOpen)
				if cancel.Clicked() {
					app.cancelAIProviderEdit()
				}
			}
		})
		for _, provider := range app.aiProviders {
			provider := provider
			isDefault := provider.ID == app.aiDefaultProviderID
			isEditing := provider.ID == app.aiProviderID
			ui.Row(c).FillWidth().Gap(6).AlignItems(ui.Center).Children(func() {
				if isEditing && app.aiProviderNameEditing {
					name := ui.TextInput(c, &app.aiProviderName).Placeholder(app.localized("provider 名称，例如 DeepSeek"))
					name.Grow(1).MinWidth(0).Disabled(app.aiRunning || app.busy || app.exporting || app.dialogOpen)
				} else {
					selectProvider := ui.Button(c, "").Grow(1).MinWidth(0).Selected(isEditing).Tooltip(app.localized("双击重命名 provider"))
					selectProvider.Children(func() {
						ui.Row(c).FillWidth().Padding(5, 10).Children(func() { ui.Text(c, provider.Name).Grow(1) })
					})
					selectProvider.Disabled(app.aiRunning || app.busy || app.exporting || app.dialogOpen || app.aiSettingsDirty())
					if selectProvider.DoubleClicked() {
						if !isEditing {
							app.loadAIProvider(provider)
						}
						app.aiProviderNameEditing = true
						app.invalidateSettingsWindow()
					} else if selectProvider.Clicked() && !isEditing {
						app.loadAIProvider(provider)
					}
				}

				defaultLabel := "设为默认"
				if isDefault {
					defaultLabel = "默认"
				}
				setDefault := ui.Button(c, app.localized(defaultLabel)).Padding(5, 8)
				setDefault.Disabled(isDefault || app.aiRunning || app.busy || app.exporting || app.dialogOpen || app.aiSettingsDirty())
				if setDefault.Clicked() {
					app.setDefaultAIProvider(provider)
				}
				remove := iconButton(c, app.localized("删除 provider")+": "+provider.Name, actionIcons["trash"]).Size(30, 30).Padding(4)
				remove.Disabled(app.aiRunning || app.busy || app.exporting || app.dialogOpen || app.aiSettingsDirty())
				if remove.Clicked() {
					app.deleteAIProvider(provider.ID)
				}
			})
		}

		if app.aiProviderNew {
			ui.Row(c).FillWidth().Gap(6).AlignItems(ui.Center).Children(func() {
				name := ui.TextInput(c, &app.aiProviderName).Placeholder(app.localized("provider 名称，例如 DeepSeek"))
				name.Grow(1).MinWidth(0).Disabled(app.aiRunning || app.busy || app.exporting || app.dialogOpen)
				cancel := iconButton(c, app.localized("取消新增 provider"), actionIcons["close"]).Size(30, 30).Padding(4)
				cancel.Disabled(app.aiRunning || app.busy || app.exporting || app.dialogOpen)
				if cancel.Clicked() {
					app.cancelAIProviderEdit()
				}
			})
		}
		if len(app.aiProviders) == 0 && !app.aiProviderNew {
			ui.Text(c, app.localized("尚无 provider，请添加一个。 ")).TextColor(c.Theme().TextMuted)
		}
		if app.aiSettingsDirty() && !app.aiProviderNew {
			ui.Text(c, app.localized("请先保存或取消 provider 修改")).TextColor(c.Theme().TextMuted)
		}
	})
}

func (app *workbench) setDefaultAIProvider(provider preferences.AIProvider) {
	if app.aiSettingsDirty() || !app.persistAIProviders(app.aiProviders, provider.ID) {
		return
	}
	app.aiDefaultProviderID = provider.ID
	app.loadAIProvider(provider)
}

func (app *workbench) persistAIProviders(providers []preferences.AIProvider, activeID string) bool {
	if app.preferences == nil {
		return false
	}
	app.preferenceRevision++
	if err := app.preferences.SaveAIProviders(providers, activeID, app.preferenceRevision); err != nil {
		app.reportError("保存 AI 设置失败", err)
		return false
	}
	app.aiProviders = providers
	app.aiDefaultProviderID = activeID
	return true
}

func (app *workbench) cancelAIProviderEdit() {
	id := app.aiDefaultProviderID
	if app.preferences != nil {
		id = app.preferences.Settings().ActiveAIProviderID
	}
	for _, p := range app.aiProviders {
		if p.ID == id {
			app.loadAIProvider(p)
			return
		}
	}
	app.loadAIProvider(preferences.AIProvider{})
}

func (app *workbench) deleteAIProvider(id string) {
	if app.aiRunning || app.busy || app.aiSettingsDirty() {
		return
	}
	providers := make([]preferences.AIProvider, 0, len(app.aiProviders))
	var removed preferences.AIProvider
	for _, p := range app.aiProviders {
		if p.ID == id {
			removed = p
			continue
		}
		providers = append(providers, p)
	}
	if removed.ID == "" {
		return
	}
	wasDefault := app.aiDefaultProviderID == id
	wasEditing := app.aiProviderID == id
	activeID := app.aiDefaultProviderID
	if activeID == id {
		activeID = ""
		if len(providers) > 0 {
			activeID = providers[0].ID
		}
	}
	if !app.persistAIProviders(providers, activeID) {
		return
	}
	if wasDefault || wasEditing {
		next := preferences.AIProvider{}
		for _, p := range providers {
			if p.ID == app.aiDefaultProviderID {
				next = p
				break
			}
		}
		if next.ID == "" && len(providers) > 0 {
			next = providers[0]
		}
		app.loadAIProvider(next)
	}
	if removed.APIKeyRef != "" && !aiProviderUsesKey(providers, removed.APIKeyRef) && app.vault != nil {
		if err := app.vault.Delete(credentials.Reference(removed.APIKeyRef)); err != nil {
			app.reportError("清理 AI 密钥失败", err)
		}
	}
}
