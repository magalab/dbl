package app

import (
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/magalab/dbl/internal/assistant"
	"github.com/magalab/dbl/internal/credentials"
	"github.com/magalab/dbl/internal/preferences"
)

func (app *workbench) aiModelCacheKey() string {
	return app.aiProviderID + "\n" + strings.TrimRight(strings.TrimSpace(app.aiEndpoint), "/")
}

func (app *workbench) fetchAIModels() {
	if app.busy || app.aiRunning || app.aiModelsLoading || app.aiRemoveKey {
		return
	}
	key := []byte(app.aiAPIKey)
	ref, endpoint, cacheKey := app.aiSettings.APIKeyRef, app.aiEndpoint, app.aiModelCacheKey()
	app.aiModelsLoading, app.busy, app.aiModelsError = true, true, ""
	ctx, cancel := app.withTaskTimeout(20 * time.Second)
	go func() {
		defer cancel()
		var err error
		if len(key) == 0 {
			key, err = app.vault.Load(credentials.Reference(ref))
		}
		defer clear(key)
		var models []string
		if err == nil {
			models, err = (assistant.OpenAICompatible{Endpoint: endpoint, APIKey: key}).Models(ctx)
		}
		app.window.Update(func() {
			app.aiModelsLoading, app.busy = false, false
			if err != nil {
				app.aiModelsError = app.localized("获取失败，可手动填写模型名称")
			} else {
				if app.aiModelCache == nil {
					app.aiModelCache = make(map[string][]string)
				}
				app.aiModelCache[cacheKey] = models
			}
			app.invalidateSettingsWindow()
			if app.window != nil {
				app.window.Invalidate()
			}
		})
	}()
}

func (app *workbench) aiModelSelector(c *ui.Context) {
	models := app.aiModelCache[app.aiModelCacheKey()]
	app.settingRow(c, "模型名称", func() {
		ui.Row(c).Width(260).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Autocomplete(c, &app.aiModel, models).Label(app.localized("模型名称")).Placeholder(app.localized("输入或选择模型")).Grow(1).MinWidth(0).Disabled(app.busy || app.aiRunning || app.exporting || app.dialogOpen)
			app.aiModelFetchButton(c)
		})
	})
	if app.aiModelsError != "" {
		ui.Text(c, app.aiModelsError).TextColor(c.Theme().TextMuted)
	}
}

func (app *workbench) aiWorkspaceModelSelector(c *ui.Context) {
	models := app.aiModelCache[app.aiModelCacheKey()]
	options := append([]string(nil), models...)
	selected := app.aiModel
	if selected != "" {
		found := false
		for _, model := range options {
			if model == selected {
				found = true
				break
			}
		}
		if !found {
			options = append([]string{selected}, options...)
		}
	} else if len(options) == 0 {
		selected = app.localized("刷新模型列表后选择")
		options = []string{selected}
	}
	previous := selected

	ui.Row(c).FillWidth().Gap(6).AlignItems(ui.Center).Children(func() {
		picker := ui.Select(c, &selected, options).Label(app.localized("模型")).Grow(1).MinWidth(100)
		picker.Disabled(len(models) == 0 || app.busy || app.aiRunning || app.exporting || app.dialogOpen || app.aiSettingsDirty())

		refreshLabel := "刷新模型列表"
		if app.aiModelsLoading {
			refreshLabel = "正在获取模型…"
		}
		refresh := iconButton(c, app.localized(refreshLabel), actionIcons["refresh"]).Size(30, 30).Padding(4)
		refresh.Disabled(app.busy || app.aiRunning || app.exporting || app.dialogOpen || app.aiRemoveKey || strings.TrimSpace(app.aiEndpoint) == "" || (app.aiAPIKey == "" && app.aiSettings.APIKeyRef == ""))
		if refresh.Clicked() {
			app.fetchAIModels()
		}
	})
	if selected != previous {
		app.switchAIModel(selected)
	}
	if app.aiModelsError != "" {
		ui.Text(c, app.aiModelsError).TextColor(c.Theme().TextMuted)
	}
}

func (app *workbench) aiModelFetchButton(c *ui.Context) {
	label := "获取模型"
	if app.aiModelsLoading {
		label = "正在获取模型…"
	}
	fetch := iconButton(c, app.localized(label), actionIcons["refresh"]).Size(30, 30).Padding(4)
	fetch.Disabled(app.busy || app.aiRunning || app.exporting || app.dialogOpen || app.aiRemoveKey || strings.TrimSpace(app.aiEndpoint) == "" || (app.aiAPIKey == "" && app.aiSettings.APIKeyRef == ""))
	if fetch.Clicked() {
		app.fetchAIModels()
	}
}

func (app *workbench) switchAIModel(model string) {
	if app.busy || app.aiRunning || app.aiSettingsDirty() {
		return
	}
	providers := append([]preferences.AIProvider(nil), app.aiProviders...)
	for i := range providers {
		if providers[i].ID == app.aiProviderID {
			providers[i].Model = model
			if app.persistAIProviders(providers, app.aiDefaultProviderID) {
				app.loadAIProvider(providers[i])
			}
			return
		}
	}
}
