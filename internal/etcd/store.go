// Package etcd adapts etcd v3 to DBL's bounded revisioned key/value contract.
package etcd

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/magalab/dbl/internal/connection"
	"github.com/magalab/dbl/internal/credentials"
	"github.com/magalab/dbl/internal/preferences"
	mvccpb "go.etcd.io/etcd/api/v3/mvccpb"
	"go.etcd.io/etcd/api/v3/v3rpc/rpctypes"
	clientv3 "go.etcd.io/etcd/client/v3"
	"go.uber.org/zap"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	defaultPort   = 2379
	maxPageSize   = 100
	maxValueBytes = 1 << 20
)

type Connector struct {
	Profile preferences.ConnectionProfile
	Vault   *credentials.Vault
}

func (connector Connector) Connect(ctx context.Context) (connection.Connection, error) {
	p := connector.Profile
	if p.Driver != preferences.DriverEtcd {
		return nil, errors.New("etcd connector received a different database type")
	}
	endpoints, tlsEnabled, err := normalizeEndpoints(p.Host, p.Port, p.TLSMode)
	if err != nil {
		return nil, err
	}
	if !tlsEnabled && (p.TLSCAPath != "" || p.TLSCertRef != "" || p.TLSKeyRef != "") {
		return nil, errors.New("etcd CA and client certificates require TLS")
	}
	cfg := clientv3.Config{Endpoints: endpoints, DialTimeout: 10 * time.Second, Username: p.Username, Logger: zap.NewNop()}
	var password []byte
	if p.CredentialRef != "" {
		if connector.Vault == nil {
			return nil, credentials.ErrUnavailable
		}
		password, err = connector.Vault.Load(credentials.Reference(p.CredentialRef))
		if err != nil {
			return nil, fmt.Errorf("load etcd credential: %w", err)
		}
	}
	defer clear(password)
	cfg.Password = string(password)
	if tlsEnabled && p.TLSMode != "disable" {
		tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12}
		if p.TLSCAPath != "" {
			pem, readErr := os.ReadFile(p.TLSCAPath)
			if readErr != nil {
				return nil, fmt.Errorf("read etcd CA certificate: %w", readErr)
			}
			roots, poolErr := x509.SystemCertPool()
			if poolErr != nil || roots == nil {
				roots = x509.NewCertPool()
			}
			if !roots.AppendCertsFromPEM(pem) {
				return nil, errors.New("etcd CA file contains no valid certificates")
			}
			tlsConfig.RootCAs = roots
		}
		if p.TLSCertRef != "" || p.TLSKeyRef != "" {
			if connector.Vault == nil {
				return nil, credentials.ErrUnavailable
			}
			certPEM, loadErr := connector.Vault.Load(credentials.Reference(p.TLSCertRef))
			if loadErr != nil {
				return nil, fmt.Errorf("load etcd client certificate: %w", loadErr)
			}
			defer clear(certPEM)
			keyPEM, loadErr := connector.Vault.Load(credentials.Reference(p.TLSKeyRef))
			if loadErr != nil {
				return nil, fmt.Errorf("load etcd client key: %w", loadErr)
			}
			defer clear(keyPEM)
			pair, pairErr := tls.X509KeyPair(certPEM, keyPEM)
			if pairErr != nil {
				return nil, fmt.Errorf("parse etcd client certificate: %w", pairErr)
			}
			tlsConfig.Certificates = []tls.Certificate{pair}
		}
		cfg.TLS = tlsConfig
	}
	cli, err := clientv3.New(cfg)
	if err != nil {
		return nil, sanitize("configure etcd client", err)
	}
	ctx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	if _, err := cli.Status(ctx, endpoints[0]); err != nil {
		_ = cli.Close()
		return nil, sanitize("connect to etcd", err)
	}
	return &Store{client: cli, readOnly: p.ReadOnly}, nil
}

// normalizeEndpoints accepts comma-separated host:port or http(s) URLs. URLs
// preserve their own ports; bare hosts use the profile port (or 2379).
func normalizeEndpoints(raw string, port int, tlsMode string) ([]string, bool, error) {
	if port == 0 {
		port = defaultPort
	}
	if port < 1 || port > 65535 {
		return nil, false, errors.New("etcd port must be between 1 and 65535")
	}
	parts := strings.Split(raw, ",")
	endpoints := make([]string, 0, len(parts))
	tlsEnabled := false
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			return nil, false, errors.New("etcd endpoint is required")
		}
		scheme := ""
		if strings.HasPrefix(part, "https://") {
			scheme, part, tlsEnabled = "https://", strings.TrimPrefix(part, "https://"), true
		} else if strings.HasPrefix(part, "http://") {
			scheme, part = "http://", strings.TrimPrefix(part, "http://")
		} else if strings.Contains(part, "://") {
			return nil, false, errors.New("etcd endpoint URL must use http or https")
		}
		if part == "" {
			return nil, false, errors.New("etcd endpoint host is required")
		}
		if strings.ContainsAny(part, "/?#@ \t\r\n") {
			return nil, false, errors.New("etcd endpoint must be a host and optional port")
		}
		if _, _, err := net.SplitHostPort(part); err != nil {
			if strings.Contains(part, ":") && net.ParseIP(strings.Trim(part, "[]")) == nil {
				return nil, false, fmt.Errorf("invalid etcd endpoint %q", part)
			}
			part = net.JoinHostPort(strings.Trim(part, "[]"), strconv.Itoa(port))
		}
		if scheme == "" {
			if tlsMode == "disable" {
				scheme = "http://"
			} else {
				scheme, tlsEnabled = "https://", true
			}
		}
		if (scheme == "https://") != (tlsMode != "disable") {
			return nil, false, errors.New("etcd endpoint scheme and TLS mode do not match")
		}
		endpoints = append(endpoints, scheme+part)
	}
	return endpoints, tlsEnabled, nil
}

