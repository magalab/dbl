// Package connection defines adapter contracts and connection lifecycle state.
package connection

import (
	"context"
	"errors"
	"io"
	"os"
	"sync"
	"time"

	"github.com/magalab/dbl/internal/sqlstream"
)

type Driver string

const (
	DriverSQLite     Driver = "sqlite"
	DriverPostgreSQL Driver = "postgresql"
	DriverMySQL      Driver = "mysql"
	DriverRedis      Driver = "redis"
	DriverMongoDB    Driver = "mongodb"
	DriverEtcd       Driver = "etcd"
)

type ErrorKind uint8

const (
	ErrorUnknown ErrorKind = iota
	ErrorCanceled
	ErrorTimeout
	ErrorNotFound
	ErrorPermission
	ErrorBusy
	ErrorQuery
)

type State uint8

const (
	Disconnected State = iota
	Connecting
	Connected
	ConnectionFailed
)

type Connection interface {
	Driver() Driver
	HealthCheck(context.Context) error
	ClassifyError(error) ErrorKind
	Close() error
}

// SQL is a capability contract for adapters that support SQL workspaces.
type SQL interface {
	Connection
	Namespaces(context.Context) ([]string, error)
	CurrentNamespace(context.Context) (string, error)
	StartQuery(context.Context, string) (*sqlstream.Cursor, error)
	Objects(context.Context, string, string, string, int) (ObjectPage, error)
	Columns(context.Context, string, string) ([]Column, error)
	Export(context.Context, string, sqlstream.ExportFormat, io.Writer) (int64, error)
}

// SQLExecutor executes a statement that does not return rows and reports the
// number of affected rows. Adapters must preserve their read-only guarantees.
type SQLExecutor interface {
	ExecStatement(context.Context, string) (int64, error)
}

// QueryCancel is an adapter-provided way to stop a query on the database
// server. It complements context cancellation for drivers whose cancellation
// only closes the client connection.
type QueryCancel func(context.Context) error

// QueryCancellation is an optional SQL capability for queries that need
// explicit server-side cancellation. The adapter registers the cancel
// function before starting the query so callers can safely defer local
// cancellation until the server acknowledges the request.
type QueryCancellation interface {
	StartQueryWithCancellation(context.Context, string, func(QueryCancel)) (*sqlstream.Cursor, error)
}

// TableEditor applies a reviewed batch of edits to one SQL table in a single
// transaction. Values are passed separately from identifiers; adapters must
// quote identifiers and bind values with driver parameters.
type TableEditor interface {
	ApplyTableChanges(context.Context, Object, []string, []TableChange) error
}

// SchemaEditor applies a reviewed set of table DDL statements. Implementations
// should use a transaction when their database supports transactional DDL.
type SchemaEditor interface {
	ApplySchemaChanges(context.Context, []string) error
}

type TableChangeKind uint8

const (
	InsertRow TableChangeKind = iota + 1
	UpdateRow
	DeleteRow
)

type TableChange struct {
	Kind     TableChangeKind
	Original map[string]any
	Values   map[string]any
}

const MaxTableChangeBatch = 500

// CurrentDatabase is an optional SQL capability for adapters that can report
// the server-side database selected by a connection.
type CurrentDatabase interface {
	CurrentDatabase(context.Context) (string, error)
}

// KeyValue is a capability contract for key/value stores. Cursor values are
// opaque to the UI and pages must remain bounded.
type KeyValue interface {
	Connection
	ScanKeys(context.Context, string, uint64, int) (KeyPage, error)
	ScanKeysByType(context.Context, string, string, uint64, int) (KeyPage, error)
	InspectKey(context.Context, string) (KeyInfo, error)
	ReadKey(context.Context, string, string, int) (KeyValuePage, error)
	WriteKey(context.Context, KeyMutation) error
	DeleteKey(context.Context, string) error
}

// RedisBulkKeyInspector is an optional Redis capability for loading compact
// type and TTL metadata for a page of keys without issuing one round trip per key.
type RedisBulkKeyInspector interface {
	InspectKeys(context.Context, []string) ([]KeyInfo, error)
}

// RedisBulkKeyWriter creates a key from multiple entries in one Redis
// transaction where the adapter can express the data type as a batch.
type RedisBulkKeyWriter interface {
	WriteKeyBatch(context.Context, []KeyMutation) error
}

// RedisBulkKeyDeleter removes several keys with one Redis DEL operation.
type RedisBulkKeyDeleter interface {
	DeleteRedisKeys(context.Context, []string) error
}

// RedisJSONSupport reports whether the connected Redis server recognizes the
// RedisJSON commands without modifying data.
type RedisJSONSupport interface {
	CheckJSONSupport(context.Context) error
}

// RedisDatabaseAdmin is an optional capability for operations scoped to the
// Redis logical database selected by the connection profile.
type RedisDatabaseAdmin interface {
	DatabaseSize(context.Context) (int64, error)
	FlushDatabase(context.Context) error
}

