package app

import (
	"context"
	"fmt"
	"regexp"
	"sync"
	"sync/atomic"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
	"github.com/magalab/dbl/internal/assistant"
	"github.com/magalab/dbl/internal/connection"
	"github.com/magalab/dbl/internal/credentials"
	"github.com/magalab/dbl/internal/preferences"
	"github.com/magalab/dbl/internal/sqlstream"
)

const maxDisplayedRows = 10000

const maxRetainedResultBytes int64 = sqlstream.DefaultWindowMaxBytes

const maxHistoryEntries = 100

const maxWorkspaces = 16

const sqliteObjectPageSize = 200

const aiDockBreakpoint = 1100

const queryTimeout = 5 * time.Minute

const exportTimeout = 10 * time.Minute

var riskySQLReadPattern = regexp.MustCompile(`(?i)\b(?:INTO|FOR\s+(?:NO\s+KEY\s+UPDATE|KEY\s+SHARE|UPDATE|SHARE)|LOCK\s+IN\s+SHARE\s+MODE)\b|:=|@|;`)

type queryRecord struct {
	ID       uint64
	SQL      string
	Started  time.Time
	Duration time.Duration
	Rows     int64
	Outcome  string
}

type resultRowKey struct {
	QueryID   uint64
	SourceRow int64
}

type aiPromptTemplate struct {
	Name   string
	Prompt string
}

type etcdWriteConfirmation struct {
	Key             string
	Value           string
	LeaseID         string
	ExistingLeaseID int64
}

type liveConnection struct {
	connection  connection.Connection
	profile     preferences.ConnectionProfile
	tree        connectionTreeSnapshot
	treeLoaded  bool
	treeLoading bool
}

type connectionTreeSnapshot struct {
	database       string
	objects        []connection.Object
	namespaces     []string
	namespace      string
	mongoDatabases []string
	redisPage      connection.KeyPage
}

func (confirmation etcdWriteConfirmation) matches(key, value, leaseID string, existingLeaseID int64) bool {
	return confirmation.Key == key && confirmation.Value == value && confirmation.LeaseID == leaseID && confirmation.ExistingLeaseID == existingLeaseID
}

