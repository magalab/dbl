package app

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/magalab/dbl/internal/assistant"
	"github.com/magalab/dbl/internal/connection"
	"github.com/magalab/dbl/internal/connection/mock"
	"github.com/magalab/dbl/internal/credentials"
	etcdadapter "github.com/magalab/dbl/internal/etcd"
	"github.com/magalab/dbl/internal/preferences"
	redisadapter "github.com/magalab/dbl/internal/redis"
	"github.com/magalab/dbl/internal/sqlite"
	"github.com/magalab/dbl/internal/sqlstream"
)

func newTestWorkbench() *workbench {
	first := newQueryWorkspace(1)
	return &workbench{
		workspaces:              []*queryWorkspace{first},
		activeQuery:             first,
		activeWorkspace:         0,
		nextWorkspaceID:         2,
		profileDriver:           "sqlite",
		queryWorkspaceRequested: true,
	}
}

func openTestConnectionEditor(t *testing.T, app *workbench) *ui.Tester {
	t.Helper()
	tester := ui.NewTester(app.view, 1440, 900)
	if err := tester.Click("新建连接…"); err != nil {
		t.Fatalf("open connection editor: %v", err)
	}
	tester.Frame()
	return tester
}

func newAIRequestWorkbench() *workbench {
	app := newTestWorkbench()
	settings := preferences.AISettings{
		Enabled: true, Endpoint: "https://provider.example/v1/chat/completions", Model: "test-model",
		APIKeyRef: "v1:0123456789abcdef0123456789abcdef",
	}
	app.aiSettings, app.aiEnabled = settings, true
	app.aiEndpoint, app.aiModel = settings.Endpoint, settings.Model
	app.aiPrompt = "Explain the selected query"
	return app
}

type cleanupTestBackend struct {
	values  map[string][]byte
	failed  map[string]bool
	deleted []string
}

func (backend *cleanupTestBackend) Set(account string, secret []byte) error {
	backend.values[account] = append([]byte(nil), secret...)
	return nil
}

func (backend *cleanupTestBackend) Get(account string) ([]byte, error) {
	secret, ok := backend.values[account]
	if !ok {
		return nil, credentials.ErrNotFound
	}
	return append([]byte(nil), secret...), nil
}

func (backend *cleanupTestBackend) Delete(account string) error {
	backend.deleted = append(backend.deleted, account)
	if backend.failed[account] {
		return errors.New("simulated credential backend failure")
	}
	delete(backend.values, account)
	return nil
}

func TestDeleteUnusedCredentialsAttemptsEveryUniqueUnreferencedItem(t *testing.T) {
	backend := &cleanupTestBackend{values: make(map[string][]byte), failed: make(map[string]bool)}
	vault := credentials.New(backend)
	first, err := vault.Store([]byte("old password"))
	if err != nil {
		t.Fatal(err)
	}
	second, err := vault.Store([]byte("old client key"))
	if err != nil {
		t.Fatal(err)
	}
	shared, err := vault.Store([]byte("shared credential"))
	if err != nil {
		t.Fatal(err)
	}
	backend.failed[string(first)] = true
	profiles := []preferences.ConnectionProfile{{ID: "other", Driver: preferences.DriverEtcd, TLSKeyRef: string(shared)}}
	err = deleteUnusedCredentials(vault, []credentials.Reference{first, second, shared, first}, profiles)
	if err == nil {
		t.Fatal("expected a cleanup error for the failed delete")
	}
	if len(backend.deleted) != 2 || backend.deleted[0] != string(first) || backend.deleted[1] != string(second) {
		t.Fatalf("delete attempts = %#v, want failed and succeeding unique refs", backend.deleted)
	}
	if _, ok := backend.values[string(second)]; ok {
		t.Fatal("successful later cleanup was skipped after the first failure")
	}
	if _, ok := backend.values[string(shared)]; !ok {
		t.Fatal("credential still referenced by another profile was deleted")
	}
}

func TestAIRequestDefaultsToPromptOnlyAndRequiresReview(t *testing.T) {
	app := newAIRequestWorkbench()
	app.activeQuery.query.Buffer.Set("SELECT 'sensitive row value'")
	app.errorSummary = "password=sensitive-error-detail"
	app.selectedObject = "private_schema.customer_records"
	app.objectColumns = []connection.Column{{Name: "customer_email", Type: "text"}}
	app.askAI()

	if !app.aiPrepared || app.aiRunning || app.aiCancel != nil {
		t.Fatal("AI request was sent without the review step")
	}
	if len(app.aiPreparedMessages) != 2 || app.aiPreparedMessages[1].Content != app.aiPrompt {
		t.Fatalf("default AI user message = %#v", app.aiPreparedMessages)
	}
	for _, secret := range []string{"sensitive row value", "sensitive-error-detail", "private_schema", "customer_email"} {
		if strings.Contains(app.aiPreview, secret) {
			t.Errorf("default AI preview leaked %q", secret)
		}
	}
}

func TestAIRequestIncludesSQLOnlyForOneExplicitlyConsentedRequest(t *testing.T) {
	app := newAIRequestWorkbench()
	app.activeQuery.query.Buffer.Set("SELECT 'explicitly selected SQL'")
	app.aiIncludeSQL = true
	app.askAI()
	if !app.aiPrepared || !strings.Contains(app.aiPreparedMessages[1].Content, "explicitly selected SQL") {
		t.Fatalf("explicitly selected SQL missing from review: %#v", app.aiPreparedMessages)
	}
	if app.aiIncludeSQL {
		t.Fatal("SQL consent was not reset after preparing the request")
	}
	if app.aiRunning || app.aiCancel != nil {
		t.Fatal("preparing an AI request started network activity")
	}
}

func TestRemovingSavedAIKeyAlsoDisablesAI(t *testing.T) {
	settings := aiSettingsForSave(true, " https://provider.example/v1/chat/completions ", " test-model ", " v1:0123456789abcdef0123456789abcdef ", true)
	if settings.Enabled {
		t.Fatal("AI remained enabled after removing its required API key")
	}
	if settings.APIKeyRef != "" {
		t.Fatalf("API key reference remained after removal: %q", settings.APIKeyRef)
	}
	if settings.Endpoint != "https://provider.example/v1/chat/completions" || settings.Model != "test-model" {
		t.Fatalf("non-secret provider settings were not preserved: %#v", settings)
	}
}

func TestEtcdWatchUIIndicatesTruncatedValues(t *testing.T) {
	app := newTestWorkbench()
	app.etcdStore = &etcdadapter.Store{}
	app.etcdSelectedKey = "/sample/key"
	app.etcdSelected = connection.EtcdEntry{Key: "/sample/key", Value: "preview", ValueTruncated: true}
	app.etcdWatchEvents = []connection.EtcdWatchEvent{{
		Type: "PUT", Key: "/sample/key", Value: "preview", Revision: 42, ValueTruncated: true,
	}}
	tester := ui.NewTester(app.view, 1200, 900)
	if !tester.HasText("值已截断，超过 1 MiB") {
		t.Fatal("etcd watch UI did not explain that the event value was truncated")
	}
}

