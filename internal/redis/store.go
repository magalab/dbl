// Package redis adapts a single Redis endpoint to DBL's bounded key/value contract.
package redis

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/magalab/dbl/internal/connection"
	"github.com/magalab/dbl/internal/credentials"
	"github.com/magalab/dbl/internal/preferences"
	redisclient "github.com/redis/go-redis/v9"
)

const (
	defaultPort = 6379
	maxPageSize = 100
	// MaxValueBytes is the largest Redis value or field size accepted by the adapter.
	MaxValueBytes  = 1 << 20
	maxStringBytes = MaxValueBytes
)

type Connector struct {
	Profile preferences.ConnectionProfile
	Vault   *credentials.Vault
}

func (connector Connector) Connect(ctx context.Context) (connection.Connection, error) {
	p := connector.Profile
	if p.Driver != preferences.DriverRedis {
		return nil, errors.New("Redis connector received a different database type")
	}
	if strings.TrimSpace(p.Host) == "" {
		return nil, errors.New("Redis host is required")
	}
	port := p.Port
	if port == 0 {
		port = defaultPort
	}
	if port < 1 || port > 65535 {
		return nil, errors.New("Redis port must be between 1 and 65535")
	}
	var password []byte
	if p.CredentialRef != "" {
		secret, err := connector.Vault.Load(credentials.Reference(p.CredentialRef))
		if err != nil {
			return nil, fmt.Errorf("load Redis credential: %w", err)
		}
		password = secret
	}
	defer clear(password)
	host := strings.Trim(strings.TrimSpace(p.Host), "[]")
	options := &redisclient.Options{Addr: net.JoinHostPort(host, strconv.Itoa(port)), Username: p.Username, Password: string(password), DB: p.RedisDB, DialTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second, MaxRetries: 1}
	if p.TLSMode != "" && p.TLSMode != "disable" {
		config, err := tlsConfig(p.Host, p.TLSMode)
		if err != nil {
			return nil, err
		}
		options.TLSConfig = config
	}
	client := redisclient.NewClient(options)
	pingCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := client.Ping(pingCtx).Err(); err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("connect to Redis: %w", err)
	}
	return &Store{client: client, readOnly: p.ReadOnly}, nil
}

func tlsConfig(host, mode string) (*tls.Config, error) {
	cfg := &tls.Config{MinVersion: tls.VersionTLS12, ServerName: strings.TrimSpace(host)}
	switch mode {
	case "verify-full", "":
		return cfg, nil
	case "verify-ca":
		roots, err := x509.SystemCertPool()
		if err != nil || roots == nil {
			roots = x509.NewCertPool()
		}
		cfg.InsecureSkipVerify = true // chain and EKU are checked below; hostname is intentionally omitted.
		cfg.VerifyConnection = func(state tls.ConnectionState) error {
			if len(state.PeerCertificates) == 0 {
				return errors.New("Redis server sent no certificate")
			}
			intermediates := x509.NewCertPool()
			for _, cert := range state.PeerCertificates[1:] {
				intermediates.AddCert(cert)
			}
			_, err := state.PeerCertificates[0].Verify(x509.VerifyOptions{Roots: roots, Intermediates: intermediates, KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}})
			return err
		}
		return cfg, nil
	case "require":
		cfg.InsecureSkipVerify = true
		return cfg, nil
	case "disable":
		return nil, nil
	default:
		return nil, errors.New("unsupported Redis TLS mode")
	}
}

type Store struct {
	client   *redisclient.Client
	readOnly bool
}

func (s *Store) Driver() connection.Driver { return connection.DriverRedis }
func (s *Store) HealthCheck(ctx context.Context) error {
	if s == nil || s.client == nil {
		return errors.New("Redis connection is closed")
	}
	return s.client.Ping(ctx).Err()
}
func (s *Store) Close() error {
	if s == nil || s.client == nil {
		return nil
	}
	return s.client.Close()
}

