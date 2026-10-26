package redis

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/magalab/dbl/internal/connection"
	"github.com/magalab/dbl/internal/credentials"
	"github.com/magalab/dbl/internal/preferences"
	redisclient "github.com/redis/go-redis/v9"
)

type memoryVaultBackend map[string][]byte

func (backend memoryVaultBackend) Set(account string, secret []byte) error {
	backend[account] = append([]byte(nil), secret...)
	return nil
}
func (backend memoryVaultBackend) Get(account string) ([]byte, error) {
	value, ok := backend[account]
	if !ok {
		return nil, credentials.ErrNotFound
	}
	return append([]byte(nil), value...), nil
}
func (backend memoryVaultBackend) Delete(account string) error {
	if _, ok := backend[account]; !ok {
		return credentials.ErrNotFound
	}
	delete(backend, account)
	return nil
}

func TestRedisIntegration(t *testing.T) {
	address := strings.TrimSpace(os.Getenv("DBL_TEST_REDIS_ADDR"))
	if address == "" {
		t.Skip("set DBL_TEST_REDIS_ADDR to run the optional Redis integration test")
	}
	host, portText, err := net.SplitHostPort(address)
	if err != nil {
		t.Fatalf("parse Redis address: %v", err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatalf("parse Redis port: %v", err)
	}
	db := 0
	if value := os.Getenv("DBL_TEST_REDIS_DB"); value != "" {
		db, err = strconv.Atoi(value)
		if err != nil {
			t.Fatalf("parse Redis DB: %v", err)
		}
	}
	profile := preferences.ConnectionProfile{Driver: preferences.DriverRedis, Host: host, Port: port, RedisDB: db, Username: os.Getenv("DBL_TEST_REDIS_USERNAME"), TLSMode: os.Getenv("DBL_TEST_REDIS_TLS_MODE")}
	if profile.TLSMode == "" {
		profile.TLSMode = "disable"
	}
	backend := memoryVaultBackend{}
	vault := credentials.New(backend)
	if password := os.Getenv("DBL_TEST_REDIS_PASSWORD"); password != "" {
		ref, err := vault.Store([]byte(password))
		if err != nil {
			t.Fatal(err)
		}
		profile.CredentialRef = string(ref)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	adapter, err := (Connector{Profile: profile, Vault: vault}).Connect(ctx)
	if err != nil {
		t.Fatal(err)
	}
	store := adapter.(*Store)
	key := fmt.Sprintf("dbl_it_%d:%s", time.Now().UnixNano(), strings.ReplaceAll(t.Name(), "/", "_"))
	t.Cleanup(func() {
		cleanupCtx, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		if err := store.DeleteKey(cleanupCtx, key); err != nil {
			t.Errorf("clean up Redis integration key: %v", err)
		}
		if err := adapter.Close(); err != nil {
			t.Errorf("close Redis integration connection: %v", err)
		}
	})
	if err := store.WriteKey(ctx, connectionMutation(key)); err != nil {
		t.Fatal(err)
	}
	info, err := store.InspectKey(ctx, key)
	if err != nil {
		t.Fatal(err)
	}
	if info.Type != "string" || info.TTL <= 0 {
		t.Fatalf("key metadata = %#v, want string with positive TTL", info)
	}
	page, err := store.ScanKeys(ctx, key[:strings.IndexByte(key, ':')+1]+"*", 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, item := range page.Keys {
		if item == key {
			found = true
		}
	}
	if !found {
		t.Fatalf("SCAN did not return integration key %q", key)
	}
	typedPage, err := store.ScanKeysByType(ctx, key[:strings.IndexByte(key, ':')+1]+"*", "string", 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	found = false
	for _, item := range typedPage.Keys {
		if item == key {
			found = true
		}
	}
	if !found {
		t.Fatalf("SCAN TYPE did not return integration string key %q", key)
	}
	if _, err := store.ScanKeysByType(ctx, "*", "unknown", 0, 100); err == nil {
		t.Fatal("SCAN TYPE accepted an unsupported key type")
	}
	value, err := store.ReadKey(ctx, key, "", 10)
	if err != nil {
		t.Fatal(err)
	}
	if value.StringValue != "integration" {
		t.Fatalf("value = %q", value.StringValue)
	}
	if err := store.DeleteKey(ctx, key); err != nil {
		t.Fatal(err)
	}
	if _, err := store.InspectKey(ctx, key); !errors.Is(err, redisclient.Nil) {
		t.Fatalf("post-delete inspection error = %v", err)
	}
}

func connectionMutation(key string) connection.KeyMutation {
	return connection.KeyMutation{Key: key, Type: "string", Value: "integration", TTL: time.Minute}
}