// Documents is a capability contract for document databases. JSON inputs and
// outputs use MongoDB Extended JSON to preserve BSON values.
type Documents interface {
	Connection
	Databases(context.Context) ([]string, error)
	Collections(context.Context, string) ([]string, error)
	CollectionIndexes(context.Context, string, string) ([]DocumentIndex, error)
	FindDocuments(context.Context, string, string, string, string, uint64, int) (DocumentPage, error)
	AggregateDocuments(context.Context, string, string, string, uint64, int) (DocumentPage, error)
	GetDocument(context.Context, string, string, string) (Document, error)
	ValidateDocument(string) error
	InsertDocument(context.Context, string, string, string) error
	ReplaceDocument(context.Context, string, string, string, string) error
}

// Configuration is a capability contract for revisioned key/value stores.
// StartKey is an opaque exclusive continuation key returned by RangeKeys.
type Configuration interface {
	Connection
	RangeKeys(context.Context, string, bool, string, int) (EtcdPage, error)
	GetKey(context.Context, string) (EtcdEntry, error)
	LeaseTimeToLive(context.Context, int64) (EtcdLeaseInfo, error)
	PutKey(context.Context, string, string, int64) (EtcdEntry, error)
	CountKeys(context.Context, string, bool) (int64, error)
	DeleteKeys(context.Context, string, bool) (int64, error)
	Watch(context.Context, string, bool, int64) <-chan EtcdWatchEvent
}

// EtcdKeyCreator exposes an atomic create-only write so adding a key cannot
// silently overwrite an existing value.
type EtcdKeyCreator interface {
	CreateKey(context.Context, string, string, int64) (EtcdEntry, error)
}

// EtcdKeyTTLWriter creates a key with a newly granted, expiring lease.
type EtcdKeyTTLWriter interface {
	CreateKeyWithTTL(context.Context, string, string, int64) (EtcdEntry, error)
}

// EtcdLeaseAdmin exposes lease listing, inspection, creation, and revocation.
// Revoking a lease deletes every key attached to it.
type EtcdLeaseAdmin interface {
	ListLeases(context.Context) ([]int64, error)
	LeaseDetails(context.Context, int64) (EtcdLeaseInfo, error)
	GrantLease(context.Context, int64) (EtcdLeaseInfo, error)
	RevokeLease(context.Context, int64) error
}

// EtcdAdmin exposes the optional cluster and authentication APIs available on etcd v3.
// Secret fields are accepted only for the duration of a call and are never returned.
type EtcdAdmin interface {
	Dashboard(context.Context) (EtcdDashboard, error)
	AuthOverview(context.Context) (EtcdAuthOverview, error)
	UserDetails(context.Context, string) (EtcdUser, error)
	RoleDetails(context.Context, string) (EtcdRole, error)
	SetAuthEnabled(context.Context, bool) error
	CreateUser(context.Context, string, string) error
	DeleteUser(context.Context, string) error
	ChangeUserPassword(context.Context, string, string) error
	GrantUserRole(context.Context, string, string) error
	RevokeUserRole(context.Context, string, string) error
	CreateRole(context.Context, string) error
	DeleteRole(context.Context, string) error
	GrantRolePermission(context.Context, string, string, string, string) error
	RevokeRolePermission(context.Context, string, string, string) error
	Defragment(context.Context, string) error
	DisarmAlarm(context.Context, uint64, string) error
}

type Connector interface {
	Connect(context.Context) (Connection, error)
}

type Object struct {
	Schema string
	Name   string
	Type   string
	SQL    string
}

type ObjectPage struct {
	Objects    []Object
	NextSchema string
	NextType   string
	NextName   string
	HasMore    bool
}

type Column struct {
	Position   int
	Name       string
	Type       string
	Extra      string
	Collation  string
	Comment    string
	NotNull    bool
	PrimaryKey bool
	Hidden     int
	Default    any
}

type KeyPage struct {
	Keys       []string
	NextCursor uint64
}
type KeyInfo struct {
	Key, Type string
	TTL       time.Duration
	Length    int64
}
type KeyValueEntry struct {
	Field, Value string
	Score        float64
	Truncated    bool
}
type KeyMutation struct {
	Key, Type, ID, Field, Value string
	Score                       float64
	TTL                         time.Duration
	PreserveTTL                 bool
}
type KeyValuePage struct {
	Type                 string
	StringValue          string
	StringValueTruncated bool
	Entries              []KeyValueEntry
	NextCursor           string
}

type Document struct {
	IDJSON, PreviewJSON, JSON string
	Truncated                 bool
}
type DocumentPage struct {
	Documents  []Document
	NextOffset uint64
	HasMore    bool
}
type DocumentIndex struct {
	Name      string
	KeysJSON  string
	Unique    bool
	Sparse    bool
	HasTTL    bool
	TTLSecond int64
}

type EtcdEntry struct {
	Key, Value                                  string
	CreateRevision, ModRevision, Version, Lease int64
	ValueTruncated                              bool
}
type EtcdLeaseInfo struct {
	ID              int64
	TTL, GrantedTTL int64
	Keys            []string
	KeyCount        int
	KeysTruncated   bool
}
type EtcdPage struct {
	Entries []EtcdEntry
	NextKey string
	HasMore bool
}
type EtcdWatchEvent struct {
	Type, Key, Value         string
	Revision, Version, Lease int64
	ValueTruncated           bool
	Err                      error
}