type workbench struct {
	window                       *mygo.Window
	lifecycleCtx                 context.Context
	lifecycleCancel              context.CancelFunc
	workspaces                   []*queryWorkspace
	activeWorkspace              int
	nextWorkspaceID              uint64
	activeQuery                  *queryWorkspace
	store                        connection.SQL
	keyStore                     connection.KeyValue
	documentStore                connection.Documents
	etcdStore                    connection.Configuration
	etcdAdmin                    connection.EtcdAdmin
	etcdSection                  string
	etcdToolbarSection           string
	etcdSidebarSection           string
	etcdPendingSection           string
	etcdGlobalSearch             bool
	etcdKeysLoaded               bool
	etcdPaneWidth                float32
	etcdDashboard                connection.EtcdDashboard
	etcdPreviousMetrics          connection.EtcdMetrics
	etcdGRPCRequestRate          float64
	etcdGRPCFailureRate          float64
	etcdMetricsRateAvailable     bool
	etcdDashboardLoading         bool
	etcdDashboardLoaded          bool
	etcdDashboardError           string
	etcdAuthOverview             connection.EtcdAuthOverview
	etcdAuthLoading              bool
	etcdAuthLoaded               bool
	etcdAuthError                string
	etcdLeases                   []int64
	etcdLeasesLoaded             bool
	etcdLeasesLoading            bool
	etcdLeaseListError           string
	etcdSelectedLeaseID          int64
	etcdSelectedLeaseInfo        connection.EtcdLeaseInfo
	etcdLeaseDetailsLoading      bool
	etcdLeaseDetailsError        string
	etcdGrantLeaseOpen           bool
	etcdGrantLeaseTTL            string
	etcdAuthTab                  string
	etcdSelectedUser             string
	etcdUserDetails              connection.EtcdUser
	etcdSelectedRole             string
	etcdRoleDetails              connection.EtcdRole
	etcdCreateKeyOpen            bool
	etcdNewKey                   string
	etcdNewValue                 string
	etcdNewValueEditor           *editor.State
	etcdNewValueFormat           string
	etcdNewLeasePolicy           string
	etcdNewLeaseTTL              string
	etcdNewLease                 string
	etcdCreateUserOpen           bool
	etcdNewUsername              string
	etcdNewPassword              string
	etcdChangePassword           string
	etcdGrantRoleName            string
	etcdCreateRoleOpen           bool
	etcdNewRole                  string
	etcdPermissionKey            string
	etcdPermissionType           string
	etcdPermissionScope          string
	etcdGrantRoleOpen            bool
	etcdGrantPermissionOpen      bool
	etcdCancel                   context.CancelFunc
	etcdPrefix                   string
	etcdUsePrefix                bool
	etcdStartKey                 string
	etcdEntries                  []connection.EtcdEntry
	etcdSelectedKey              string
	etcdSelected                 connection.EtcdEntry
	etcdLeaseInfo                connection.EtcdLeaseInfo
	etcdLeaseInfoErr             string
	etcdLeaseInfoLoading         bool
	etcdValue                    string
	etcdLease                    string
	etcdDeleteCount              int64
	etcdDeleteReady              bool
	etcdDeletePrefix             bool
	etcdDeleteTargetKey          string
	etcdDeleteTargetPrefix       bool
	etcdWriteReady               bool
	etcdWriteConfirmation        etcdWriteConfirmation
	etcdWatchRevision            string
	etcdWatchEvents              []connection.EtcdWatchEvent
	etcdWatching                 bool
	mongoCancel                  context.CancelFunc
	mongoDatabases               []string
	mongoCollections             []string
	mongoIndexes                 []connection.DocumentIndex
	mongoIndexError              string
	mongoIndexesLoading          bool
	mongoSelectedDB              string
	mongoSelectedColl            string
	mongoDocuments               []connection.Document
	mongoNextOffset              uint64
	mongoHasMore                 bool
	mongoFilter                  string
	mongoProjection              string
	mongoPipeline                string
	mongoFilterEditor            *editor.State
	mongoProjectionEditor        *editor.State
	mongoPipelineEditor          *editor.State
	mongoUsePipeline             bool
	mongoSelectedID              string
	mongoOriginalJSON            string
	mongoEditor                  *editor.State
	mongoNewDocument             bool
	mongoDocumentTruncated       bool
	mongoShowDiff                bool
	mongoTreeVisible             bool
	mongoTreeExpanded            map[string]bool
	connectionManager            connection.Manager
	liveConnections              map[string]liveConnection
	treeConnectionLoading        map[string]bool
	treeConnectionErrors         map[string]string
	preferences                  *preferences.Store
	vault                        *credentials.Vault
	preferenceRevision           uint64
	queryHistoryEnabled          bool
	queryHistorySaving           bool
	queryHistorySaveRev          uint64
	nextQueryHistoryID           uint64
	appearance                   string
	recentSQLitePaths            []string
	recentList                   ui.ListState
	connectionProfiles           []preferences.ConnectionProfile
	selectedProfileID            string
	profileDriver                string
	profileName                  string
	profileGroup                 string
	profileEnvironment           string
	profileHost                  string
	profilePort                  string
	profileTLSMode               string
	profileDatabase              string
	profileRedisDB               string
	profileRedisAliases          map[int]string
	profileAuthSource            string
	profileTLSCAPath             string
	profileTLSCertPath           string
	profileTLSKeyPath            string
	profileSecretVisible         bool
	aiModelCache                 map[string][]string
	aiModelsError                string
	aiModelsLoading              bool
	aiProviders                  []preferences.AIProvider
	aiProviderID                 string
	aiDefaultProviderID          string
	aiProviderName               string
	aiProviderNew                bool
	aiProviderNameEditing        bool
	aiOptionsExpanded            bool
	aiSettings                   preferences.AISettings
	aiEnabled                    bool
	aiEndpoint                   string
	aiModel                      string
	aiAPIKey                     string
	aiRemoveKey                  bool
	aiPrompt                     string
	aiPromptTemplate             string
	aiMode                       string
	aiResponseMode               string
	aiIncludeSQL                 bool
	aiIncludeError               bool
	aiIncludeSchema              bool
	aiIncludeMongoQuery          bool
	aiIncludeRedisMeta           bool
	aiIncludeEtcdMeta            bool
	aiRequestError               string
	aiResponse                   string
	aiPreview                    string
	aiPreparedMessages           []assistant.Message
	aiPreparedSettings           preferences.AISettings
	aiPrepared                   bool
	aiCancel                     context.CancelFunc
	aiRunning                    bool
	showAI                       bool
	aiFullPage                   bool
	queryWorkspaceRequested      bool
	aiDraftPending               bool
	aiDraftApproved              bool
	aiDraftStatement             string
	redisMatch                   string
	redisGroupByPrefix           bool
	redisGroupDelimiter          string
	redisGroupExpanded           map[string]bool
	redisKeyTTLDeadlines         map[string]time.Time
	redisTTLNow                  time.Time
	redisTTLRefreshTimer         *time.Timer
	redisTTLRefreshGeneration    uint64
	redisKeyListWidth            float32
	redisCursor                  uint64
	redisDeleteConfirmKey        string
	redisDeleteConfirmGeneration uint64
	redisDeleteConfirmTimer      *time.Timer
	redisHasMore                 bool
	redisKeys                    []string
	redisKeySelection            ui.Selection[string]
	redisKeyMetadata             map[string]connection.KeyInfo
	redisEntryList               ui.ListState
	redisSelectedKey             string
	redisInfo                    connection.KeyInfo
	redisPage                    connection.KeyValuePage
	redisValueCursor             string
	redisValueDraft              string
	redisEntryField              string
	redisEntryScore              string
	redisEntryFilter             string
	redisEntryEditorOpen         bool
	showRedisKeyEditor           bool
	redisNewKey                  string
	redisNewType                 string
	redisNewTypeChoice           string
	redisNewTTLMode              string
	redisNewTTLSeconds           string
	redisNewValue                string
	redisNewStreamID             string
	redisNewEntries              []redisEntryDraft
	redisJSONSupportKnown        bool
	redisJSONSupported           bool
	redisJSONChecking            bool
	redisJSONSupportError        string
	redisJSONSupportConnectionID string
	profileUsername              string
	profileSecret                string
	profileCredential            string
	removeCredential             bool
	profileReadOnly              bool
	profileFavorite              bool
	connectionSearch             string
	activeConnectionsOnly        bool
	connectionTreeExpanded       map[string]*bool
	showConnectionEditor         bool
	connectionEditorOffset       ui.Point
	showRedisAliasEditor         bool
	redisAliasDB                 int
	redisAliasDraft              string
	settingsWindow               *mygo.Window
	settingsTab                  string
	objects                      []connection.Object
	sqlCompletionColumnCache     map[string]sqlCompletionColumnCacheEntry
	sqlCompletionColumnLoading   map[string]bool
	sqlCompletionPending         map[uint64]sqlCompletionPendingRequest
	sqlNamespaces                []string
	sqlNamespace                 string
	sqlNamespaceChoice           string
	openedSQLProfile             preferences.ConnectionProfile
	hasOpenedSQLProfile          bool
	objectCursorSchema           string
	objectCursorType             string
	objectCursorName             string
	objectsHasMore               bool
	objectList                   ui.ListState
	selectedObject               string
	selectedType                 string
	selectedSQL                  string
	objectColumns                []connection.Column
	objectColumnsLoading         bool
	objectColumnsError           string
	path                         string
	openedPath                   string
	openedDriver                 connection.Driver
	openedConnectionID           string
	openedLabel                  string
	readOnlyMode                 bool
	openedReadOnly               bool
	openedEnvironment            string
	sqlConfirmationPending       string
	sqlConfirmationApproved      string
	cursor                       *sqlstream.Cursor
	queryContext                 context.Context
	resultCountWorkspace         *queryWorkspace
	cancelResultCount            context.CancelFunc
	cancelResultCountServer      connection.QueryCancel
	resultCountCancelRequested   bool
	resultCountServerCancelReady bool
	resultCountServerCancelSent  bool
	resultCountCancelError       string
	resultCountCancelSignal      *atomic.Bool
	resultCountServerCancelDone  chan struct{}
	resultCountServerCancelMu    sync.Mutex
	resultCountServerCancelTask  connection.QueryCancel
	resultCountGeneration        uint64
	exporting                    bool
	cancelExport                 context.CancelFunc
	leftWidth                    float32
	aiPanelWidth                 float32
	workspaceTabsScroll          ui.ScrollState
	workspaceTabsLastActiveID    uint64
	resultHeight                 float32
	queryEditorHeight            float32
	leftCollapsed                bool
	rightCollapsed               bool
	language                     string
	observedLayout               preferences.Layout
	savedLayout                  preferences.Layout
	layoutSaveTimer              *time.Timer
	layoutSaving                 bool
	dialogOpen                   bool
	confirmationOpen             bool
	confirmation                 *appConfirmation
	busy                         bool
	cancelQuery                  context.CancelFunc
	cancelRedis                  context.CancelFunc
	errorSummary                 string
	errorDetails                 string
	showErrorDetails             bool
	announcedStatus              string
	status                       string
}