func (s *Store) DatabaseSize(ctx context.Context) (int64, error) {
	if s == nil || s.client == nil {
		return 0, errors.New("Redis connection is closed")
	}
	return s.client.DBSize(ctx).Result()
}

func (s *Store) FlushDatabase(ctx context.Context) error {
	if s == nil || s.client == nil {
		return errors.New("Redis connection is closed")
	}
	if s.readOnly {
		return errors.New("Redis connection is read-only")
	}
	return s.client.FlushDB(ctx).Err()
}
func (s *Store) ClassifyError(err error) connection.ErrorKind {
	if err == nil {
		return connection.ErrorUnknown
	}
	if kind := connection.ClassifyCommonError(err); kind != connection.ErrorUnknown {
		return kind
	}
	if errors.Is(err, redisclient.Nil) {
		return connection.ErrorNotFound
	}
	message := strings.ToUpper(err.Error())
	if strings.Contains(message, "NOAUTH") || strings.Contains(message, "WRONGPASS") || strings.Contains(message, "NOPERM") {
		return connection.ErrorPermission
	}
	if strings.Contains(message, "LOADING") || strings.Contains(message, "TRYAGAIN") || strings.Contains(message, "BUSY") {
		return connection.ErrorBusy
	}
	var netErr interface{ Timeout() bool }
	if errors.As(err, &netErr) && netErr.Timeout() {
		return connection.ErrorTimeout
	}
	return connection.ErrorUnknown
}
func bounded(n int) int {
	if n < 1 {
		return 100
	}
	if n > maxPageSize {
		return maxPageSize
	}
	return n
}
func (s *Store) ScanKeys(ctx context.Context, match string, cursor uint64, count int) (connection.KeyPage, error) {
	return s.scanKeys(ctx, match, "", cursor, count)
}

func (s *Store) ScanKeysByType(ctx context.Context, match, keyType string, cursor uint64, count int) (connection.KeyPage, error) {
	if keyType != "" {
		switch keyType {
		case "string", "hash", "list", "set", "zset", "stream":
		default:
			return connection.KeyPage{}, errors.New("unsupported Redis key type filter")
		}
	}
	return s.scanKeys(ctx, match, keyType, cursor, count)
}

func (s *Store) scanKeys(ctx context.Context, match, keyType string, cursor uint64, count int) (connection.KeyPage, error) {
	if s == nil || s.client == nil {
		return connection.KeyPage{}, errors.New("Redis connection is closed")
	}
	var keys []string
	var next uint64
	var err error
	if keyType == "" {
		keys, next, err = s.client.Scan(ctx, cursor, match, int64(bounded(count))).Result()
	} else {
		keys, next, err = s.client.ScanType(ctx, cursor, match, int64(bounded(count)), keyType).Result()
	}
	if err != nil {
		return connection.KeyPage{}, err
	}
	return connection.KeyPage{Keys: keys, NextCursor: next}, nil
}
func (s *Store) InspectKey(ctx context.Context, key string) (connection.KeyInfo, error) {
	if s == nil || s.client == nil {
		return connection.KeyInfo{}, errors.New("Redis connection is closed")
	}
	typ, err := s.client.Type(ctx, key).Result()
	if err != nil {
		return connection.KeyInfo{}, err
	}
	if typ == "none" {
		return connection.KeyInfo{}, redisclient.Nil
	}
	typ = normalizedRedisType(typ)
	ttl, err := s.client.TTL(ctx, key).Result()
	if err != nil {
		return connection.KeyInfo{}, err
	}
	length, err := s.client.Do(ctx, "MEMORY", "USAGE", key).Int64()
	if err != nil {
		if ctx.Err() != nil {
			return connection.KeyInfo{}, ctx.Err()
		}
		length = -1
	}
	return connection.KeyInfo{Key: key, Type: typ, TTL: ttl, Length: length}, nil
}

