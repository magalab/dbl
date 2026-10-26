// Package preferences persists non-secret application preferences.
package preferences

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/magalab/dbl/internal/credentials"
	"github.com/magalab/dbl/internal/privatefs"
)

const maxRecentSQLitePaths = 10
const maxRecentConnectionIDs = 10
const maxSQLiteProfiles = 100
const MaxQueryHistoryEntries = 100
const MaxQueryHistorySQLBytes = 64 << 10

type Settings struct {
	Version             int                 `json:"version"`
	Appearance          string              `json:"appearance,omitempty"`
	Layout              *Layout             `json:"layout,omitempty"`
	LastSQLitePath      string              `json:"last_sqlite_path,omitempty"`
	RecentSQLitePaths   []string            `json:"recent_sqlite_paths,omitempty"`
	RecentConnectionIDs []string            `json:"recent_connection_ids,omitempty"`
	SQLiteProfiles      []SQLiteProfile     `json:"sqlite_profiles,omitempty"`
	ConnectionProfiles  []ConnectionProfile `json:"connection_profiles,omitempty"`
	QueryHistoryEnabled bool                `json:"query_history_enabled,omitempty"`
	QueryHistory        []QueryHistoryEntry `json:"query_history,omitempty"`
	AI                  AISettings          `json:"ai,omitempty"`
}

// QueryHistoryEntry is an explicitly opted-in local record of a SQL execution.
type QueryHistoryEntry struct {
	ID       uint64        `json:"id"`
	SQL      string        `json:"sql"`
	Started  time.Time     `json:"started"`
	Duration time.Duration `json:"duration"`
	Rows     int64         `json:"rows"`
	Outcome  string        `json:"outcome"`
}

// AISettings stores provider location/model and an opaque credential reference.
type AISettings struct {
	Enabled   bool   `json:"enabled,omitempty"`
	Endpoint  string `json:"endpoint,omitempty"`
	Model     string `json:"model,omitempty"`
	APIKeyRef string `json:"api_key_ref,omitempty"`
}

// Layout contains non-sensitive splitter and result-panel dimensions.
type Layout struct {
	LeftWidth      float32 `json:"left_width"`
	InspectorWidth float32 `json:"inspector_width"`
	ResultHeight   float32 `json:"result_height"`
}

// SQLiteProfile contains only non-secret local connection metadata.
type SQLiteProfile struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Group       string `json:"group,omitempty"`
	Path        string `json:"path"`
	Environment string `json:"environment,omitempty"`
	ReadOnly    bool   `json:"read_only,omitempty"`
	Favorite    bool   `json:"favorite,omitempty"`
}

// Store serializes updates and persists only non-secret preferences.
type Store struct {
	mu                   sync.Mutex
	dir                  string
	settings             Settings
	revision             uint64
	queryHistoryRevision uint64
}

func Open() (*Store, error) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return nil, fmt.Errorf("find user config directory: %w", err)
	}
	return OpenAt(filepath.Join(configDir, "dbl"))
}