type queryWorkspace struct {
	id                         uint64
	title                      string
	implicitQuery              bool
	queryConnectionID          string
	queryNamespace             string
	connectionView             bool
	connectionDriver           connection.Driver
	connectionID               string
	redisView                  bool
	redisConnectionID          string
	redisStateLoaded           bool
	redisMatch                 string
	redisGroupByPrefix         bool
	redisGroupDelimiter        string
	redisGroupExpanded         map[string]bool
	redisKeyTTLDeadlines       map[string]time.Time
	redisKeys                  []string
	redisCursor                uint64
	redisHasMore               bool
	redisSelectedKey           string
	redisKeySelection          ui.Selection[string]
	redisKeyMetadata           map[string]connection.KeyInfo
	redisEntryList             ui.ListState
	redisInfo                  connection.KeyInfo
	redisPage                  connection.KeyValuePage
	redisValueCursor           string
	redisValueDraft            string
	redisEntryField            string
	redisEntryScore            string
	redisEntryFilter           string
	dataView                   bool
	schemaView                 bool
	schemaObject               connection.Object
	schemaConnectionID         string
	schemaLoading              bool
	schemaLoaded               bool
	schemaLoadError            string
	schemaError                string
	schemaBaseline             []connection.Column
	schemaColumns              []schemaColumnDraft
	schemaPreviewTab           bool
	schemaApplyPending         bool
	schemaCloseAfterApply      bool
	dataObject                 connection.Object
	dataConnectionID           string
	pendingDataRefresh         bool
	query                      *editor.State
	sqlCompletionBound         bool
	sqlCompletionInputRevision uint64
	resultSQL                  string
	resultPath                 string
	resultConnectionID         string
	resultTotal                int64
	resultTotalKnown           bool
	resultTotalCounted         bool
	resultCountCancelled       bool
	columns                    []sqlstream.Column
	resultWindow               sqlstream.Window
	streamStopped              bool
	history                    []queryRecord
	activeHistoryID            uint64
	showHistory                bool
	resultList                 ui.ListState
	resultSort                 ui.SortOrder
	resultOrder                []int
	resultOrderDirty           bool
	lastSortColumn             string
	lastSortDesc               bool
	historyList                ui.ListState
	dataColumns                []connection.Column
	dataResultColumns          []sqlstream.Column
	dataColumnsLoading         bool
	dataColumnsError           string
	dataColumnsFor             string
	dataChanges                []dataRowChange
	dataCellEditing            bool
	dataEditSourceRow          int64
	dataEditColumn             int
	dataEditDraft              string
	dataEditNull               bool
	dataEditOriginal           []any
	dataInsertEditing          bool
	dataInsertDraft            map[int]string
	dataInsertIncluded         map[int]bool
	dataInsertNull             map[int]bool
	dataSelectedRow            int
	dataPage                   int
	resultPageSize             int
	resultPage                 int
	pageJumpInput              string
}