type Store struct {
	client   *clientv3.Client
	readOnly bool
}

func (s *Store) Driver() connection.Driver { return connection.DriverEtcd }
func (s *Store) HealthCheck(ctx context.Context) error {
	if err := s.ensureOpen(); err != nil {
		return err
	}
	_, err := s.client.Status(ctx, s.client.Endpoints()[0])
	return err
}
func (s *Store) Close() error {
	if s == nil || s.client == nil {
		return nil
	}
	return s.client.Close()
}
func (s *Store) ClassifyError(err error) connection.ErrorKind {
	if err == nil {
		return connection.ErrorUnknown
	}
	if kind := connection.ClassifyCommonError(err); kind != connection.ErrorUnknown {
		return kind
	}
	switch status.Code(err) {
	case codes.Unauthenticated, codes.PermissionDenied:
		return connection.ErrorPermission
	case codes.DeadlineExceeded:
		return connection.ErrorTimeout
	case codes.Unavailable, codes.ResourceExhausted, codes.Aborted:
		return connection.ErrorBusy
	case codes.InvalidArgument:
		return connection.ErrorQuery
	}
	if errors.Is(err, rpctypes.ErrKeyNotFound) {
		return connection.ErrorNotFound
	}
	return connection.ErrorUnknown
}

func (s *Store) RangeKeys(ctx context.Context, key string, prefix bool, startKey string, limit int) (connection.EtcdPage, error) {
	if err := s.ensureOpen(); err != nil {
		return connection.EtcdPage{}, err
	}
	if key == "" && !prefix {
		return connection.EtcdPage{}, errors.New("exact etcd key is required")
	}
	if err := validateKey(key); err != nil && key != "" {
		return connection.EtcdPage{}, err
	}
	if startKey != "" && !prefix {
		return connection.EtcdPage{}, errors.New("exact etcd key reads cannot be paged")
	}
	limit = bounded(limit)
	queryKey := key
	if startKey != "" {
		queryKey = startKey
	}
	opts := []clientv3.OpOption{clientv3.WithSort(clientv3.SortByKey, clientv3.SortAscend), clientv3.WithLimit(int64(limit + 1))}
	if prefix {
		opts = append(opts, clientv3.WithRange(clientv3.GetPrefixRangeEnd(key)))
	}
	if startKey != "" {
		opts = append(opts, clientv3.WithFromKey())
	}
	resp, err := s.client.Get(ctx, queryKey, opts...)
	if err != nil {
		return connection.EtcdPage{}, err
	}
	page := connection.EtcdPage{Entries: make([]connection.EtcdEntry, 0, limit)}
	for i, kv := range resp.Kvs {
		if i == limit {
			page.HasMore = true
			break
		}
		page.Entries = append(page.Entries, entryFromKV(kv))
	}
	if page.HasMore && len(page.Entries) > 0 {
		page.NextKey = page.Entries[len(page.Entries)-1].Key + "\x00"
	}
	return page, nil
}

func (s *Store) GetKey(ctx context.Context, key string) (connection.EtcdEntry, error) {
	if err := s.ensureOpen(); err != nil {
		return connection.EtcdEntry{}, err
	}
	if err := validateKey(key); err != nil {
		return connection.EtcdEntry{}, err
	}
	resp, err := s.client.Get(ctx, key)
	if err != nil {
		return connection.EtcdEntry{}, err
	}
	if len(resp.Kvs) == 0 {
		return connection.EtcdEntry{}, rpctypes.ErrKeyNotFound
	}
	return entryFromKV(resp.Kvs[0]), nil
}

func (s *Store) LeaseTimeToLive(ctx context.Context, leaseID int64) (connection.EtcdLeaseInfo, error) {
	if err := s.ensureOpen(); err != nil {
		return connection.EtcdLeaseInfo{}, err
	}
	if leaseID <= 0 {
		return connection.EtcdLeaseInfo{}, errors.New("etcd lease ID must be positive")
	}
	resp, err := s.client.TimeToLive(ctx, clientv3.LeaseID(leaseID))
	if err != nil {
		return connection.EtcdLeaseInfo{}, err
	}
	return connection.EtcdLeaseInfo{TTL: resp.TTL, GrantedTTL: resp.GrantedTTL}, nil
}

