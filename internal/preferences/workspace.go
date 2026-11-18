package preferences

import (
	"errors"
	"math"
	"strings"
	"unicode"
)

const MaxQueryDrafts = 16
const MaxQueryDraftSQLBytes = 256 << 10

// QueryDraft stores editor content and connection metadata, never live sessions
// or results. ConnectionID is an opaque profile identity or a local SQLite path.
type QueryDraft struct {
	Title        string `json:"title"`
	SQL          string `json:"sql"`
	ConnectionID string `json:"connection_id,omitempty"`
	Namespace    string `json:"namespace,omitempty"`
}

type InterfaceSettings struct {
	StartupMode    string  `json:"startup_mode,omitempty"`
	CloseToTray    bool    `json:"close_to_tray,omitempty"`
	FontFamily     string  `json:"font_family,omitempty"`
	FontSize       float32 `json:"font_size,omitempty"`
	EditorFont     string  `json:"editor_font,omitempty"`
	EditorFontSize float32 `json:"editor_font_size,omitempty"`
}

func validQueryDraft(draft QueryDraft) bool {
	return len(draft.SQL) <= MaxQueryDraftSQLBytes && len(draft.Title) <= 256 &&
		len(draft.ConnectionID) <= 8192 && len(draft.Namespace) <= 1024
}

// SetQueryDrafts merges only this feature's fields so asynchronous saves cannot
// overwrite unrelated preferences. Its revision also fences pending saves when
// the user disables persistence.
func (store *Store) SetQueryDrafts(enabled bool, drafts []QueryDraft, active int, revision uint64) error {
	if !enabled {
		drafts, active = nil, 0
	}
	if len(drafts) > MaxQueryDrafts || active < 0 || len(drafts) == 0 && active != 0 || len(drafts) > 0 && active >= len(drafts) {
		return errors.New("invalid query draft count or active index")
	}
	for _, draft := range drafts {
		if !validQueryDraft(draft) {
			return errors.New("query draft exceeds its storage limit")
		}
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if revision <= store.queryDraftsRevision {
		return nil
	}
	settings := store.settings
	settings.QueryDraftsEnabled, settings.ActiveQueryDraft = enabled, active
	settings.QueryDrafts = append([]QueryDraft(nil), drafts...)
	if err := writeSettings(store.dir, settings); err != nil {
		return err
	}
	store.settings, store.queryDraftsRevision = settings, revision
	return nil
}

func validFontName(name string) bool {
	return len(name) <= 256 && !strings.ContainsFunc(name, unicode.IsControl)
}

func validFontSize(size float32) bool {
	return size == 0 || !math.IsNaN(float64(size)) && size >= 10 && size <= 32
}

func normalizeInterface(settings InterfaceSettings) InterfaceSettings {
	switch settings.StartupMode {
	case "normal", "maximized", "fullscreen":
	default:
		settings.StartupMode = "normal"
	}
	if !validFontName(settings.FontFamily) {
		settings.FontFamily = ""
	}
	if !validFontName(settings.EditorFont) {
		settings.EditorFont = ""
	}
	if !validFontSize(settings.FontSize) {
		settings.FontSize = 0
	}
	if !validFontSize(settings.EditorFontSize) {
		settings.EditorFontSize = 0
	}
	return settings
}

func (store *Store) SetInterface(settings InterfaceSettings, revision uint64) error {
	if normalizeInterface(settings) != settings {
		return errors.New("invalid interface settings")
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if revision <= store.interfaceRevision {
		return nil
	}
	updated := store.settings
	updated.Interface = settings
	if err := writeSettings(store.dir, updated); err != nil {
		return err
	}
	store.settings, store.interfaceRevision = updated, revision
	return nil
}

func normalizeWorkspaceSettings(settings Settings) Settings {
	settings.Interface = normalizeInterface(settings.Interface)
	if !settings.QueryDraftsEnabled {
		settings.QueryDrafts, settings.ActiveQueryDraft = nil, 0
		return settings
	}
	drafts := make([]QueryDraft, 0, min(len(settings.QueryDrafts), MaxQueryDrafts))
	active := 0
	for i, draft := range settings.QueryDrafts {
		if !validQueryDraft(draft) || len(drafts) == MaxQueryDrafts {
			continue
		}
		if i == settings.ActiveQueryDraft {
			active = len(drafts)
		}
		drafts = append(drafts, draft)
	}
	settings.QueryDrafts, settings.ActiveQueryDraft = drafts, active
	return settings
}