type appConfirmation struct {
	title    string
	message  string
	detail   string
	buttons  []string
	primary  int
	onChoose func(int)
}

type schemaColumnDraft struct {
	OriginalName string
	Name         string
	Type         string
	NotNull      bool
	Default      string
	DefaultSet   bool
	PrimaryKey   bool
	Hidden       bool
	IsNew        bool
	Dropped      bool
}

type dataRowChange struct {
	kind             connection.TableChangeKind
	sourceRow        int64
	original         []any
	values           map[string]any
	deletedRowValues map[string]any
}

type redisEntryDraft struct {
	Field string
	Value string
	Score string
}

func newWorkbench() *workbench {
	workspace := newQueryWorkspace(1)
	lifecycleCtx, lifecycleCancel := context.WithCancel(context.Background())
	app := &workbench{
		lifecycleCtx:        lifecycleCtx,
		lifecycleCancel:     lifecycleCancel,
		workspaces:          []*queryWorkspace{workspace},
		activeQuery:         workspace,
		activeWorkspace:     0,
		nextWorkspaceID:     2,
		leftWidth:           220,
		aiPanelWidth:        380,
		redisKeyListWidth:   320,
		etcdPaneWidth:       320,
		redisGroupByPrefix:  true,
		redisGroupDelimiter: ":",
		resultHeight:        250,
		queryEditorHeight:   260,
		rightCollapsed:      true,
		settingsTab:         "general",
		status:              "就绪 · 纯 Go SQLite 工作区",
		profileDriver:       preferences.DriverSQLite,
	}
	workspace.implicitQuery = true
	prefs, err := preferences.Open()
	if err != nil {
		app.reportError("加载本地偏好设置失败", err)
		return app
	}
	app.preferences = prefs
	vault, err := credentials.NewMigratingLocal(prefs.CredentialDirectory())
	if err != nil {
		app.reportError("初始化本机凭据存储失败", err)
		return app
	}
	app.vault = vault
	settings := prefs.Settings()
	app.aiProviders = settings.AIProviders
	app.aiProviderID = settings.ActiveAIProviderID
	app.aiDefaultProviderID = settings.ActiveAIProviderID
	for _, provider := range app.aiProviders {
		if provider.ID == app.aiProviderID {
			app.aiProviderName = provider.Name
		}
	}
	app.aiSettings = settings.AI
	app.aiEnabled = settings.AI.Enabled
	app.aiEndpoint, app.aiModel = settings.AI.Endpoint, settings.AI.Model
	app.aiMode = "生成 SQL"
	app.aiPromptTemplate = promptTemplatesForMode(app.aiMode)[0].Name
	app.appearance = settings.Appearance
	if app.appearance == "" {
		app.appearance = "system"
	}
	app.language = settings.Language
	if app.language == "" {
		app.language = "zh"
	}
	app.status = app.localized("就绪 · 纯 Go SQLite 工作区")
	app.path = existingSQLitePath(settings.LastSQLitePath)
	app.recentSQLitePaths = settings.RecentSQLitePaths
	app.connectionProfiles = settings.ConnectionProfiles
	app.queryHistoryEnabled = settings.QueryHistoryEnabled
	for _, entry := range settings.QueryHistory {
		app.workspaces[0].history = append(app.workspaces[0].history, queryRecord{
			ID: entry.ID, SQL: entry.SQL, Started: entry.Started, Duration: entry.Duration, Rows: entry.Rows, Outcome: entry.Outcome,
		})
		if entry.ID > app.nextQueryHistoryID {
			app.nextQueryHistoryID = entry.ID
		}
	}
	if settings.Layout != nil {
		if settings.Layout.LeftWidth > 0 {
			app.leftWidth = settings.Layout.LeftWidth
		}
		if settings.Layout.InspectorWidth > 0 {
			app.aiPanelWidth = max(settings.Layout.InspectorWidth, float32(320))
		}
		if settings.Layout.ResultHeight > 0 {
			app.resultHeight = settings.Layout.ResultHeight
		}
		if settings.Layout.QueryEditorHeight > 0 {
			app.queryEditorHeight = settings.Layout.QueryEditorHeight
		}
		app.leftCollapsed = settings.Layout.LeftCollapsed
		app.rightCollapsed = settings.Layout.RightCollapsed
	}
	app.observedLayout = app.currentLayout()
	app.savedLayout = app.observedLayout
	return app
}