func TestEtcdProfileShowsTLSAndCredentialControls(t *testing.T) {
	app := newTestWorkbench()
	tester := openTestConnectionEditor(t, app)
	app.profileDriver = preferences.DriverEtcd
	app.profileHost = "https://127.0.0.1:2379"
	app.profileTLSMode = tlsModeLabel("verify-full")
	app.profileTLSCAPath = "/tmp/etcd-ca.pem"
	app.selectedProfileID = "etcd-1"
	app.connectionProfiles = []preferences.ConnectionProfile{{
		ID: "etcd-1", Name: "Config", Driver: preferences.DriverEtcd,
		TLSCertRef: "v1:0123456789abcdef0123456789abcdef",
		TLSKeyRef:  "v1:abcdef0123456789abcdef0123456789",
	}}

	tester.Frame()
	for _, label := range []string{
		"主机", "TLS 模式", "自定义 CA（可选，PEM 文件路径）", "本机凭据库已有客户端证书", "本机凭据库已有客户端私钥",
		"用户名", "密码（留空则保留已保存密码）", "测试连接", "保存并连接",
		"etcd 使用用户名/密码认证；endpoint scheme 决定 HTTP 或 HTTPS。HTTPS 使用系统根证书和可选自定义 CA，也可配置 vault 中的 mTLS 客户端证书/私钥。",
	} {
		if !tester.HasText(label) {
			t.Errorf("etcd profile UI is missing %q", label)
		}
	}
	if tester.HasText("v1:0123456789abcdef0123456789abcdef") || tester.HasText("v1:abcdef0123456789abcdef0123456789") {
		t.Fatal("etcd profile UI exposed an opaque credential reference")
	}
}

func TestEtcdWorkspaceShowsRevisionLeaseWatchAndDeleteSafety(t *testing.T) {
	app := newTestWorkbench()
	app.etcdStore = &etcdadapter.Store{}
	app.openedDriver = connection.DriverEtcd
	app.openedLabel = "Config · https://127.0.0.1:2379"
	app.etcdPrefix = "/services/"
	app.etcdUsePrefix = true
	app.etcdStartKey = "/services/next"
	app.etcdEntries = []connection.EtcdEntry{{Key: "/services/api"}}
	app.etcdSelectedKey = "/services/api"
	app.etcdSelected = connection.EtcdEntry{
		Key: "/services/api", Value: "https://api.internal", CreateRevision: 10,
		ModRevision: 42, Version: 3, Lease: 7,
	}
	app.etcdValue = "https://api.internal"
	app.etcdLeaseInfo = connection.EtcdLeaseInfo{TTL: 23, GrantedTTL: 60}
	app.etcdWriteReady = true
	app.etcdWriteConfirmation = etcdWriteConfirmation{
		Key: "/services/api", Value: "https://api.internal", ExistingLeaseID: 7,
	}
	app.etcdDeletePrefix = true
	app.etcdDeleteReady = true
	app.etcdDeleteTargetKey = "/services/"
	app.etcdDeleteTargetPrefix = true
	app.etcdDeleteCount = 4
	app.etcdWatchRevision = "42"
	app.etcdWatching = true
	tester := ui.NewTester(app.view, 1440, 900)
	for _, label := range []string{
		"搜索 etcd 键", "前缀匹配", "读取", "继续读取下一页", "etcd 键列表", "/services/api",
		"创建修订 10 · 修改修订 42 · 版本 3 · Lease ID 7", "租约剩余 TTL 23 秒 · 授予 TTL 60 秒",
		"值", "Lease ID（可选）", "写入目标：/services/api", "待写入值（20 字节）", "https://api.internal",
		"写入后将移除现有租约（Lease ID 7）", "确认写入",
		"按前缀删除", "将删除前缀 \"/services/\" 匹配的 4 个键。", "确认删除",
		"起始 revision", "停止 Watch",
	} {
		if !tester.HasText(label) {
			t.Errorf("etcd workspace is missing %q", label)
		}
	}
	if _, found := tester.Find("SQL 查询编辑器"); found {
		t.Fatal("etcd workspace displayed the SQL editor")
	}
}

func TestEtcdWorkspaceHidesWriteConfirmationAfterPayloadEdit(t *testing.T) {
	app := newTestWorkbench()
	app.etcdStore = &etcdadapter.Store{}
	app.openedDriver = connection.DriverEtcd
	app.etcdSelectedKey = "/services/api"
	app.etcdSelected = connection.EtcdEntry{Key: "/services/api"}
	app.etcdValue = "edited after preview"
	app.etcdWriteReady = true
	app.etcdWriteConfirmation = etcdWriteConfirmation{Key: "/services/api", Value: "reviewed value"}
	tester := ui.NewTester(app.view, 1440, 900)
	if !tester.HasText("待确认内容已变化，请重新预览写入。") {
		t.Fatal("edited etcd payload did not require a fresh preview")
	}
	if tester.HasText("确认写入") {
		t.Fatal("stale etcd write confirmation remained available after editing the payload")
	}
}

func TestEtcdNewKeyLeaseHintPointsToWorkspaceNavigation(t *testing.T) {
	app := newTestWorkbench()
	app.etcdStore = &etcdadapter.Store{}
	app.openedDriver = connection.DriverEtcd
	app.etcdCreateKeyOpen = true
	app.etcdNewLeasePolicy = "existing"
	tester := ui.NewTester(app.view, 1440, 900)
	if !tester.HasText("租约") {
		t.Fatal("etcd workspace does not show its top-level Leases tab")
	}
	if !tester.HasText("可在顶栏租约页面创建租约并查看 Lease ID。") {
		t.Fatal("new key dialog does not direct users to the Leases workspace tab")
	}
}