func (s *Store) PutKey(ctx context.Context, key, value string, lease int64) (connection.EtcdEntry, error) {
	if err := s.checkWritable(); err != nil {
		return connection.EtcdEntry{}, err
	}
	if err := validateKey(key); err != nil {
		return connection.EtcdEntry{}, err
	}
	if len(value) > maxValueBytes {
		return connection.EtcdEntry{}, errors.New("etcd value exceeds 1 MiB write limit")
	}
	opts := []clientv3.OpOption{}
	if lease != 0 {
		opts = append(opts, clientv3.WithLease(clientv3.LeaseID(lease)))
	}
	if _, err := s.client.Put(ctx, key, value, opts...); err != nil {
		return connection.EtcdEntry{}, err
	}
	return s.GetKey(ctx, key)
}

func (s *Store) CountKeys(ctx context.Context, key string, prefix bool) (int64, error) {
	if err := s.ensureOpen(); err != nil {
		return 0, err
	}
	if err := validateKey(key); err != nil && !(prefix && key == "") {
		return 0, err
	}
	opts := []clientv3.OpOption{clientv3.WithCountOnly()}
	if prefix {
		opts = append(opts, clientv3.WithPrefix())
	}
	resp, err := s.client.Get(ctx, key, opts...)
	if err != nil {
		return 0, err
	}
	return resp.Count, nil
}

func (s *Store) DeleteKeys(ctx context.Context, key string, prefix bool) (int64, error) {
	if err := s.checkWritable(); err != nil {
		return 0, err
	}
	if key == "" {
		return 0, errors.New("refusing to delete an empty etcd prefix")
	}
	if err := validateKey(key); err != nil {
		return 0, err
	}
	opts := []clientv3.OpOption{}
	if prefix {
		opts = append(opts, clientv3.WithPrefix())
	}
	resp, err := s.client.Delete(ctx, key, opts...)
	if err != nil {
		return 0, err
	}
	return resp.Deleted, nil
}

func (s *Store) Watch(ctx context.Context, key string, prefix bool, revision int64) <-chan connection.EtcdWatchEvent {
	out := make(chan connection.EtcdWatchEvent, 32)
	if err := s.ensureOpen(); err != nil {
		out <- connection.EtcdWatchEvent{Err: err}
		close(out)
		return out
	}
	if err := validateKey(key); err != nil && !(prefix && key == "") {
		out <- connection.EtcdWatchEvent{Err: err}
		close(out)
		return out
	}
	opts := []clientv3.OpOption{}
	if prefix {
		opts = append(opts, clientv3.WithPrefix())
	}
	if revision > 0 {
		opts = append(opts, clientv3.WithRev(revision))
	}
	go func() {
		defer close(out)
		for resp := range s.client.Watch(ctx, key, opts...) {
			if err := resp.Err(); err != nil {
				if ctx.Err() != nil {
					return
				}
				sendWatch(ctx, out, connection.EtcdWatchEvent{Err: err})
				return
			}
			for _, event := range resp.Events {
				item := watchEventFromKV(event.Type.String(), event.Kv)
				if err := sendWatch(ctx, out, item); err != nil {
					return
				}
			}
		}
	}()
	return out
}

func sendWatch(ctx context.Context, out chan<- connection.EtcdWatchEvent, event connection.EtcdWatchEvent) error {
	select {
	case out <- event:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
func (s *Store) ensureOpen() error {
	if s == nil || s.client == nil {
		return errors.New("etcd connection is closed")
	}
	return nil
}
func (s *Store) checkWritable() error {
	if err := s.ensureOpen(); err != nil {
		return err
	}
	if s.readOnly {
		return errors.New("etcd profile is read-only")
	}
	return nil
}
func validateKey(key string) error {
	if key == "" {
		return errors.New("etcd key or prefix is required")
	}
	if len(key) > 4096 {
		return errors.New("etcd key exceeds 4 KiB limit")
	}
	return nil
}
func watchEventFromKV(eventType string, kv *mvccpb.KeyValue) connection.EtcdWatchEvent {
	if kv == nil {
		return connection.EtcdWatchEvent{Type: eventType}
	}
	return connection.EtcdWatchEvent{
		Type: eventType, Key: string(kv.Key), Value: clipValue(kv.Value),
		Revision: kv.ModRevision, Version: kv.Version, Lease: int64(kv.Lease),
		ValueTruncated: len(kv.Value) > maxValueBytes,
	}
}
func bounded(n int) int {
	if n < 1 {
		return 50
	}
	if n > maxPageSize {
		return maxPageSize
	}
	return n
}
func clipValue(value []byte) string {
	if len(value) > maxValueBytes {
		value = value[:maxValueBytes]
	}
	return string(value)
}
func entryFromKV(kv *mvccpb.KeyValue) connection.EtcdEntry {
	return connection.EtcdEntry{Key: string(kv.Key), Value: clipValue(kv.Value), ValueTruncated: len(kv.Value) > maxValueBytes, CreateRevision: kv.CreateRevision, ModRevision: kv.ModRevision, Version: kv.Version, Lease: int64(kv.Lease)}
}
func sanitize(op string, err error) error { return fmt.Errorf("%s: %w", op, err) }
