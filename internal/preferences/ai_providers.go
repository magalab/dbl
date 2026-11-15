package preferences

import (
	"errors"
	"strings"
)

// AIProvider contains no secrets; each provider owns an opaque vault reference.
type AIProvider struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	AISettings
}

func normalizeAISettings(settings AISettings) AISettings {
	settings.Endpoint = strings.TrimSpace(settings.Endpoint)
	settings.Model = strings.TrimSpace(settings.Model)
	settings.APIKeyRef = strings.TrimSpace(settings.APIKeyRef)
	return settings
}

func migrateAIProviders(settings Settings) Settings {
	if len(settings.AIProviders) == 0 && settings.AI != (AISettings{}) {
		settings.AIProviders = []AIProvider{{ID: "legacy", Name: "Default", AISettings: settings.AI}}
		settings.ActiveAIProviderID = "legacy"
	}
	for _, provider := range settings.AIProviders {
		if provider.ID == settings.ActiveAIProviderID {
			settings.AI = provider.AISettings
			return settings
		}
	}
	if len(settings.AIProviders) > 0 {
		settings.ActiveAIProviderID = settings.AIProviders[0].ID
		settings.AI = settings.AIProviders[0].AISettings
	}
	return settings
}

// SaveAIProviders atomically saves the collection and selected provider.
func (store *Store) SaveAIProviders(providers []AIProvider, activeID string, revision uint64) error {
	if len(providers) > 100 {
		return errors.New("too many AI providers")
	}
	providers = append([]AIProvider(nil), providers...)
	seen := make(map[string]bool)
	var active AISettings
	found := activeID == "" && len(providers) == 0
	for i := range providers {
		p := &providers[i]
		p.Name = strings.TrimSpace(p.Name)
		if p.ID == "" || seen[p.ID] || p.Name == "" {
			return errors.New("AI providers require unique IDs and non-empty names")
		}
		seen[p.ID] = true
		p.AISettings = normalizeAISettings(p.AISettings)
		if err := validateAISettings(p.AISettings); err != nil {
			return err
		}
		if p.ID == activeID {
			active, found = p.AISettings, true
		}
	}
	if !found {
		return errors.New("active AI provider not found")
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if revision <= store.revision {
		return errors.New("stale preferences update")
	}
	updated := store.settings
	updated.Version = 1
	updated.AIProviders, updated.ActiveAIProviderID, updated.AI = providers, activeID, active
	if err := writeSettings(store.dir, updated); err != nil {
		return err
	}
	store.settings, store.revision = updated, revision
	return nil
}