func TestEtcdDeleteTargetDefaultsToSelectedExactKey(t *testing.T) {
	tests := []struct {
		name       string
		selected   string
		browse     string
		prefixMode bool
		want       string
	}{
		{name: "selected key despite active browse prefix", selected: "/services/api", browse: "/services/", want: "/services/api"},
		{name: "explicit prefix deletion", selected: "/services/api", browse: "/services/", prefixMode: true, want: "/services/"},
		{name: "prefix deletion requires a nonempty prefix", selected: "/services/api", prefixMode: true, want: ""},
		{name: "no selection", browse: "/services/", want: ""},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			app := &workbench{
				etcdSelectedKey:  test.selected,
				etcdPrefix:       test.browse,
				etcdDeletePrefix: test.prefixMode,
			}
			if got := app.etcdDeleteTarget(); got != test.want {
				t.Fatalf("etcdDeleteTarget() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestEtcdWriteConfirmationBindsTargetValueAndLease(t *testing.T) {
	confirmation := etcdWriteConfirmation{Key: "/services/api", Value: "https://api.internal", LeaseID: "7", ExistingLeaseID: 5}
	if !confirmation.matches("/services/api", "https://api.internal", "7", 5) {
		t.Fatal("confirmation did not match the reviewed payload")
	}
	for _, edited := range []etcdWriteConfirmation{
		{Key: "/services/other", Value: "https://api.internal", LeaseID: "7", ExistingLeaseID: 5},
		{Key: "/services/api", Value: "https://other.internal", LeaseID: "7", ExistingLeaseID: 5},
		{Key: "/services/api", Value: "https://api.internal", LeaseID: "8", ExistingLeaseID: 5},
		{Key: "/services/api", Value: "https://api.internal", LeaseID: "7", ExistingLeaseID: 6},
	} {
		if confirmation.matches(edited.Key, edited.Value, edited.LeaseID, edited.ExistingLeaseID) {
			t.Errorf("edited payload %#v still matched the preview", edited)
		}
	}
}

func TestPreviewEtcdWriteCapturesExactPayload(t *testing.T) {
	app := &workbench{
		etcdStore:       &etcdadapter.Store{},
		etcdSelectedKey: "/services/api",
		etcdValue:       "https://api.internal",
		etcdLease:       " 7 ",
	}
	app.previewEtcdWrite()
	want := etcdWriteConfirmation{Key: "/services/api", Value: "https://api.internal", LeaseID: "7"}
	if !app.etcdWriteReady || app.etcdWriteConfirmation != want {
		t.Fatalf("write preview = ready:%v payload:%#v, want ready:true payload:%#v", app.etcdWriteReady, app.etcdWriteConfirmation, want)
	}
}

func TestPreviewEtcdWriteRejectsInvalidLeaseAndClearsOldApproval(t *testing.T) {
	app := &workbench{
		etcdStore:       &etcdadapter.Store{},
		etcdSelectedKey: "/services/api",
		etcdValue:       "https://api.internal",
		etcdLease:       "-1",
		etcdWriteReady:  true,
		etcdWriteConfirmation: etcdWriteConfirmation{
			Key: "/services/api", Value: "old value",
		},
	}
	app.previewEtcdWrite()
	if app.etcdWriteReady || app.etcdWriteConfirmation != (etcdWriteConfirmation{}) {
		t.Fatal("invalid lease retained an earlier write approval")
	}
	if app.status != "etcd 写入失败" {
		t.Fatalf("status = %q, want invalid lease error", app.status)
	}
}

func TestEtcdCommitRejectsPayloadChangedAfterPreview(t *testing.T) {
	app := &workbench{
		etcdStore:       &etcdadapter.Store{},
		etcdSelectedKey: "/services/api",
		etcdValue:       "https://edited.internal",
		etcdWriteReady:  true,
		etcdWriteConfirmation: etcdWriteConfirmation{
			Key: "/services/api", Value: "https://api.internal",
		},
	}
	app.commitEtcdValue()
	if app.busy {
		t.Fatal("changed payload started an etcd write")
	}
	if app.etcdWriteReady || app.etcdWriteConfirmation != (etcdWriteConfirmation{}) {
		t.Fatal("stale write confirmation was not cleared")
	}
	if app.status != "写入内容已更改，请重新预览并确认。" {
		t.Fatalf("status = %q, want a re-preview prompt", app.status)
	}
}

func TestParseEtcdLeaseID(t *testing.T) {
	tests := []struct {
		input string
		want  int64
		valid bool
	}{
		{input: "", want: 0, valid: true},
		{input: " 0 ", want: 0, valid: true},
		{input: "42", want: 42, valid: true},
		{input: "-1"},
		{input: "not-a-lease"},
	}
	for _, test := range tests {
		got, err := parseEtcdLeaseID(test.input)
		if (err == nil) != test.valid || (err == nil && got != test.want) {
			t.Errorf("parseEtcdLeaseID(%q) = %d, %v; want %d valid=%v", test.input, got, err, test.want, test.valid)
		}
	}
}

func TestSQLRiskSummaryRequiresReviewForAmbiguousReadStatements(t *testing.T) {
	tests := []struct {
		statement string
		wantRisk  bool
	}{
		{statement: "SELECT id, name FROM customers", wantRisk: false},
		{statement: "SELECT nextval('orders_id_seq')", wantRisk: true},
		{statement: "SELECT nextval/**/('orders_id_seq')", wantRisk: true},
		{statement: "SELECT value INTO OUTFILE '/tmp/export.csv' FROM records", wantRisk: true},
		{statement: "SELECT @counter := 1", wantRisk: true},
		{statement: "SELECT id FROM jobs FOR UPDATE", wantRisk: true},
		{statement: "SELECT id FROM jobs FOR NO KEY UPDATE", wantRisk: true},
		{statement: "SELECT id FROM jobs FOR KEY SHARE", wantRisk: true},
		{statement: "SELECT 1; DROP TABLE customers", wantRisk: true},
		{statement: "SELECT id FROM customers WHERE id IN (1, 2)", wantRisk: false},
		{statement: "SHOW TABLES; DROP TABLE customers", wantRisk: true},
	}
	for _, test := range tests {
		t.Run(test.statement, func(t *testing.T) {
			got := sqlRiskSummary(test.statement)
			if strings.HasPrefix(got, "读取类") == test.wantRisk {
				t.Fatalf("sqlRiskSummary(%q) = %q, want risk=%v", test.statement, got, test.wantRisk)
			}
		})
	}
}

func TestSQLWritesRequireConfirmationOnEveryWritableEnvironment(t *testing.T) {
	for _, test := range []struct {
		statement string
		readOnly  bool
		want      bool
	}{
		{statement: "UPDATE customers SET active = false", want: true},
		{statement: "SELECT id FROM customers", want: false},
		{statement: "SELECT nextval('orders_id_seq')", want: true},
		{statement: "UPDATE customers SET active = false", readOnly: true, want: false},
	} {
		if got := requiresSQLConfirmation(test.statement, test.readOnly); got != test.want {
			t.Errorf("requiresSQLConfirmation(%q, readOnly=%v) = %v, want %v", test.statement, test.readOnly, got, test.want)
		}
	}
}

func TestSQLFormatterActionUpdatesBufferAndReportsSuccess(t *testing.T) {
	app := newTestWorkbench()
	app.activeQuery.query.Buffer.Set("select id from users where active=true")

	app.formatSQL()

	if got, want := app.activeQuery.query.Buffer.String(), "SELECT id\nFROM users\nWHERE active = TRUE"; got != want {
		t.Fatalf("formatted editor buffer = %q, want %q", got, want)
	}
	if app.status != "SQL 已格式化。" {
		t.Fatalf("format status = %q", app.status)
	}
}

func TestSQLFormatterActionKeepsBufferWhenInputIsMalformed(t *testing.T) {
	app := newTestWorkbench()
	original := "select 'unfinished"
	app.activeQuery.query.Buffer.Set(original)

	app.formatSQL()

	if got := app.activeQuery.query.Buffer.String(); got != original {
		t.Fatalf("malformed SQL changed editor buffer to %q", got)
	}
	if app.errorSummary != "格式化 SQL 失败" || app.errorDetails == "" {
		t.Fatalf("format error was not exposed: summary=%q details=%q", app.errorSummary, app.errorDetails)
	}
}

func TestSQLWorkbenchShowsFormatterAction(t *testing.T) {
	tester := ui.NewTester(newTestWorkbench().view, 1200, 900)
	if !tester.HasText("格式化 SQL") {
		t.Fatal("SQL workspace does not expose the formatter action")
	}
}

func TestSQLWorkspaceShowsDatabaseSchemaSelector(t *testing.T) {
	store, err := sqlite.Open(context.Background(), t.TempDir()+"/selector.db", sqlite.CreateNew)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	app := newTestWorkbench()
	app.store = store
	app.openedDriver = connection.DriverPostgreSQL
	app.sqlNamespace = "public"
	app.sqlNamespaces = []string{"analytics", "public"}
	app.sqlNamespaceChoice = "public"
	tester := ui.NewTester(app.view, 1440, 900)
	if !tester.HasText("数据库 / Schema") {
		t.Fatal("SQL workspace does not expose the namespace selector")
	}
}

func TestSQLNamespaceSwitchTargetsChangeExecutionContext(t *testing.T) {
	profile := preferences.ConnectionProfile{Driver: preferences.DriverPostgreSQL, Database: "warehouse", CredentialRef: "opaque-ref"}
	postgresProfile, searchPath, err := namespaceSwitchTarget(connection.DriverPostgreSQL, profile, `analyst"schema`)
	if err != nil || postgresProfile.Database != "warehouse" || postgresProfile.CredentialRef != "opaque-ref" || searchPath != `analyst"schema` {
		t.Fatalf("PostgreSQL namespace target profile=%#v searchPath=%q err=%v", postgresProfile, searchPath, err)
	}
	mysqlProfile, mysqlSearchPath, err := namespaceSwitchTarget(connection.DriverMySQL, profile, "reporting")
	if err != nil || mysqlProfile.Database != "reporting" || mysqlSearchPath != "" || mysqlProfile.CredentialRef != "opaque-ref" {
		t.Fatalf("MySQL namespace target profile=%#v searchPath=%q err=%v", mysqlProfile, mysqlSearchPath, err)
	}
	if _, _, err := namespaceSwitchTarget(connection.DriverSQLite, profile, "attached"); err == nil {
		t.Fatal("SQLite accepted a namespace switch that the adapter cannot apply")
	}
}

func TestSQLNamespaceSelectorCanChooseFirstDatabaseWhenNoneIsActive(t *testing.T) {
	if namespaceSelectorDisabled("", []string{"analytics"}) {
		t.Fatal("selector disabled while there is one available database and none is active")
	}
	if !namespaceSelectorDisabled("main", []string{"main"}) {
		t.Fatal("selector remained enabled when main is the only available namespace")
	}
	if !namespaceSelectorDisabled("", nil) {
		t.Fatal("selector enabled without available namespaces")
	}
}

func TestAIPromptTemplatesCoverEveryTaskMode(t *testing.T) {
	for _, mode := range []string{"生成 SQL", "解释 SQL", "解释错误", "生成 Mongo Filter", "生成 Mongo Pipeline", "Redis 操作建议", "etcd 操作建议"} {
		templates := promptTemplatesForMode(mode)
		if len(templates) < 2 || templates[0].Name != "空白提示" || templates[1].Prompt == "" {
			t.Errorf("mode %q has incomplete templates: %#v", mode, templates)
		}
	}
}

func TestApplyAIPromptTemplateCanBeEditedAfterInsertion(t *testing.T) {
	app := newTestWorkbench()
	app.aiMode = "生成 SQL"
	app.aiPromptTemplate = promptTemplatesForMode(app.aiMode)[1].Name
	app.applyAIPromptTemplate()

	if app.aiPrompt == "" || app.status != "已将提示模板填入编辑区；可继续修改。" {
		t.Fatalf("template was not applied: prompt=%q status=%q", app.aiPrompt, app.status)
	}
	app.aiPrompt += " Include a date range."
	if !strings.Contains(app.aiPrompt, "date range") {
		t.Fatal("applied prompt was not left editable")
	}
}

func TestAIWorkspaceUsesMultilinePromptEditorAndTemplates(t *testing.T) {
	app := newTestWorkbench()
	app.showAI = true
	tester := ui.NewTester(app.view, 1440, 1200)
	for _, label := range []string{"提示模板", "应用提示模板", "提示"} {
		if !tester.HasText(label) {
			t.Errorf("AI workspace does not expose %q", label)
		}
	}
}

func TestAIRegenerationReusesReviewedPayloadAndRequiresReviewAgain(t *testing.T) {
	app := newAIRequestWorkbench()
	app.aiPrompt = "Original request"
	app.aiPreparedMessages = []assistant.Message{{Role: "system", Content: "system"}, {Role: "user", Content: "Original request plus explicitly selected context"}}
	app.aiPreparedSettings = app.aiSettings
	app.aiPreview = `{"messages":[{"role":"user","content":"Original request plus explicitly selected context"}]}`
	app.aiResponse = "previous answer"
	app.aiResponseMode = "生成 SQL"
	app.aiIncludeSQL = false
	messagesBefore := append([]assistant.Message(nil), app.aiPreparedMessages...)
	previewBefore := app.aiPreview

	app.prepareAIRegeneration()

	if !app.aiPrepared || app.aiRunning || app.aiCancel != nil {
		t.Fatal("regeneration did not stop at the request review step")
	}
	if len(app.aiPreparedMessages) != len(messagesBefore) || app.aiPreparedMessages[1] != messagesBefore[1] || app.aiPreview != previewBefore {
		t.Fatal("regeneration changed the previously reviewed messages or preview")
	}
	if app.aiResponse != "previous answer" {
		t.Fatal("previous response disappeared before the user confirmed regeneration")
	}
}

func TestAIRegenerationIsUnavailableAfterAIIsDisabled(t *testing.T) {
	app := newAIRequestWorkbench()
	app.aiPreparedMessages = []assistant.Message{{Role: "user", Content: "previous request"}}
	app.aiPreparedSettings = app.aiSettings
	app.aiEnabled = false

	app.prepareAIRegeneration()

	if app.aiPrepared {
		t.Fatal("regeneration was prepared after the user disabled AI")
	}
}

func TestAIWorkspaceShowsRegenerateActionForPriorResponse(t *testing.T) {
	app := newTestWorkbench()
	app.showAI = true
	app.aiResponse = "previous answer"
	app.aiPreparedMessages = []assistant.Message{{Role: "user", Content: "previous request"}}
	app.aiPreparedSettings = preferences.AISettings{Enabled: true}
	tester := ui.NewTester(app.view, 1440, 1200)
	if !tester.HasText("按上次请求重新生成") {
		t.Fatal("AI workspace does not expose regeneration for a previous response")
	}
}

func TestAboutPanelOptionsExposeVersionAndMyGoRuntime(t *testing.T) {
	packaged := aboutPanelOptions("0.1.0")
	if packaged.ApplicationName != "DBL · 数据工作台" || packaged.ApplicationVersion != "0.1.0" || !strings.Contains(packaged.Credits, "MyGo 0.6.2") {
		t.Fatalf("packaged about panel is missing app/runtime versions: %#v", packaged)
	}
	development := aboutPanelOptions("devel")
	if development.ApplicationVersion != "开发版" {
		t.Fatalf("development about panel version = %q, want 开发版", development.ApplicationVersion)
	}
}

func TestAboutMenuIsAvailableOnEachDesktopPlatform(t *testing.T) {
	findLabel := func(items []*mygo.MenuItem, label string) *mygo.MenuItem {
		for _, item := range items {
			if item.Label == label {
				return item
			}
		}
		return nil
	}
	findRole := func(items []*mygo.MenuItem, role mygo.MenuRole) *mygo.MenuItem {
		for _, item := range items {
			if item.Role == role {
				return item
			}
		}
		return nil
	}
	app := newTestWorkbench()

	macItems := appMenuItems(app, "darwin")
	macAppMenu := findRole(macItems, mygo.RoleAppMenu)
	if macAppMenu == nil || findLabel(macAppMenu.Submenu, "关于 DBL") == nil {
		t.Fatal("macOS application menu does not expose About DBL")
	}
	if findLabel(macItems, "帮助") != nil {
		t.Fatal("macOS should expose About DBL from the application menu only")
	}

	for _, goos := range []string{"linux", "windows"} {
		items := appMenuItems(app, goos)
		help := findLabel(items, "帮助")
		if help == nil || findLabel(help.Submenu, "关于 DBL") == nil {
			t.Errorf("%s menu does not expose Help → About DBL", goos)
		}
	}
}

func TestMenusExposeCommonKeyboardShortcuts(t *testing.T) {
	app := newTestWorkbench()
	findMenuItem := func(items []*mygo.MenuItem, label string) *mygo.MenuItem {
		for _, item := range items {
			if item.Label == label {
				return item
			}
			if found := findMenuItemInSubmenu(item.Submenu, label); found != nil {
				return found
			}
		}
		return nil
	}
	for _, goos := range []string{"darwin", "linux", "windows"} {
		items := appMenuItems(app, goos)
		for _, test := range []struct{ label, accelerator string }{
			{label: "新建连接…", accelerator: "CmdOrCtrl+N"},
			{label: "新建查询工作区", accelerator: "CmdOrCtrl+T"},
		} {
			item := findMenuItem(items, test.label)
			if item == nil || item.Accelerator != test.accelerator {
				t.Errorf("%s menu item %q = %#v, want accelerator %q", goos, test.label, item, test.accelerator)
			}
		}
		settings := findMenuItem(items, "设置…")
		if goos == "darwin" {
			if settings == nil || settings.Accelerator != "CmdOrCtrl+," {
				t.Errorf("macOS app menu settings = %#v, want CmdOrCtrl+,", settings)
			}
		} else if findMenuItem(items, "打开设置…") == nil || findMenuItem(items, "打开设置…").Accelerator != "CmdOrCtrl+," {
			t.Errorf("%s settings menu is missing CmdOrCtrl+,", goos)
		}
	}
}

func findMenuItemInSubmenu(items []*mygo.MenuItem, label string) *mygo.MenuItem {
	for _, item := range items {
		if item.Label == label {
			return item
		}
		if found := findMenuItemInSubmenu(item.Submenu, label); found != nil {
			return found
		}
	}
	return nil
}

func TestConnectionProfileEditorIsModal(t *testing.T) {
	app := newTestWorkbench()
	tester := ui.NewTester(app.view, 1280, 820)
	if _, found := tester.Find("新建连接…"); !found {
		t.Fatal("main toolbar does not offer new connection")
	}
	if tester.HasText("数据库类型") {
		t.Fatal("connection editor fields are still embedded in the sidebar")
	}
	if err := tester.Click("新建连接…"); err != nil {
		t.Fatal(err)
	}
	tester.Frame()
	if !tester.HasText("数据库类型") || !tester.HasText("保存连接配置") || !tester.HasText("关闭") {
		t.Fatal("connection editor modal is missing its form or actions")
	}
	app.profileSecret = "unsaved-secret"
	if err := tester.Click("关闭"); err != nil {
		t.Fatal(err)
	}
	tester.Frame()
	if app.showConnectionEditor || app.profileSecret != "" {
		t.Fatal("closing the connection editor did not clear its transient secret")
	}
	if err := tester.Click("新建连接…"); err != nil {
		t.Fatal(err)
	}
	tester.Frame()
	app.profileSecret = "escape-secret"
	tester.Key(0, ui.KeyEscape)
	tester.Frame()
	if app.showConnectionEditor || app.profileSecret != "" {
		t.Fatal("dismissing the connection editor with Escape did not clear its transient secret")
	}
	settingsTester := ui.NewTester(app.settingsView, 640, 620)
	settingsTester.Frame()
	if !settingsTester.HasText("查询历史") || !settingsTester.HasText("AI 助手") {
		t.Fatal("settings dialog is missing its preference sections")
	}
}

func TestConnectionProfileEditorFitsNarrowWindow(t *testing.T) {
	app := newTestWorkbench()
	app.showConnectionEditor = true
	tester := ui.NewTester(app.view, 640, 480)
	for _, label := range []string{"新建连接", "数据库类型", "SQLite 文件路径", "保存连接配置"} {
		if !tester.HasText(label) {
			t.Errorf("narrow connection editor is missing %q", label)
		}
	}
}

func TestReportErrorRedactsDatabaseServerValues(t *testing.T) {
	app := newTestWorkbench()
	app.reportError("查询失败", &pgconn.PgError{
		Code: "23505", Message: "duplicate key", Detail: "Key (email)=(private@example.test) already exists",
	})
	if app.errorSummary != "查询失败" || !strings.Contains(app.errorDetails, "SQLSTATE 23505") {
		t.Fatalf("database error summary/details = %q / %q", app.errorSummary, app.errorDetails)
	}
	if strings.Contains(app.errorDetails, "private@example.test") || strings.Contains(app.errorDetails, "duplicate key") {
		t.Fatalf("database error details leaked server message values: %q", app.errorDetails)
	}

	app.reportError("格式化失败", errors.New("unterminated SQL quote at byte 12"))
	if app.errorDetails != "unterminated SQL quote at byte 12" {
		t.Fatalf("application-generated diagnostic = %q", app.errorDetails)
	}
}

func TestQueryHistoryOutcomeDoesNotPersistDatabaseErrorDetails(t *testing.T) {
	for _, test := range []struct{ in, want string }{
		{"查询失败: duplicate value sensitive@example.test", "失败"},
		{"查询已取消。", "已取消"},
		{"查询超过 5 分钟，已停止。", "超时"},
		{"已读取下一批", "完成"},
	} {
		if got := safeHistoryOutcome(test.in); got != test.want {
			t.Errorf("safeHistoryOutcome(%q) = %q, want %q", test.in, got, test.want)
		}
	}
}

type testKeyValue struct{}

func (testKeyValue) Driver() connection.Driver                { return connection.DriverRedis }
func (testKeyValue) HealthCheck(context.Context) error        { return nil }
func (testKeyValue) ClassifyError(error) connection.ErrorKind { return connection.ErrorUnknown }
func (testKeyValue) Close() error                             { return nil }
func (testKeyValue) ScanKeys(context.Context, string, uint64, int) (connection.KeyPage, error) {
	return connection.KeyPage{}, nil
}
func (testKeyValue) ScanKeysByType(context.Context, string, string, uint64, int) (connection.KeyPage, error) {
	return connection.KeyPage{}, nil
}
func (testKeyValue) InspectKey(context.Context, string) (connection.KeyInfo, error) {
	return connection.KeyInfo{}, nil
}
func (testKeyValue) ReadKey(context.Context, string, string, int) (connection.KeyValuePage, error) {
	return connection.KeyValuePage{}, nil
}
func (testKeyValue) WriteKey(context.Context, connection.KeyMutation) error { return nil }
func (testKeyValue) DeleteKey(context.Context, string) error                { return nil }

type testDocuments struct{}

func (testDocuments) Driver() connection.Driver                   { return connection.DriverMongoDB }
func (testDocuments) HealthCheck(context.Context) error           { return nil }
func (testDocuments) ClassifyError(error) connection.ErrorKind    { return connection.ErrorUnknown }
func (testDocuments) Close() error                                { return nil }
func (testDocuments) Databases(context.Context) ([]string, error) { return []string{"catalog"}, nil }
func (testDocuments) Collections(context.Context, string) ([]string, error) {
	return []string{"items"}, nil
}
func (testDocuments) CollectionIndexes(context.Context, string, string) ([]connection.DocumentIndex, error) {
	return nil, nil
}
func (testDocuments) FindDocuments(context.Context, string, string, string, string, uint64, int) (connection.DocumentPage, error) {
	return connection.DocumentPage{}, nil
}
func (testDocuments) AggregateDocuments(context.Context, string, string, string, uint64, int) (connection.DocumentPage, error) {
	return connection.DocumentPage{}, nil
}
func (testDocuments) GetDocument(context.Context, string, string, string) (connection.Document, error) {
	return connection.Document{}, nil
}
func (testDocuments) ValidateDocument(string) error                                { return nil }
func (testDocuments) InsertDocument(context.Context, string, string, string) error { return nil }
func (testDocuments) ReplaceDocument(context.Context, string, string, string, string) error {
	return nil
}

func TestRemoteConnectionProfileControlsAreAccessibleAndHideCredentialReferences(t *testing.T) {
	app := newTestWorkbench()
	openTestConnectionEditor(t, app)
	app.profileDriver = "postgresql"
	app.profileTLSMode = tlsModeLabel("verify-full")
	app.profileCredential = "v1:0123456789abcdef0123456789abcdef"
	app.connectionProfiles = []preferences.ConnectionProfile{{
		ID: "profile-1", Name: "Analytics", Group: "Reporting", Driver: preferences.DriverPostgreSQL,
		Host: "db.example.test", Port: 5432, Database: "warehouse", Username: "reader",
		CredentialRef: "v1:0123456789abcdef0123456789abcdef",
	}}
	tester := ui.NewTester(app.view, 1440, 900)

	for _, name := range []string{"数据库类型", "主机", "端口", "数据库", "用户名", "密码（留空则保留已保存密码）", "连接名称", "分组"} {
		if _, found := tester.Find(name); !found {
			t.Errorf("accessible remote-profile control %q was not found", name)
		}
	}
	if !tester.HasText("PostgreSQL 使用系统根证书和可选自定义 CA 验证证书链及主机名") {
		t.Fatal("PostgreSQL TLS policy is not explained in the profile UI")
	}
	if !tester.HasText("本机加密凭据库中已有密码") {
		t.Fatal("saved credential state is not disclosed")
	}
	if _, testFound := tester.Find("测试连接"); !testFound {
		t.Fatal("PostgreSQL profile does not expose test and connect actions")
	}
	if _, connectFound := tester.Find("保存并连接"); !connectFound {
		t.Fatal("PostgreSQL profile does not expose test and connect actions")
	}
	if tester.HasText("v1:0123456789abcdef0123456789abcdef") {
		t.Fatal("opaque credential reference was exposed in the UI")
	}

	app.profileDriver = preferences.DriverSQLite
	tester.Frame()
	if _, found := tester.Find("主机"); found {
		t.Fatal("remote host input remained visible for a SQLite profile")
	}
}

func TestSQLiteProfileHasTestConnectionAction(t *testing.T) {
	app := newTestWorkbench()
	openTestConnectionEditor(t, app)
	app.profileDriver = preferences.DriverSQLite
	app.path = "/tmp/local.db"
	app.connectionProfiles = []preferences.ConnectionProfile{{ID: "sqlite-1", Name: "Local", Driver: preferences.DriverSQLite, Path: app.path}}
	tester := ui.NewTester(app.view, 1440, 900)
	if _, found := tester.Find("测试连接"); !found {
		t.Fatal("saved SQLite profile has no test-connection action")
	}
}

func TestQueryHistoryPanelExplainsLocalRetentionAndClearActions(t *testing.T) {
	app := newTestWorkbench()
	app.activeQuery.showHistory = true
	tester := ui.NewTester(app.view, 1440, 900)
	for _, label := range []string{"在本机保存查询历史", "SQL 可能包含敏感值", "清空此工作区历史", "清空所有工作区及已保存历史"} {
		if !tester.HasText(label) {
			t.Errorf("query history panel is missing %q", label)
		}
	}
}

func TestAIAssistantUsesRightDockOnWideScreens(t *testing.T) {
	app := newTestWorkbench()
	tester := ui.NewTester(app.view, 1440, 900)
	if tester.HasText("AI 任务") {
		t.Fatal("AI assistant dock should be closed by default")
	}
	app.showAI = true
	tester.Frame()
	if !tester.HasText("AI 任务") {
		t.Fatal("wide workspace did not show the AI assistant dock")
	}
	if !tester.HasText("SQL 查询编辑器") {
		t.Fatal("opening the AI dock replaced the main SQL workspace")
	}
}

func TestShutdownCancelsBackgroundTasksAndClosesActiveConnection(t *testing.T) {
	app := newTestWorkbench()
	app.lifecycleCtx, app.lifecycleCancel = context.WithCancel(context.Background())
	makeTask := func() (context.Context, context.CancelFunc) {
		return context.WithCancel(context.Background())
	}
	tasks := make([]context.Context, 0, 7)
	newCancel := func() context.CancelFunc {
		ctx, cancel := makeTask()
		tasks = append(tasks, ctx)
		return cancel
	}
	app.cancelQuery = newCancel()
	app.cancelExport = newCancel()
	app.cancelRedis = newCancel()
	app.mongoCancel = newCancel()
	app.etcdCancel = newCancel()
	app.aiCancel = newCancel()
	lifecycleTask, lifecycleTaskCancel := app.withTaskTimeout(time.Hour)
	defer lifecycleTaskCancel()
	tasks = append(tasks, lifecycleTask)
	active, err := (mock.Connector{}).Connect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	app.connectionManager.Replace(active)

	app.shutdown()
	for index, ctx := range tasks {
		if ctx.Err() != context.Canceled {
			t.Errorf("background task %d error = %v, want canceled", index, ctx.Err())
		}
	}
	if snapshot := app.connectionManager.Snapshot(); snapshot.State != connection.Disconnected {
		t.Fatalf("connection state after shutdown = %#v", snapshot)
	}
	if err := active.HealthCheck(context.Background()); err == nil {
		t.Fatal("active database connection remained open after shutdown")
	}
}

func TestMySQLProfileCanTestAndConnect(t *testing.T) {
	app := newTestWorkbench()
	openTestConnectionEditor(t, app)
	app.profileDriver = preferences.DriverMySQL
	app.profileTLSMode = tlsModeLabel("verify-full")
	app.connectionProfiles = []preferences.ConnectionProfile{{
		ID: "mysql-1", Name: "Analytics", Driver: preferences.DriverMySQL,
		Host: "db.example.test", Port: 3306, Database: "warehouse", Username: "reader",
	}}
	tester := ui.NewTester(app.view, 1440, 900)
	if _, testFound := tester.Find("测试连接"); !testFound {
		t.Fatal("MySQL profile does not expose test and connect actions")
	}
	if _, connectFound := tester.Find("保存并连接"); !connectFound {
		t.Fatal("MySQL profile does not expose test and connect actions")
	}
	if tester.HasText("驱动待接入") {
		t.Fatal("MySQL profile is still marked as pending")
	}
	if !tester.HasText("自定义 CA（可选，PEM 文件路径；追加到系统根证书）") || !tester.HasText("MySQL 使用系统根证书和可选自定义 CA 验证证书链及主机名") {
		t.Fatal("MySQL strict TLS policy is not explained in the profile UI")
	}
}

func TestPostgreSQLProfileShowsCustomCAAndStrictTLSPolicy(t *testing.T) {
	app := newTestWorkbench()
	openTestConnectionEditor(t, app)
	app.profileDriver = preferences.DriverPostgreSQL
	app.profileTLSMode = tlsModeLabel("verify-full")
	app.connectionProfiles = []preferences.ConnectionProfile{{
		ID: "postgres-1", Name: "Analytics", Driver: preferences.DriverPostgreSQL,
		Host: "db.example.test", Port: 5432, Database: "analytics", Username: "reader",
	}}
	tester := ui.NewTester(app.view, 1440, 900)
	for _, label := range []string{"自定义 CA（可选，PEM 文件路径；追加到系统根证书）", "PostgreSQL 使用系统根证书和可选自定义 CA 验证证书链及主机名", "测试连接", "保存并连接"} {
		if !tester.HasText(label) {
			t.Errorf("PostgreSQL profile UI is missing %q", label)
		}
	}
}

func TestRedisProfileCanTestConnectAndExplainsTLS(t *testing.T) {
	app := newTestWorkbench()
	openTestConnectionEditor(t, app)
	app.profileDriver = preferences.DriverRedis
	app.profileTLSMode = tlsModeLabel("verify-full")
	app.connectionProfiles = []preferences.ConnectionProfile{{
		ID: "redis-1", Name: "Cache", Driver: preferences.DriverRedis, Host: "cache.example.test", Port: 6379,
	}}
	tester := ui.NewTester(app.view, 1440, 900)
	for _, name := range []string{"Redis DB 索引", "主机", "端口", "用户名", "密码（留空则保留已保存密码）", "测试连接", "保存并连接"} {
		if !tester.HasText(name) {
			t.Errorf("Redis profile UI is missing %q", name)
		}
	}
	if !tester.HasText("Redis TLS 使用系统根证书验证证书和主机名") {
		t.Fatal("Redis strict TLS policy is not explained")
	}
	if tester.HasText("驱动待接入") {
		t.Fatal("Redis profile is still marked as pending")
	}
}

func TestMongoProfileCanTestAndConnect(t *testing.T) {
	app := newTestWorkbench()
	openTestConnectionEditor(t, app)
	app.profileDriver = preferences.DriverMongoDB
	app.profileTLSMode = tlsModeLabel("verify-full")
	app.connectionProfiles = []preferences.ConnectionProfile{{ID: "mongo-1", Name: "Catalog", Driver: preferences.DriverMongoDB, Host: "mongo.example.test", Port: 27017, Database: "catalog", AuthSource: "admin", Username: "reader"}}
	tester := ui.NewTester(app.view, 1440, 900)
	for _, label := range []string{"数据库", "自定义 CA（可选，PEM 文件路径；追加到系统根证书）", "认证数据库", "用户名", "密码（留空则保留已保存密码）", "测试连接", "保存并连接"} {
		if !tester.HasText(label) {
			t.Errorf("MongoDB profile UI is missing %q", label)
		}
	}
	if !tester.HasText("MongoDB TLS 使用系统根证书和可选自定义 CA 验证证书和主机名") {
		t.Fatal("MongoDB TLS policy is not explained")
	}
	if tester.HasText("驱动待接入") {
		t.Fatal("MongoDB profile is still pending")
	}
}

func TestMongoWorkspaceShowsFindPipelineAndDocumentActions(t *testing.T) {
	app := newTestWorkbench()
	app.documentStore = testDocuments{}
	app.openedDriver = connection.DriverMongoDB
	app.openedLabel = "Catalog · localhost"
	app.mongoDatabases = []string{"catalog"}
	app.mongoCollections = []string{"items"}
	app.mongoSelectedDB = "catalog"
	app.mongoSelectedColl = "items"
	app.mongoHasMore = true
	app.mongoDocuments = []connection.Document{{IDJSON: `{"$oid":"65a000000000000000000001"}`, PreviewJSON: `{"name":"Ada"}`}}
	tester := ui.NewTester(app.view, 1440, 900)
	for _, label := range []string{"MongoDB 文档", "catalog", "items", "MongoDB filter JSON", "MongoDB projection JSON", "查询文档", "加载下一页", "新建文档"} {
		if !tester.HasText(label) {
			t.Errorf("MongoDB workspace is missing %q", label)
		}
	}
	if _, found := tester.Find("SQL 查询编辑器"); found {
		t.Fatal("MongoDB workspace displayed the SQL editor")
	}
}

func TestRedisMatchPatternTreatsPlainInputAsPrefix(t *testing.T) {
	for input, want := range map[string]string{"cache:": "cache:*", "cache:*": "cache:*", "": "*", "users?": "users?"} {
		if got := redisMatchPattern(input); got != want {
			t.Errorf("redisMatchPattern(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestRedisWorkspaceShowsTypeTTLValueAndSafetyActions(t *testing.T) {
	app := newTestWorkbench()
	app.keyStore = testKeyValue{}
	app.openedDriver = connection.DriverRedis
	app.openedLabel = "Cache · localhost/0"
	app.redisKeys = []string{"cache:session"}
	app.redisSelectedKey = "cache:session"
	app.redisInfo = connection.KeyInfo{Key: "cache:session", Type: "hash", TTL: 30 * time.Second, Length: 256}
	app.redisPage = connection.KeyValuePage{Type: "hash", Entries: []connection.KeyValueEntry{{Field: "user", Value: "panda"}}}
	tester := ui.NewTester(app.view, 1440, 900)
	for _, label := range []string{"Redis 键列表", "cache:session", "HASH", "30秒", "user", "panda", "删除此键…", "添加条目", "字段"} {
		if !tester.HasText(label) {
			t.Errorf("Redis workspace missing %q", label)
		}
	}
	if _, found := tester.Find("SQL 查询编辑器"); found {
		t.Fatal("Redis key/value workspace displayed the SQL editor")
	}
}

func TestRedisMutationConfirmationDescribesExactOperationAndTTLImpact(t *testing.T) {
	tests := []struct {
		name      string
		mutation  connection.KeyMutation
		wantLines []string
	}{
		{
			name:      "string replacement clears existing expiry",
			mutation:  connection.KeyMutation{Key: "cache:item", Type: "string", Value: "line1\nline2"},
			wantLines: []string{"键：cache:item", "操作：替换整个字符串值", `值（11 字节，转义预览最多 4 KiB）：`, `"line1\nline2"`, "TTL：写入后永不过期（会清除现有 TTL）"},
		},
		{
			name:      "hash field update keeps expiry unless a new ttl is set",
			mutation:  connection.KeyMutation{Key: "cache:hash", Type: "hash", Field: "owner", Value: "Ada", PreserveTTL: true},
			wantLines: []string{"操作：新增或更新 Hash 字段", "字段（5 字节，转义预览最多 4 KiB）：", `"owner"`, "TTL：写入后保留现有 TTL"},
		},
		{
			name:      "list append with ttl changes whole key expiry",
			mutation:  connection.KeyMutation{Key: "queue:jobs", Type: "list", Value: "job", TTL: 5 * time.Second},
			wantLines: []string{"操作：在列表末尾追加元素", "TTL：写入后设置整个键的 TTL：5s"},
		},
		{
			name:      "set and sorted set describe member behavior",
			mutation:  connection.KeyMutation{Key: "members", Type: "set", Value: "panda"},
			wantLines: []string{"操作：添加 Set 成员（重复成员不会增加）"},
		},
		{
			name:      "sorted set confirms score",
			mutation:  connection.KeyMutation{Key: "scores", Type: "zset", Value: "panda", Score: 9.5},
			wantLines: []string{"操作：新增成员或更新成员分数", "Score：9.5"},
		},
		{
			name:      "stream appends a record",
			mutation:  connection.KeyMutation{Key: "events", Type: "stream", Field: "kind", Value: "created"},
			wantLines: []string{"操作：向 Stream 追加一条记录", `字段（4 字节，转义预览最多 4 KiB）：`, `"kind"`},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			detail := redisMutationConfirmationDetail(test.mutation)
			for _, want := range test.wantLines {
				if !strings.Contains(detail, want) {
					t.Errorf("confirmation detail is missing %q:\n%s", want, detail)
				}
			}
		})
	}
}

func TestRedisMutationConfirmationBoundsLargeValuePreview(t *testing.T) {
	detail := redisMutationConfirmationDetail(connection.KeyMutation{
		Key: "cache:large", Type: "string", Value: strings.Repeat("x", 8192),
	})
	if !strings.Contains(detail, "值（8192 字节，转义预览最多 4 KiB）") || !strings.Contains(detail, "预览已截断") {
		t.Fatalf("large value preview lacks a clear bound: %q", detail)
	}
	if len(detail) > 4300 {
		t.Fatalf("large confirmation detail was not bounded: %d bytes", len(detail))
	}
}

func TestRedisOperationConfirmationIncludesEnvironmentAndConnection(t *testing.T) {
	detail := redisOperationConfirmationDetail(
		"生产", "Cache · redis.example.test/2", "将删除 Redis 键：cache:session\n此操作无法在 DBL 中撤销。",
	)
	for _, want := range []string{"环境：生产", "连接：Cache · redis.example.test/2", "将删除 Redis 键：cache:session", "此操作无法在 DBL 中撤销。"} {
		if !strings.Contains(detail, want) {
			t.Errorf("Redis confirmation detail is missing %q:\n%s", want, detail)
		}
	}
}

func TestMongoDocumentConfirmationIncludesEnvironmentAndTarget(t *testing.T) {
	detail := mongoDocumentConfirmationDetail("生产", "Orders · mongo.example.test", "sales", "orders", "order-42", "+ 新文档\n{\"status\":\"paid\"}")
	for _, want := range []string{"环境：生产", "连接：Orders · mongo.example.test", "数据库：sales", "集合：orders", "文档 ID：order-42", "+ 新文档", `{"status":"paid"}`} {
		if !strings.Contains(detail, want) {
			t.Errorf("MongoDB confirmation detail is missing %q:\n%s", want, detail)
		}
	}
}

func TestRedisWriteRejectsOversizeInputsBeforeBuildingConfirmation(t *testing.T) {
	for _, test := range []struct {
		name  string
		field string
		value string
	}{
		{name: "value", value: strings.Repeat("x", redisadapter.MaxValueBytes+1)},
		{name: "field", field: strings.Repeat("f", redisadapter.MaxValueBytes+1), value: "value"},
	} {
		t.Run(test.name, func(t *testing.T) {
			app := newTestWorkbench()
			app.keyStore = testKeyValue{}
			app.redisSelectedKey = "cache:item"
			app.redisInfo.Type = "hash"
			app.redisEntryField = test.field
			app.redisValueDraft = test.value
			app.confirmWriteRedisKey()
			if app.dialogOpen {
				t.Fatal("oversize Redis input opened the confirmation dialog")
			}
			if app.errorSummary != "写入 Redis 数据失败" {
				t.Fatalf("error summary = %q, want oversize input rejection", app.errorSummary)
			}
		})
	}
}

func TestSelectingRemoteProfilePreservesSQLiteQuickOpenPath(t *testing.T) {
	app := newTestWorkbench()
	app.path = "/tmp/local.sqlite"
	app.loadConnectionProfile(preferences.ConnectionProfile{
		ID: "remote-1", Name: "Analytics", Driver: preferences.DriverPostgreSQL,
		Host: "db.example.test", Port: 5432, Database: "warehouse",
	})
	if app.path != "/tmp/local.sqlite" {
		t.Fatalf("remote profile replaced SQLite quick-open path with %q", app.path)
	}
	if app.profileHost != "db.example.test" || app.profilePort != "5432" {
		t.Fatalf("remote profile fields were not loaded: host=%q port=%q", app.profileHost, app.profilePort)
	}
}

func TestQueryWorkspacesKeepEditorAndResultsIndependent(t *testing.T) {
	app := newTestWorkbench()
	first := app.activeQuery
	first.query.Buffer.Set("SELECT 'first';")
	first.resultSQL = "SELECT 'first';"
	first.resultPath = "/db/first.sqlite"
	first.columns = []sqlstream.Column{{Name: "value"}}
	first.resultWindow.Append([][]any{{"first result"}})

	app.addWorkspace()
	second := app.activeQuery
	second.query.Buffer.Set("SELECT 'second';")
	second.resultSQL = "SELECT 'second';"
	second.resultPath = "/db/second.sqlite"
	second.columns = []sqlstream.Column{{Name: "value"}}
	second.resultWindow.Append([][]any{{"second result"}})
	if len(app.workspaces) != 2 || second == first {
		t.Fatalf("workspace set = %d tabs, active workspace aliases first: %v", len(app.workspaces), second == first)
	}

	app.activateWorkspace(0)
	if app.activeQuery != first || app.activeQuery.query.Buffer.String() != "SELECT 'first';" {
		t.Fatalf("switch did not restore first workspace editor: %q", app.activeQuery.query.Buffer.String())
	}
	if got := app.activeQuery.resultWindow.Row(0)[0]; got != "first result" {
		t.Fatalf("first workspace result = %v", got)
	}
	app.activateWorkspace(1)
	if app.activeQuery != second || app.activeQuery.query.Buffer.String() != "SELECT 'second';" {
		t.Fatalf("switch did not restore second workspace editor: %q", app.activeQuery.query.Buffer.String())
	}
	if got := app.activeQuery.resultWindow.Row(0)[0]; got != "second result" {
		t.Fatalf("second workspace result = %v", got)
	}
	app.activeQuery.streamStopped = true
	app.activateWorkspace(0)
	app.activateWorkspace(1)
	if app.status != "已切换到 查询 2。 当前结果只包含已读取部分。" {
		t.Fatalf("status after reopening a partial-result workspace = %q", app.status)
	}
}

func TestClosingWorkspaceKeepsActiveWorkspaceAndAtLeastOneTab(t *testing.T) {
	app := newTestWorkbench()
	app.addWorkspace()
	second := app.activeQuery
	app.closeWorkspace(0)
	if len(app.workspaces) != 1 || app.activeWorkspace != 0 || app.activeQuery != second {
		t.Fatalf("after closing inactive tab: count=%d active=%d same=%v", len(app.workspaces), app.activeWorkspace, app.activeQuery == second)
	}
	app.closeWorkspace(0)
	if len(app.workspaces) != 1 {
		t.Fatalf("last workspace was closed: count=%d", len(app.workspaces))
	}
}

func TestWorkspaceLimit(t *testing.T) {
	app := newTestWorkbench()
	for len(app.workspaces) < maxWorkspaces {
		app.addWorkspace()
	}
	app.addWorkspace()
	if len(app.workspaces) != maxWorkspaces {
		t.Fatalf("workspace count = %d, want limit %d", len(app.workspaces), maxWorkspaces)
	}
}

func TestWorkbenchControlsHaveAccessibleNames(t *testing.T) {
	app := newTestWorkbench()
	app.activeQuery.columns = []sqlstream.Column{{Name: "value"}}
	app.activeQuery.resultPath = ""
	app.activeQuery.resultWindow.Append([][]any{{"sample"}})
	tester := ui.NewTester(app.view, 1440, 900)

	for _, name := range []string{
		"工作区",
		"SQL 查询编辑器",
		"查询结果",
	} {
		if _, found := tester.Find(name); !found {
			t.Errorf("accessible UI element %q was not found", name)
		}
	}
	if err := tester.Click("新建连接…"); err != nil {
		t.Fatal(err)
	}
	tester.Frame()
	for _, name := range []string{"SQLite 文件路径", "连接名称", "分组"} {
		if _, found := tester.Find(name); !found {
			t.Errorf("connection editor control %q is not accessible", name)
		}
	}
}

func TestWorkbenchAnnouncesStatusChangesOnce(t *testing.T) {
	app := newTestWorkbench()
	tester := ui.NewTester(app.view, 1200, 800)
	app.status = "查询完成 · 1 行"
	tester.Frame()

	announcements := tester.Announcements()
	if len(announcements) != 1 || announcements[0] != app.status {
		t.Fatalf("status announcements = %#v, want one announcement %q", announcements, app.status)
	}
	tester.Frame()
	if announcements := tester.Announcements(); len(announcements) != 0 {
		t.Fatalf("unchanged status was announced again: %#v", announcements)
	}
}

func TestAIAssistantUsesFullPageAtNarrowWindowWidths(t *testing.T) {
	app := newTestWorkbench()
	app.showAI = true
	tester := ui.NewTester(app.view, 1440, 900)
	if _, found := tester.Find("AI 任务"); !found {
		t.Fatal("AI assistant is missing at a wide window size")
	}
	tester.SetSize(900, 700)
	tester.Frame()
	if _, found := tester.Find("AI 任务"); !found {
		t.Fatal("AI assistant disappeared below the narrow-window breakpoint")
	}
	if _, found := tester.Find("SQL 查询编辑器"); found {
		t.Fatal("narrow window did not switch the AI assistant to full-page layout")
	}
	tester.SetSize(1440, 900)
	tester.Frame()
	if _, found := tester.Find("SQL 查询编辑器"); !found {
		t.Fatal("wide window did not restore the main SQL workspace")
	}
}
