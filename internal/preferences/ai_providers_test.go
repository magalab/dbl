package preferences

import "testing"

func TestAIProviderMigrationAndIndependentPersistence(t *testing.T) {
	dir := t.TempDir()
	store, err := OpenAt(dir)
	if err != nil {
		t.Fatal(err)
	}
	legacy := AISettings{Endpoint: "https://legacy.example/v1", Model: "legacy-model", APIKeyRef: "v1:0123456789abcdef0123456789abcdef"}
	if err := store.SaveAISettings(legacy, 1); err != nil {
		t.Fatal(err)
	}
	store, err = OpenAt(dir)
	if err != nil {
		t.Fatal(err)
	}
	initial := store.Settings()
	if len(initial.AIProviders) != 1 || initial.AIProviders[0].AISettings != legacy || initial.ActiveAIProviderID != "legacy" {
		t.Fatal("legacy configuration was not migrated intact")
	}
	second := AIProvider{ID: "second", Name: "Other account", AISettings: AISettings{Endpoint: "https://other.example/v1", Model: "other-model", APIKeyRef: "v1:abcdef0123456789abcdef0123456789"}}
	providers := append(initial.AIProviders, second)
	if err := store.SaveAIProviders(providers, second.ID, 1); err != nil {
		t.Fatal(err)
	}
	// Callers cannot mutate the saved collection through either input or output slices.
	providers[1].Name = "changed"
	snapshot := store.Settings()
	snapshot.AIProviders[0].Name = "changed"
	reopened, err := OpenAt(dir)
	if err != nil {
		t.Fatal(err)
	}
	got := reopened.Settings()
	if got.AI != second.AISettings || got.AIProviders[0].AISettings != legacy || got.AIProviders[1].Name != second.Name {
		t.Fatal("provider settings or credentials crossed between accounts")
	}
	if err := reopened.SaveAIProviders(got.AIProviders, "missing", 1); err == nil {
		t.Fatal("accepted missing active provider")
	}
	if reopened.Settings().ActiveAIProviderID != second.ID {
		t.Fatal("failed update changed selection")
	}
	if err := reopened.SaveAIProviders(nil, "", 2); err != nil {
		t.Fatal(err)
	}
	reopened, err = OpenAt(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(reopened.Settings().AIProviders) != 0 || reopened.Settings().AI != (AISettings{}) {
		t.Fatal("removed provider resurrected from legacy settings")
	}
}
