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
	Key, Type, Field, Value string
	Score                   float64
	TTL                     time.Duration
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
	TTL, GrantedTTL int64
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