// OpenAt loads preferences from dir. It is exported to keep storage behavior
// testable without touching the user's real configuration directory.
func OpenAt(dir string) (*Store, error) {
	if strings.TrimSpace(dir) == "" {
		return nil, errors.New("preferences directory is empty")
	}
	store := &Store{dir: dir, settings: Settings{Version: 1}}
	dirInfo, err := os.Stat(dir)
	if errors.Is(err, os.ErrNotExist) {
		return store, nil
	}
	if err != nil {
		return nil, fmt.Errorf("inspect preferences directory: %w", err)
	}
	if !dirInfo.IsDir() {
		return nil, errors.New("preferences path must be a directory")
	}
	if err := privatefs.Restrict(dir, true); err != nil {
		return nil, fmt.Errorf("restrict preferences directory permissions: %w", err)
	}
	path := filepath.Join(dir, "settings.json")
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return store, nil
	}
	if err != nil {
		return nil, fmt.Errorf("inspect preferences file: %w", err)
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("preferences file must be a regular file")
	}
	if err := privatefs.Restrict(path, false); err != nil {
		return nil, fmt.Errorf("restrict preferences file permissions: %w", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read preferences file: %w", err)
	}
	var settings Settings
	if err := json.Unmarshal(data, &settings); err != nil {
		return nil, fmt.Errorf("parse preferences file: %w", err)
	}
	if settings.Version != 1 {
		return nil, fmt.Errorf("unsupported preferences version %d", settings.Version)
	}
	if settings.Appearance != "" && settings.Appearance != "system" && settings.Appearance != "light" && settings.Appearance != "dark" {
		return nil, fmt.Errorf("unsupported appearance setting %q", settings.Appearance)
	}
	if settings.Layout != nil {
		layout := sanitizeLayout(*settings.Layout)
		if layout == (Layout{}) {
			settings.Layout = nil
		} else {
			settings.Layout = &layout
		}
	}
	settings.RecentSQLitePaths = uniquePaths(settings.RecentSQLitePaths)
	if len(settings.RecentSQLitePaths) > maxRecentSQLitePaths {
		settings.RecentSQLitePaths = settings.RecentSQLitePaths[:maxRecentSQLitePaths]
	}
	if settings.LastSQLitePath == "" && len(settings.RecentSQLitePaths) > 0 {
		settings.LastSQLitePath = settings.RecentSQLitePaths[0]
	}
	settings.SQLiteProfiles = append([]SQLiteProfile(nil), settings.SQLiteProfiles...)
	if len(settings.SQLiteProfiles) > maxSQLiteProfiles {
		settings.SQLiteProfiles = settings.SQLiteProfiles[:maxSQLiteProfiles]
	}
	if settings.ConnectionProfiles == nil {
		settings.ConnectionProfiles = make([]ConnectionProfile, 0, len(settings.SQLiteProfiles))
		for _, profile := range settings.SQLiteProfiles {
			settings.ConnectionProfiles = append(settings.ConnectionProfiles, connectionProfile(profile))
		}
	} else {
		settings.ConnectionProfiles = append([]ConnectionProfile(nil), settings.ConnectionProfiles...)
		seen := make(map[string]struct{}, len(settings.ConnectionProfiles))
		for _, profile := range settings.ConnectionProfiles {
			seen[profile.ID] = struct{}{}
		}
		for _, legacy := range settings.SQLiteProfiles {
			if _, ok := seen[legacy.ID]; !ok {
				settings.ConnectionProfiles = append(settings.ConnectionProfiles, connectionProfile(legacy))
			}
		}
	}
	if len(settings.ConnectionProfiles) > maxSQLiteProfiles {
		settings.ConnectionProfiles = settings.ConnectionProfiles[:maxSQLiteProfiles]
	}
	for i, profile := range settings.ConnectionProfiles {
		normalized, err := normalizeConnectionProfile(profile)
		if err != nil {
			return nil, fmt.Errorf("invalid connection profile %q: %w", profile.Name, err)
		}
		settings.ConnectionProfiles[i] = normalized
	}
	settings.RecentConnectionIDs = validRecentConnectionIDs(settings.RecentConnectionIDs, settings.ConnectionProfiles)
	if !settings.QueryHistoryEnabled {
		settings.QueryHistory = nil
	} else {
		settings.QueryHistory = normalizeQueryHistory(settings.QueryHistory)
	}
	store.settings = settings
	return store, nil
}

