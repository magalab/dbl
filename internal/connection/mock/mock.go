// Package mock provides a deterministic adapter for lifecycle demos and tests.
package mock

import (
	"context"
	"errors"
	"sync"
	"time"

	domain "github.com/magalab/dbl/internal/connection"
)

type Connector struct {
	Delay      time.Duration
	ConnectErr error
	HealthErr  error
}

func (connector Connector) Connect(ctx context.Context) (domain.Connection, error) {
	if connector.Delay > 0 {
		timer := time.NewTimer(connector.Delay)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if connector.ConnectErr != nil {
		return nil, connector.ConnectErr
	}
	return &Connection{healthErr: connector.HealthErr}, nil
}

type Connection struct {
	mu        sync.Mutex
	closed    bool
	healthErr error
}

func (conn *Connection) Driver() domain.Driver { return "mock" }

func (conn *Connection) HealthCheck(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	conn.mu.Lock()
	defer conn.mu.Unlock()
	if conn.closed {
		return errors.New("mock connection is closed")
	}
	return conn.healthErr
}

func (conn *Connection) ClassifyError(err error) domain.ErrorKind {
	return domain.ClassifyCommonError(err)
}

func (conn *Connection) Close() error {
	conn.mu.Lock()
	defer conn.mu.Unlock()
	conn.closed = true
	return nil
}

var _ domain.Connector = Connector{}
var _ domain.Connection = (*Connection)(nil)