func (s *Store) InspectKeys(ctx context.Context, keys []string) ([]connection.KeyInfo, error) {
	if s == nil || s.client == nil {
		return nil, errors.New("Redis connection is closed")
	}
	if len(keys) == 0 {
		return nil, nil
	}
	pipe := s.client.Pipeline()
	typeCommands := make([]*redisclient.StatusCmd, len(keys))
	ttlCommands := make([]*redisclient.DurationCmd, len(keys))
	for i, key := range keys {
		typeCommands[i] = pipe.Type(ctx, key)
		ttlCommands[i] = pipe.TTL(ctx, key)
	}
	_, execErr := pipe.Exec(ctx)
	if execErr != nil && !errors.Is(execErr, redisclient.Nil) {
		return nil, execErr
	}
	infos := make([]connection.KeyInfo, 0, len(keys))
	for i, key := range keys {
		typ, typeErr := typeCommands[i].Result()
		if typeErr != nil && !errors.Is(typeErr, redisclient.Nil) {
			return infos, typeErr
		}
		ttl, ttlErr := ttlCommands[i].Result()
		if ttlErr != nil && !errors.Is(ttlErr, redisclient.Nil) {
			return infos, ttlErr
		}
		infos = append(infos, connection.KeyInfo{Key: key, Type: normalizedRedisType(typ), TTL: ttl})
	}
	return infos, nil
}

func normalizedRedisType(typ string) string {
	if strings.HasPrefix(typ, "ReJSON-") {
		return "json"
	}
	return typ
}

func (s *Store) CheckJSONSupport(ctx context.Context) error {
	if s == nil || s.client == nil {
		return errors.New("Redis connection is closed")
	}
	return s.client.Do(ctx, "JSON.GET", "__dbl_json_support_probe_9f742f6c__").Err()
}
func (s *Store) ReadKey(ctx context.Context, key, token string, count int) (connection.KeyValuePage, error) {
	info, err := s.InspectKey(ctx, key)
	if err != nil {
		return connection.KeyValuePage{}, err
	}
	count = bounded(count)
	page := connection.KeyValuePage{Type: info.Type}
	cursor, err := strconv.ParseUint(token, 10, 64)
	if token == "" {
		cursor = 0
	} else if err != nil && info.Type != "stream" {
		return page, errors.New("invalid Redis value cursor")
	}
	switch info.Type {
	case "string":
		length, err := s.client.StrLen(ctx, key).Result()
		if err != nil {
			return page, err
		}
		if length > 0 {
			end := length - 1
			if end >= maxStringBytes {
				end = maxStringBytes - 1
				page.StringValueTruncated = true
			}
			value, err := s.client.GetRange(ctx, key, 0, end).Result()
			if err != nil {
				return page, err
			}
			page.StringValue = value
		}
	case "json":
		value, err := s.client.Do(ctx, "JSON.GET", key).Text()
		if err != nil && !errors.Is(err, redisclient.Nil) {
			return page, err
		}
		page.StringValue, page.StringValueTruncated = clipRedisValue(value)
	case "hash":
		values, next, err := s.client.HScan(ctx, key, cursor, "*", int64(count)).Result()
		if err != nil {
			return page, err
		}
		for i := 0; i+1 < len(values); i += 2 {
			field, fieldCut := clipRedisValue(values[i])
			value, valueCut := clipRedisValue(values[i+1])
			page.Entries = append(page.Entries, connection.KeyValueEntry{Field: field, Value: value, Truncated: fieldCut || valueCut})
		}
		if next != 0 {
			page.NextCursor = strconv.FormatUint(next, 10)
		}
	case "set":
		values, next, err := s.client.SScan(ctx, key, cursor, "*", int64(count)).Result()
		if err != nil {
			return page, err
		}
		for _, v := range values {
			value, cut := clipRedisValue(v)
			page.Entries = append(page.Entries, connection.KeyValueEntry{Value: value, Truncated: cut})
		}
		if next != 0 {
			page.NextCursor = strconv.FormatUint(next, 10)
		}
	case "zset":
		values, next, err := s.client.ZScan(ctx, key, cursor, "*", int64(count)).Result()
		if err != nil {
			return page, err
		}
		for i := 0; i+1 < len(values); i += 2 {
			var score float64
			_, _ = fmt.Sscan(values[i+1], &score)
			value, cut := clipRedisValue(values[i])
			page.Entries = append(page.Entries, connection.KeyValueEntry{Value: value, Score: score, Truncated: cut})
		}
		if next != 0 {
			page.NextCursor = strconv.FormatUint(next, 10)
		}
	case "list":
		start := int64(cursor)
		values, err := s.client.LRange(ctx, key, start, start+int64(count)-1).Result()
		if err != nil {
			return page, err
		}
		for _, v := range values {
			value, cut := clipRedisValue(v)
			page.Entries = append(page.Entries, connection.KeyValueEntry{Value: value, Truncated: cut})
		}
		if len(values) == count {
			page.NextCursor = strconv.FormatInt(start+int64(count), 10)
		}
	case "stream":
		start := "+"
		if token != "" {
			start = "(" + token
		}
		values, err := s.client.XRevRangeN(ctx, key, start, "-", int64(count)).Result()
		if err != nil {
			return page, err
		}
		for _, v := range values {
			value, cut := clipRedisValue(fmt.Sprint(v.Values))
			page.Entries = append(page.Entries, connection.KeyValueEntry{Field: v.ID, Value: value, Truncated: cut})
		}
		if len(values) == count {
			page.NextCursor = values[len(values)-1].ID
		}
	default:
		return page, fmt.Errorf("unsupported Redis key type %q", info.Type)
	}
	return page, nil
}