// SetLayout persists valid window splitter dimensions.
func (store *Store) SetLayout(layout Layout, revision uint64) error {
	layout = sanitizeLayout(layout)
	if layout == (Layout{}) {
		return errors.New("layout dimensions are invalid")
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if revision <= store.revision {
		return errors.New("stale preferences update")
	}
	settings := store.settings
	settings.Version = 1
	settings.Layout = &layout
	if err := writeSettings(store.dir, settings); err != nil {
		return err
	}
	store.settings = settings
	store.revision = revision
	return nil
}

func sanitizeLayout(layout Layout) Layout {
	const maxDimension = 4096
	if invalidDimension(layout.LeftWidth, maxDimension) {
		layout.LeftWidth = 0
	}
	if invalidDimension(layout.InspectorWidth, maxDimension) {
		layout.InspectorWidth = 0
	}
	if invalidDimension(layout.ResultHeight, maxDimension) {
		layout.ResultHeight = 0
	}
	return layout
}

func invalidDimension(value float32, max float32) bool {
	return math.IsNaN(float64(value)) || math.IsInf(float64(value), 0) || value <= 0 || value > max
}

// SetAppearance persists one of the supported app appearance modes.
func (store *Store) SetAppearance(appearance string, revision uint64) error {
	if appearance != "system" && appearance != "light" && appearance != "dark" {
		return errors.New("appearance must be system, light, or dark")
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if revision <= store.revision {
		return errors.New("stale preferences update")
	}
	settings := store.settings
	settings.Version = 1
	settings.Appearance = appearance
	if err := writeSettings(store.dir, settings); err != nil {
		return err
	}
	store.settings = settings
	store.revision = revision
	return nil
}

func (store *Store) SaveAISettings(settings AISettings, revision uint64) error {
	settings.Endpoint = strings.TrimSpace(settings.Endpoint)
	settings.Model = strings.TrimSpace(settings.Model)
	settings.APIKeyRef = strings.TrimSpace(settings.APIKeyRef)
	var parsedEndpoint *url.URL
	if settings.Endpoint != "" {
		endpoint, err := url.Parse(settings.Endpoint)
		if err != nil || endpoint.Host == "" || (endpoint.Scheme != "https" && endpoint.Scheme != "http") || endpoint.User != nil || endpoint.RawQuery != "" || endpoint.Fragment != "" {
			return errors.New("AI endpoint must be an http(s) URL without credentials, query, or fragment")
		}
		if endpoint.Scheme != "https" && endpoint.Hostname() != "localhost" && endpoint.Hostname() != "127.0.0.1" && endpoint.Hostname() != "::1" {
			return errors.New("AI endpoint must use HTTPS except for localhost providers")
		}
		parsedEndpoint = endpoint
	}
	if settings.APIKeyRef != "" && !credentials.Reference(settings.APIKeyRef).Valid() {
		return errors.New("AI API key reference is invalid")
	}
	if settings.Enabled {
		if parsedEndpoint == nil || settings.Model == "" || !credentials.Reference(settings.APIKeyRef).Valid() {
			return errors.New("enabled AI requires a model and saved API key")
		}
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if revision <= store.revision {
		return errors.New("stale preferences update")
	}
	updated := store.settings
	updated.Version = 1
	updated.AI = settings
	if err := writeSettings(store.dir, updated); err != nil {
		return err
	}
	store.settings, store.revision = updated, revision
	return nil
}

// SetQueryHistory persists a bounded query history only when the user opts in.
// Disabling persistence removes all history from disk in the same atomic update.
func (store *Store) SetQueryHistory(enabled bool, entries []QueryHistoryEntry, revision uint64) error {
	if len(entries) > MaxQueryHistoryEntries {
		entries = entries[len(entries)-MaxQueryHistoryEntries:]
	}
	if !enabled {
		entries = nil
	}
	for _, entry := range entries {
		if entry.ID == 0 || strings.TrimSpace(entry.SQL) == "" || len(entry.SQL) > MaxQueryHistorySQLBytes || entry.Started.IsZero() || entry.Duration < 0 || entry.Rows < 0 || len(entry.Outcome) > 1024 {
			return errors.New("query history entry is invalid or exceeds its storage limit")
		}
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if revision <= store.queryHistoryRevision {
		return nil
	}
	settings := store.settings
	settings.Version = 1
	settings.QueryHistoryEnabled = enabled
	settings.QueryHistory = append([]QueryHistoryEntry(nil), entries...)
	if err := writeSettings(store.dir, settings); err != nil {
		return err
	}
	store.settings = settings
	if revision > store.revision {
		store.revision = revision
	}
	store.queryHistoryRevision = revision
	return nil
}

func (store *Store) Settings() Settings {
	store.mu.Lock()
	defer store.mu.Unlock()
	settings := store.settings
	if settings.Layout != nil {
		layout := *settings.Layout
		settings.Layout = &layout
	}
	settings.RecentSQLitePaths = append([]string(nil), settings.RecentSQLitePaths...)
	settings.RecentConnectionIDs = append([]string(nil), settings.RecentConnectionIDs...)
	settings.SQLiteProfiles = append([]SQLiteProfile(nil), settings.SQLiteProfiles...)
	settings.ConnectionProfiles = append([]ConnectionProfile(nil), settings.ConnectionProfiles...)
	settings.QueryHistory = append([]QueryHistoryEntry(nil), settings.QueryHistory...)
	return settings
}

func normalizeQueryHistory(entries []QueryHistoryEntry) []QueryHistoryEntry {
	valid := make([]QueryHistoryEntry, 0, min(len(entries), MaxQueryHistoryEntries))
	for _, entry := range entries {
		if entry.ID == 0 || strings.TrimSpace(entry.SQL) == "" || len(entry.SQL) > MaxQueryHistorySQLBytes || entry.Started.IsZero() || entry.Duration < 0 || entry.Rows < 0 || len(entry.Outcome) > 1024 {
			continue
		}
		valid = append(valid, entry)
	}
	if len(valid) > MaxQueryHistoryEntries {
		valid = valid[len(valid)-MaxQueryHistoryEntries:]
	}
	return valid
}

// SaveSQLiteProfile creates or updates a profile by ID.
func (store *Store) SaveSQLiteProfile(profile SQLiteProfile, revision uint64) (SQLiteProfile, error) {
	saved, err := store.SaveConnectionProfile(connectionProfile(profile), revision)
	if err != nil {
		return SQLiteProfile{}, err
	}
	return sqliteProfile(saved), nil
}

// SaveConnectionProfile creates or updates a non-secret connection profile.
func (store *Store) SaveConnectionProfile(profile ConnectionProfile, revision uint64) (ConnectionProfile, error) {
	var err error
	profile, err = normalizeConnectionProfile(profile)
	if err != nil {
		return ConnectionProfile{}, err
	}
	if profile.ID == "" {
		profile.ID, err = newID()
		if err != nil {
			return ConnectionProfile{}, fmt.Errorf("create connection profile ID: %w", err)
		}
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if revision <= store.revision {
		return ConnectionProfile{}, errors.New("stale preferences update")
	}
	profiles := append([]ConnectionProfile(nil), store.settings.ConnectionProfiles...)
	index := -1
	for i, existing := range profiles {
		if existing.ID == profile.ID {
			index = i
			continue
		}
		if existing.Driver == profile.Driver && strings.EqualFold(existing.Name, profile.Name) {
			return ConnectionProfile{}, fmt.Errorf("%s profile name %q is already used", profile.Driver, profile.Name)
		}
	}
	if index == -1 {
		if len(profiles) >= maxSQLiteProfiles {
			return ConnectionProfile{}, fmt.Errorf("at most %d connection profiles are supported", maxSQLiteProfiles)
		}
		profiles = append(profiles, profile)
	} else {
		profiles[index] = profile
	}
	settings := store.settings
	settings.ConnectionProfiles = profiles
	settings.SQLiteProfiles = legacySQLiteProfiles(profiles)
	settings.RecentConnectionIDs = validRecentConnectionIDs(settings.RecentConnectionIDs, profiles)
	if err := writeSettings(store.dir, settings); err != nil {
		return ConnectionProfile{}, err
	}
	store.settings = settings
	store.revision = revision
	return profile, nil
}

// CopyConnectionProfile duplicates metadata under a new ID and name.
func (store *Store) CopyConnectionProfile(id, name string, revision uint64) (ConnectionProfile, error) {
	settings := store.Settings()
	for _, profile := range settings.ConnectionProfiles {
		if profile.ID == strings.TrimSpace(id) {
			profile.ID = ""
			profile.Name = strings.TrimSpace(name)
			return store.SaveConnectionProfile(profile, revision)
		}
	}
	return ConnectionProfile{}, errors.New("connection profile not found")
}

func (store *Store) DeleteSQLiteProfile(id string, revision uint64) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return errors.New("SQLite profile ID is empty")
	}
	for _, profile := range store.Settings().ConnectionProfiles {
		if profile.ID == id && profile.Driver == DriverSQLite {
			return store.DeleteConnectionProfile(id, revision)
		}
	}
	return errors.New("SQLite profile not found")
}

// DeleteConnectionProfile removes a profile by ID.
func (store *Store) DeleteConnectionProfile(id string, revision uint64) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return errors.New("connection profile ID is empty")
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if revision <= store.revision {
		return errors.New("stale preferences update")
	}
	profiles := make([]ConnectionProfile, 0, len(store.settings.ConnectionProfiles))
	found := false
	for _, profile := range store.settings.ConnectionProfiles {
		if profile.ID == id {
			found = true
			continue
		}
		profiles = append(profiles, profile)
	}
	if !found {
		return errors.New("connection profile not found")
	}
	settings := store.settings
	settings.ConnectionProfiles = profiles
	settings.SQLiteProfiles = legacySQLiteProfiles(profiles)
	settings.RecentConnectionIDs = validRecentConnectionIDs(settings.RecentConnectionIDs, profiles)
	if err := writeSettings(store.dir, settings); err != nil {
		return err
	}
	store.settings = settings
	store.revision = revision
	return nil
}

// RememberConnectionProfile records a successfully opened connection without
// persisting connection secrets. Older asynchronous writes are ignored.
func (store *Store) RememberConnectionProfile(id string, revision uint64) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return errors.New("connection profile ID is empty")
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if revision <= store.revision {
		return nil
	}
	found := false
	for _, profile := range store.settings.ConnectionProfiles {
		if profile.ID == id {
			found = true
			break
		}
	}
	if !found {
		return errors.New("connection profile not found")
	}
	settings := store.settings
	settings.Version = 1
	settings.RecentConnectionIDs = append([]string{id}, settings.RecentConnectionIDs...)
	settings.RecentConnectionIDs = uniqueIDs(settings.RecentConnectionIDs)
	if len(settings.RecentConnectionIDs) > maxRecentConnectionIDs {
		settings.RecentConnectionIDs = settings.RecentConnectionIDs[:maxRecentConnectionIDs]
	}
	if err := writeSettings(store.dir, settings); err != nil {
		return err
	}
	store.settings = settings
	store.revision = revision
	return nil
}

func validRecentConnectionIDs(ids []string, profiles []ConnectionProfile) []string {
	known := make(map[string]struct{}, len(profiles))
	for _, profile := range profiles {
		known[profile.ID] = struct{}{}
	}
	filtered := make([]string, 0, len(ids))
	for _, id := range ids {
		if _, ok := known[strings.TrimSpace(id)]; ok {
			filtered = append(filtered, id)
		}
	}
	filtered = uniqueIDs(filtered)
	if len(filtered) > maxRecentConnectionIDs {
		filtered = filtered[:maxRecentConnectionIDs]
	}
	return filtered
}

func uniqueIDs(ids []string) []string {
	seen := make(map[string]struct{}, len(ids))
	unique := make([]string, 0, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		unique = append(unique, id)
	}
	return unique
}

// RememberSQLitePath records a successfully opened path. revision is assigned
// by the caller so an older asynchronous disk write cannot replace a newer
// selection if goroutines reach this method out of order.
func (store *Store) RememberSQLitePath(path string, revision uint64) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return errors.New("SQLite path is empty")
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if revision <= store.revision {
		return nil
	}
	settings := store.settings
	settings.Version = 1
	settings.LastSQLitePath = path
	settings.RecentSQLitePaths = append([]string{path}, store.settings.RecentSQLitePaths...)
	settings.RecentSQLitePaths = uniquePaths(settings.RecentSQLitePaths)
	settings.SQLiteProfiles = append([]SQLiteProfile(nil), store.settings.SQLiteProfiles...)
	if len(settings.RecentSQLitePaths) > maxRecentSQLitePaths {
		settings.RecentSQLitePaths = settings.RecentSQLitePaths[:maxRecentSQLitePaths]
	}
	if err := writeSettings(store.dir, settings); err != nil {
		return err
	}
	store.settings = settings
	store.revision = revision
	return nil
}

