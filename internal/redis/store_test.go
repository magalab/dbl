package redis

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	miniredis "github.com/alicebob/miniredis/v2"
	"github.com/magalab/dbl/internal/connection"
	redisclient "github.com/redis/go-redis/v9"
)

func testStore(t *testing.T) (*Store, *redisclient.Client) {
	t.Helper()
	server := miniredis.RunT(t)
	client := redisclient.NewClient(&redisclient.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	return &Store{client: client}, client
}

func TestScanKeysUsesBoundedCancellableCursor(t *testing.T) {
	store, client := testStore(t)
	for i := 0; i < 20; i++ {
		if err := client.Set(context.Background(), "cache:"+strconv.Itoa(i), "v", 0).Err(); err != nil {
			t.Fatal(err)
		}
	}
	page, err := store.ScanKeys(context.Background(), "cache:*", 0, 7)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Keys) == 0 || len(page.Keys) > 20 {
		t.Fatalf("SCAN page size = %d", len(page.Keys))
	}
	for _, key := range page.Keys {
		if !strings.HasPrefix(key, "cache:") {
			t.Fatalf("unexpected key %q", key)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := store.ScanKeys(ctx, "*", 0, 100); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled SCAN error = %v", err)
	}
	if _, err := store.ScanKeys(context.Background(), "*", 0, maxPageSize+1); err != nil {
		t.Fatal(err)
	}
}

func TestInspectAndReadBoundedRedisTypes(t *testing.T) {
	store, client := testStore(t)
	ctx := context.Background()
	if err := client.Set(ctx, "str", "value", 2*time.Minute).Err(); err != nil {
		t.Fatal(err)
	}
	if err := client.HSet(ctx, "hash", "f1", "v1", "f2", "v2").Err(); err != nil {
		t.Fatal(err)
	}
	if err := client.RPush(ctx, "list", "a", "b").Err(); err != nil {
		t.Fatal(err)
	}
	if err := client.SAdd(ctx, "set", "a", "b").Err(); err != nil {
		t.Fatal(err)
	}
	if err := client.ZAdd(ctx, "zset", redisclient.Z{Score: 2, Member: "b"}, redisclient.Z{Score: 1, Member: "a"}).Err(); err != nil {
		t.Fatal(err)
	}
	if err := client.XAdd(ctx, &redisclient.XAddArgs{Stream: "stream", Values: map[string]any{"f": "v"}}).Err(); err != nil {
		t.Fatal(err)
	}
	for _, keyType := range []string{"string", "hash", "list", "set", "zset", "stream"} {
		key := keyType
		if keyType == "string" {
			key = "str"
		}
		info, err := store.InspectKey(ctx, key)
		if err != nil {
			t.Fatalf("InspectKey(%s): %v", key, err)
		}
		if info.Type != keyType {
			t.Fatalf("%s type = %s", key, info.Type)
		}
		page, err := store.ReadKey(ctx, key, "", 2)
		if err != nil {
			t.Fatalf("ReadKey(%s): %v", key, err)
		}
		if page.Type != keyType {
			t.Fatalf("%s page type = %s", key, page.Type)
		}
		switch keyType {
		case "string":
			if page.StringValue != "value" {
				t.Fatalf("string value = %q", page.StringValue)
			}
		default:
			if len(page.Entries) == 0 {
				t.Fatalf("%s returned no entries", keyType)
			}
		}
	}
	large := strings.Repeat("x", maxStringBytes+128)
	if err := client.Set(ctx, "large", large, 0).Err(); err != nil {
		t.Fatal(err)
	}
	largePage, err := store.ReadKey(ctx, "large", "", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(largePage.StringValue) != maxStringBytes || !largePage.StringValueTruncated {
		t.Fatalf("large string page len=%d truncated=%v", len(largePage.StringValue), largePage.StringValueTruncated)
	}
	if err := client.HSet(ctx, "large-hash", "field", large).Err(); err != nil {
		t.Fatal(err)
	}
	largeHash, err := store.ReadKey(ctx, "large-hash", "", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(largeHash.Entries) != 1 || len(largeHash.Entries[0].Value) != maxStringBytes || !largeHash.Entries[0].Truncated {
		t.Fatalf("large hash entry = %#v", largeHash.Entries)
	}
	if _, err := store.InspectKey(ctx, "missing"); !errors.Is(err, redisclient.Nil) {
		t.Fatalf("missing key error = %v", err)
	}
}

func TestSetDeleteAndReadOnly(t *testing.T) {
	store, client := testStore(t)
	ctx := context.Background()
	if err := store.WriteKey(ctx, connection.KeyMutation{Key: "target", Type: "string", Value: "new", TTL: time.Minute}); err != nil {
		t.Fatal(err)
	}
	value, err := client.Get(ctx, "target").Result()
	if err != nil || value != "new" {
		t.Fatalf("written value = %q, err=%v", value, err)
	}
	if err := store.DeleteKey(ctx, "target"); err != nil {
		t.Fatal(err)
	}
	store.readOnly = true
	if err := store.WriteKey(ctx, connection.KeyMutation{Key: "target", Type: "string", Value: "x"}); err == nil {
		t.Fatal("read-only SET succeeded")
	}
	if err := store.DeleteKey(ctx, "target"); err == nil {
		t.Fatal("read-only DEL succeeded")
	}
	if err := store.WriteKey(ctx, connection.KeyMutation{Key: "big", Type: "string", Value: strings.Repeat("x", maxStringBytes+1)}); err == nil {
		t.Fatal("oversized string succeeded")
	}
	store.readOnly = false
	for _, mutation := range []connection.KeyMutation{
		{Key: "hash2", Type: "hash", Field: "field", Value: "value"},
		{Key: "list2", Type: "list", Value: "value"},
		{Key: "set2", Type: "set", Value: "value"},
		{Key: "zset2", Type: "zset", Value: "member", Score: 3.5},
		{Key: "stream2", Type: "stream", Field: "field", Value: "value"},
	} {
		if err := store.WriteKey(ctx, mutation); err != nil {
			t.Fatalf("WriteKey(%s): %v", mutation.Type, err)
		}
	}
	if store.ClassifyError(context.Canceled) != connection.ErrorCanceled {
		t.Fatal("context cancellation was not classified")
	}
	if store.ClassifyError(errors.New("NOAUTH Authentication required")) != connection.ErrorPermission {
		t.Fatal("authentication error was not classified as permission")
	}
	if store.ClassifyError(nil) != connection.ErrorUnknown {
		t.Fatal("nil error classification was not safe")
	}
}

func TestStreamUsesExclusiveIDCursor(t *testing.T) {
	store, client := testStore(t)
	ctx := context.Background()
	for _, id := range []string{"1-0", "2-0", "3-0", "4-0", "5-0"} {
		if err := client.XAdd(ctx, &redisclient.XAddArgs{Stream: "events", ID: id, Values: map[string]any{"id": id}}).Err(); err != nil {
			t.Fatal(err)
		}
	}
	first, err := store.ReadKey(ctx, "events", "", 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Entries) != 2 || first.NextCursor == "" {
		t.Fatalf("first stream page = %#v", first)
	}
	second, err := store.ReadKey(ctx, "events", first.NextCursor, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Entries) != 2 || second.NextCursor == "" {
		t.Fatalf("second stream page = %#v", second)
	}
	if first.Entries[1].Field == second.Entries[0].Field {
		t.Fatal("stream cursor repeated an entry")
	}
	third, err := store.ReadKey(ctx, "events", second.NextCursor, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(third.Entries) != 1 || third.NextCursor != "" {
		t.Fatalf("third stream page = %#v", third)
	}
}

func TestTLSConfigModes(t *testing.T) {
	full, err := tlsConfig("redis.example.test", "verify-full")
	if err != nil || full.ServerName != "redis.example.test" || full.InsecureSkipVerify {
		t.Fatalf("verify-full config=%#v err=%v", full, err)
	}
	ca, err := tlsConfig("redis.example.test", "verify-ca")
	if err != nil || !ca.InsecureSkipVerify || ca.VerifyConnection == nil {
		t.Fatalf("verify-ca config=%#v err=%v", ca, err)
	}
	required, err := tlsConfig("localhost", "require")
	if err != nil || !required.InsecureSkipVerify {
		t.Fatalf("require config=%#v err=%v", required, err)
	}
	if config, err := tlsConfig("localhost", "disable"); err != nil || config != nil {
		t.Fatalf("disable config=%#v err=%v", config, err)
	}
	if _, err := tlsConfig("localhost", "prefer"); err == nil {
		t.Fatal("prefer TLS mode accepted")
	}
}
