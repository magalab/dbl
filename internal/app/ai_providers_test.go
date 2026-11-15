package app

import (
	"github.com/egoist/mygo/ui"
	"github.com/magalab/dbl/internal/assistant"
	"github.com/magalab/dbl/internal/preferences"
	"testing"
)

func TestAIWorkspaceHidesRequestInternalsAndExpandsOptions(t *testing.T) {
	app := newTestWorkbench()
	app.showAI = true
	app.leftWidth, app.aiPanelWidth = 220, 380
	app.aiPreview = "PRIVATE_PAYLOAD_SENTINEL"
	app.aiPrepared = true
	tester := ui.NewTester(app.view, 1440, 1000)
	if tester.HasText("PRIVATE_PAYLOAD_SENTINEL") || tester.HasText("丢弃预览") || tester.HasText("提示模板") {
		t.Fatal("default workspace exposes request internals or expanded options")
	}
	if _, ok := tester.Find("发送消息"); !ok {
		t.Fatal("missing direct send action")
	}
	if err := tester.Click("展开任务与上下文"); err != nil {
		t.Fatal(err)
	}
	if !tester.HasText("提示模板") {
		t.Fatal("task options did not expand")
	}
}

func TestAIProviderSwitchAndRegenerationUseSelectedAccount(t *testing.T) {
	app := newAIRequestWorkbench()
	store, err := preferences.OpenAt(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	app.preferences = store
	first := preferences.AIProvider{ID: "a", Name: "A", AISettings: app.aiSettings}
	second := preferences.AIProvider{ID: "b", Name: "B", AISettings: app.aiSettings}
	second.Endpoint, second.Model = "https://second.example/v1", "second-model"
	second.APIKeyRef = "v1:abcdef0123456789abcdef0123456789"
	app.aiProviders = []preferences.AIProvider{first, second}
	app.loadAIProvider(first)
	app.aiPreparedMessages = []assistant.Message{{Role: "user", Content: "previous request"}}
	app.aiPreparedSettings = first.AISettings
	if !app.persistAIProviders(app.aiProviders, second.ID) {
		t.Fatal("switch did not persist")
	}
	app.loadAIProvider(second)
	app.prepareAIRegeneration()
	if app.aiPreparedSettings != second.AISettings || app.aiPreparedMessages[0].Content != "previous request" {
		t.Fatal("regeneration used stale provider or changed messages")
	}
	app.aiPrepared = false
	app.aiProviderName = "unsaved name"
	if !app.aiSettingsDirty() {
		t.Fatal("unsaved provider name not detected")
	}
	app.cancelAIProviderEdit()
	if app.aiSettingsDirty() || app.aiProviderName != "B" {
		t.Fatal("cancel did not restore saved provider")
	}
}

func TestModelSwitchPersistsOnlySelectedProvider(t *testing.T) {
	app := newAIRequestWorkbench()
	store, err := preferences.OpenAt(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	app.preferences = store
	a := preferences.AIProvider{ID: "a", Name: "A", AISettings: app.aiSettings}
	b := preferences.AIProvider{ID: "b", Name: "B", AISettings: app.aiSettings}
	app.aiProviders = []preferences.AIProvider{a, b}
	app.loadAIProvider(a)
	app.switchAIModel("chosen-model")
	if app.aiModel != "chosen-model" || store.Settings().AI.Model != "chosen-model" || app.aiProviders[1].Model != b.Model {
		t.Fatal("model selection was not isolated and persisted")
	}
	app.aiModel = "unsaved-model"
	app.switchAIModel("must-not-save")
	if store.Settings().AI.Model != "chosen-model" {
		t.Fatal("switch overwrote unsaved edits")
	}
}

func TestBeginAIProviderAddKeepsSavedProviderVisible(t *testing.T) {
	app := newAIRequestWorkbench()
	p := preferences.AIProvider{ID: "saved", Name: "Saved account", AISettings: app.aiSettings}
	app.aiProviders = []preferences.AIProvider{p}
	app.loadAIProvider(p)
	app.beginAIProviderAdd()
	if app.aiProviderID != p.ID || app.aiProviderNew != true || len(app.aiProviders) != 1 || app.aiProviders[0] != p {
		t.Fatal("adding a provider hid or changed saved provider selection")
	}
	if app.aiSettingsDirty() != true || app.aiEndpoint != "" || app.aiAPIKey != "" {
		t.Fatal("new provider draft was not initialized")
	}
	app.cancelAIProviderEdit()
	if app.aiProviderID != p.ID || app.aiSettings != p.AISettings || app.aiProviderNew {
		t.Fatal("cancel did not restore the active saved provider")
	}
}

func TestAIProviderDisplayNamesAreReadableAndDisambiguateDuplicates(t *testing.T) {
	providers := []preferences.AIProvider{
		{ID: "a", Name: "MiniMax", AISettings: preferences.AISettings{Endpoint: "https://api.minimaxi.com/v1"}},
		{ID: "b", Name: "MiniMax", AISettings: preferences.AISettings{Endpoint: "https://api.minimax.io/v1"}},
		{ID: "c", Name: "Local", AISettings: preferences.AISettings{Endpoint: "http://localhost:8080/v1"}},
	}
	got := aiProviderDisplayNames(providers)
	want := []string{"MiniMax · api.minimaxi.com", "MiniMax · api.minimax.io", "Local"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("provider label %d = %q, want %q", i, got[i], want[i])
		}
	}
}