func (app *workbench) hasActiveConnection() bool {
	return app.store != nil || app.keyStore != nil || app.documentStore != nil || app.etcdStore != nil
}

func (app *workbench) implicitQueryWorkspace() (int, *queryWorkspace) {
	for index, workspace := range app.workspaces {
		if workspace.implicitQuery {
			return index, workspace
		}
	}
	return -1, nil
}

func (app *workbench) workspaceCount() int {
	count := len(app.workspaces)
	if _, workspace := app.implicitQueryWorkspace(); workspace != nil {
		count--
	}
	return count
}

func (app *workbench) taskParentContext() context.Context {
	if app.lifecycleCtx != nil {
		return app.lifecycleCtx
	}
	return context.Background()
}

func (app *workbench) withTaskTimeout(timeout time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(app.taskParentContext(), timeout)
}

func (app *workbench) withTaskCancel() (context.Context, context.CancelFunc) {
	return context.WithCancel(app.taskParentContext())
}

func newQueryWorkspace(id uint64) *queryWorkspace {
	buffer := editor.NewBuffer("SELECT sqlite_version() AS sqlite_version;")
	buffer.SetLanguage(editor.LanguageFor("query.sql"))
	return &queryWorkspace{
		id:              id,
		title:           fmt.Sprintf("查询 %d", id),
		query:           &editor.State{Buffer: buffer},
		resultWindow:    sqlstream.Window{MaxRows: maxDisplayedRows, MaxBytes: maxRetainedResultBytes},
		dataSelectedRow: -1,
		resultPageSize:  dataWorkspacePageSize,
		pageJumpInput:   "1",
	}
}