func clipRedisValue(value string) (string, bool) {
	if len(value) <= maxStringBytes {
		return value, false
	}
	return value[:maxStringBytes], true
}
func (s *Store) WriteKey(ctx context.Context, mutation connection.KeyMutation) error {
	if s == nil || s.client == nil {
		return errors.New("Redis connection is closed")
	}
	if s.readOnly {
		return errors.New("Redis profile is read-only")
	}
	if mutation.Key == "" {
		return errors.New("Redis key is required")
	}
	if len(mutation.Value) > maxStringBytes || len(mutation.Field) > maxStringBytes {
		return errors.New("Redis value exceeds 1 MiB limit")
	}
	switch mutation.Type {
	case "string":
		if mutation.PreserveTTL {
			return s.client.SetArgs(ctx, mutation.Key, mutation.Value, redisclient.SetArgs{KeepTTL: true}).Err()
		}
		return s.client.Set(ctx, mutation.Key, mutation.Value, mutation.TTL).Err()
	case "json":
		if !json.Valid([]byte(mutation.Value)) {
			return errors.New("Redis JSON value is not valid JSON")
		}
		if err := s.client.Do(ctx, "JSON.SET", mutation.Key, ".", mutation.Value).Err(); err != nil {
			return err
		}
	case "hash":
		if mutation.Field == "" {
			return errors.New("Redis hash field is required")
		}
		if err := s.client.HSet(ctx, mutation.Key, mutation.Field, mutation.Value).Err(); err != nil {
			return err
		}
	case "list":
		if err := s.client.RPush(ctx, mutation.Key, mutation.Value).Err(); err != nil {
			return err
		}
	case "set":
		if err := s.client.SAdd(ctx, mutation.Key, mutation.Value).Err(); err != nil {
			return err
		}
	case "zset":
		if err := s.client.ZAdd(ctx, mutation.Key, redisclient.Z{Score: mutation.Score, Member: mutation.Value}).Err(); err != nil {
			return err
		}
	case "stream":
		if mutation.Field == "" {
			return errors.New("Redis stream field is required")
		}
		id := mutation.ID
		if id == "" {
			id = "*"
		}
		if err := s.client.XAdd(ctx, &redisclient.XAddArgs{Stream: mutation.Key, ID: id, Values: map[string]any{mutation.Field: mutation.Value}}).Err(); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unsupported Redis write type %q", mutation.Type)
	}
	if mutation.TTL > 0 {
		return s.client.Expire(ctx, mutation.Key, mutation.TTL).Err()
	}
	return nil
}