type EtcdDashboard struct {
	Members       []EtcdMember
	Alarms        []EtcdAlarm
	Metrics       EtcdMetrics
	MemberListErr string
	AlarmListErr  string
	AuthStatusErr string
	AuthEnabled   bool
	AuthRevision  uint64
	ClusterID     uint64
	Revision      int64
}

type EtcdMember struct {
	ID               uint64
	Name             string
	ClientURLs       []string
	PeerURLs         []string
	IsLearner        bool
	Reachable        bool
	Endpoint         string
	StatusError      string
	Version          string
	ReportedMemberID uint64
	LeaderID         uint64
	Revision         int64
	RaftIndex        uint64
	RaftAppliedIndex uint64
	RaftTerm         uint64
	BackendSize      int64
	BackendSizeInUse int64
	BackendSizeQuota int64
	ServerErrors     []string
}

type EtcdAlarm struct {
	MemberID uint64
	Type     string
}

type EtcdMetrics struct {
	Available           bool
	Endpoint            string
	Error               string
	SampledAt           time.Time
	UptimeSeconds       float64
	HasLeader           float64
	LeaderChanges       float64
	ProposalsCommitted  float64
	ProposalsApplied    float64
	ProposalsPending    float64
	ProposalsFailed     float64
	BackendSize         float64
	BackendSizeInUse    float64
	BackendQuota        float64
	ResidentMemoryBytes float64
	Goroutines          float64
	GRPCRequestsTotal   float64
	GRPCFailuresTotal   float64
}

type EtcdAuthOverview struct {
	Enabled     bool
	Revision    uint64
	Users       []string
	Roles       []string
	StatusError string
	UsersError  string
	RolesError  string
}

type EtcdUser struct {
	Name  string
	Roles []string
}

type EtcdRole struct {
	Name        string
	Permissions []EtcdPermission
}

type EtcdPermission struct {
	Type     string
	Key      string
	RangeEnd string
}

type Snapshot struct {
	State  State
	Driver Driver
	Err    error
}

// Manager serializes connection attempts and disconnects while allowing UI
// code to inspect lifecycle state without owning adapter internals.
type Manager struct {
	operation sync.Mutex
	mu        sync.RWMutex
	state     State
	active    Connection
	err       error
}

func (manager *Manager) Connect(ctx context.Context, connector Connector) error {
	manager.operation.Lock()
	defer manager.operation.Unlock()

	manager.mu.Lock()
	previous := manager.active
	manager.active = nil
	manager.state = Connecting
	manager.err = nil
	manager.mu.Unlock()
	if previous != nil {
		_ = previous.Close()
	}

	active, err := connector.Connect(ctx)
	if err == nil && active == nil {
		err = errors.New("connector returned no connection")
	}
	if err == nil && ctx.Err() != nil {
		err = ctx.Err()
	}
	if err != nil {
		if active != nil {
			_ = active.Close()
		}
		manager.mu.Lock()
		manager.state = ConnectionFailed
		manager.err = err
		manager.mu.Unlock()
		return err
	}

	manager.mu.Lock()
	manager.active = active
	manager.state = Connected
	manager.mu.Unlock()
	return nil
}

func (manager *Manager) Disconnect() error {
	manager.operation.Lock()
	defer manager.operation.Unlock()
	manager.mu.Lock()
	active := manager.active
	manager.active = nil
	manager.state = Disconnected
	manager.err = nil
	manager.mu.Unlock()
	if active != nil {
		return active.Close()
	}
	return nil
}

// Replace installs an already connected adapter and returns the previous one.
// The caller owns closing the returned adapter after its UI/workspace switch.
func (manager *Manager) Replace(active Connection) Connection {
	manager.operation.Lock()
	defer manager.operation.Unlock()
	manager.mu.Lock()
	previous := manager.active
	manager.active = active
	manager.err = nil
	if active == nil {
		manager.state = Disconnected
	} else {
		manager.state = Connected
	}
	manager.mu.Unlock()
	return previous
}

func (manager *Manager) HealthCheck(ctx context.Context) error {
	manager.mu.RLock()
	active := manager.active
	manager.mu.RUnlock()
	if active == nil {
		return errors.New("no active connection")
	}
	return active.HealthCheck(ctx)
}

func (manager *Manager) Snapshot() Snapshot {
	manager.mu.RLock()
	defer manager.mu.RUnlock()
	snapshot := Snapshot{State: manager.state, Err: manager.err}
	if manager.active != nil {
		snapshot.Driver = manager.active.Driver()
	}
	return snapshot
}

func ClassifyCommonError(err error) ErrorKind {
	switch {
	case err == nil:
		return ErrorUnknown
	case errors.Is(err, context.Canceled):
		return ErrorCanceled
	case errors.Is(err, context.DeadlineExceeded):
		return ErrorTimeout
	case errors.Is(err, os.ErrNotExist):
		return ErrorNotFound
	case errors.Is(err, os.ErrPermission):
		return ErrorPermission
	default:
		return ErrorUnknown
	}
}