func uniquePaths(paths []string) []string {
	seen := make(map[string]struct{}, len(paths))
	unique := make([]string, 0, len(paths))
	for _, path := range paths {
		path = strings.TrimSpace(path)
		if path == "" {
			continue
		}
		if _, exists := seen[path]; exists {
			continue
		}
		seen[path] = struct{}{}
		unique = append(unique, path)
	}
	return unique
}

func newID() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw[:]), nil
}

func writeSettings(dir string, settings Settings) error {
	return writeSettingsWithCheckpoint(dir, settings, nil)
}

// writeSettingsWithCheckpoint allows subprocess tests to terminate at durable
// write boundaries without changing the production persistence path.
func writeSettingsWithCheckpoint(dir string, settings Settings, checkpoint func(string)) error {
	mark := func(stage string) {
		if checkpoint != nil {
			checkpoint(stage)
		}
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create preferences directory: %w", err)
	}
	if err := privatefs.Restrict(dir, true); err != nil {
		return fmt.Errorf("restrict preferences directory permissions: %w", err)
	}
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return fmt.Errorf("encode preferences: %w", err)
	}
	temp, err := os.CreateTemp(dir, ".settings-*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary preferences file: %w", err)
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	mark("created")
	if err := privatefs.Restrict(tempPath, false); err != nil {
		_ = temp.Close()
		return fmt.Errorf("restrict temporary preferences permissions: %w", err)
	}
	if _, err := temp.Write(data); err != nil {
		_ = temp.Close()
		return fmt.Errorf("write temporary preferences: %w", err)
	}
	mark("written")
	if err := temp.Sync(); err != nil {
		_ = temp.Close()
		return fmt.Errorf("sync temporary preferences: %w", err)
	}
	mark("synced")
	if err := temp.Close(); err != nil {
		return fmt.Errorf("close temporary preferences: %w", err)
	}
	mark("closed")
	if err := replaceSettingsFile(tempPath, filepath.Join(dir, "settings.json")); err != nil {
		return fmt.Errorf("replace preferences file: %w", err)
	}
	mark("replaced")
	return nil
}