func (s *Store) WriteKeyBatch(ctx context.Context, mutations []connection.KeyMutation) error {
	if s == nil || s.client == nil {
		return errors.New("Redis connection is closed")
	}
	if s.readOnly {
		return errors.New("Redis profile is read-only")
	}
	if len(mutations) == 0 {
		return errors.New("Redis key has no values")
	}
	key, kind := mutations[0].Key, mutations[0].Type
	if key == "" {
		return errors.New("Redis key is required")
	}
	for _, mutation := range mutations {
		if mutation.Key != key || mutation.Type != kind {
			return errors.New("Redis key batch must have one key and one data type")
		}
		if len(mutation.Value) > maxStringBytes || len(mutation.Field) > maxStringBytes {
			return errors.New("Redis value exceeds 1 MiB limit")
		}
	}
	if (kind == "string" || kind == "json") && len(mutations) != 1 {
		return errors.New("Redis string and JSON keys accept one value")
	}
	for _, mutation := range mutations {
		switch kind {
		case "string":
		case "json":
			if !json.Valid([]byte(mutation.Value)) {
				return errors.New("Redis JSON value is not valid JSON")
			}
		case "hash", "stream":
			if mutation.Field == "" {
				return errors.New("Redis hash and stream entries require a field name")
			}
		case "list", "set":
		case "zset":
			if math.IsNaN(mutation.Score) || math.IsInf(mutation.Score, 0) {
				return errors.New("Redis sorted set score must be a finite number")
			}
		default:
			return fmt.Errorf("unsupported Redis write type %q", kind)
		}
	}
	pipe := s.client.TxPipeline()
	switch kind {
	case "string":
		mutation := mutations[0]
		pipe.Set(ctx, key, mutation.Value, 0)
	case "json":
		mutation := mutations[0]
		pipe.Do(ctx, "JSON.SET", key, ".", mutation.Value)
	case "hash":
		values := make([]any, 0, len(mutations)*2)
		for _, mutation := range mutations {
			values = append(values, mutation.Field, mutation.Value)
		}
		pipe.HSet(ctx, key, values...)
	case "list":
		values := make([]any, 0, len(mutations))
		for _, mutation := range mutations {
			values = append(values, mutation.Value)
		}
		pipe.RPush(ctx, key, values...)
	case "set":
		values := make([]any, 0, len(mutations))
		for _, mutation := range mutations {
			values = append(values, mutation.Value)
		}
		pipe.SAdd(ctx, key, values...)
	case "zset":
		members := make([]redisclient.Z, 0, len(mutations))
		for _, mutation := range mutations {
			members = append(members, redisclient.Z{Score: mutation.Score, Member: mutation.Value})
		}
		pipe.ZAdd(ctx, key, members...)
	case "stream":
		values := make(map[string]any, len(mutations))
		for _, mutation := range mutations {
			values[mutation.Field] = mutation.Value
		}
		id := mutations[0].ID
		if id == "" {
			id = "*"
		}
		pipe.XAdd(ctx, &redisclient.XAddArgs{Stream: key, ID: id, Values: values})
	}
	if ttl := mutations[0].TTL; ttl > 0 {
		pipe.Expire(ctx, key, ttl)
	}
	_, err := pipe.Exec(ctx)
	return err
}

func (s *Store) DeleteKey(ctx context.Context, key string) error {
	if s == nil || s.client == nil {
		return errors.New("Redis connection is closed")
	}
	if s.readOnly {
		return errors.New("Redis profile is read-only")
	}
	return s.client.Del(ctx, key).Err()
}

func (s *Store) DeleteRedisKeys(ctx context.Context, keys []string) error {
	if s == nil || s.client == nil {
		return errors.New("Redis connection is closed")
	}
	if s.readOnly {
		return errors.New("Redis profile is read-only")
	}
	if len(keys) == 0 {
		return nil
	}
	return s.client.Del(ctx, keys...).Err()
}
