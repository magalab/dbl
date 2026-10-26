package etcd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/magalab/dbl/internal/connection"
	"github.com/magalab/dbl/internal/credentials"
	"github.com/magalab/dbl/internal/preferences"
	"go.etcd.io/etcd/api/v3/v3rpc/rpctypes"
	clientv3 "go.etcd.io/etcd/client/v3"
)

type integrationVaultBackend map[string][]byte

func (backend integrationVaultBackend) Set(account string, secret []byte) error {
	backend[account] = append([]byte(nil), secret...)
	return nil
}
func (backend integrationVaultBackend) Get(account string) ([]byte, error) {
	secret, ok := backend[account]
	if !ok {
		return nil, credentials.ErrNotFound
	}
	return append([]byte(nil), secret...), nil
}
func (backend integrationVaultBackend) Delete(account string) error {
	if _, ok := backend[account]; !ok {
		return credentials.ErrNotFound
	}
	delete(backend, account)
	return nil
}

func TestEtcdIntegration(t *testing.T) {
	endpoint := strings.TrimSpace(os.Getenv("DBL_TEST_ETCD_ENDPOINT"))
	if endpoint == "" {
		t.Skip("set DBL_TEST_ETCD_ENDPOINT to run the optional etcd integration test")
	}
	tlsMode := "verify-full"
	if strings.HasPrefix(endpoint, "http://") {
		tlsMode = "disable"
	}
	vault := credentials.New(integrationVaultBackend{})
	profile := preferences.ConnectionProfile{
		ID: "etcd-integration", Name: "integration", Driver: preferences.DriverEtcd,
		Host: endpoint, TLSMode: tlsMode, TLSCAPath: strings.TrimSpace(os.Getenv("DBL_TEST_ETCD_CA")),
		Username: os.Getenv("DBL_TEST_ETCD_USERNAME"),
	}
	if password := []byte(os.Getenv("DBL_TEST_ETCD_PASSWORD")); len(password) > 0 {
		ref, err := vault.Store(password)
		clear(password)
		if err != nil {
			t.Fatal(err)
		}
		profile.CredentialRef = string(ref)
	}
	for _, item := range []struct {
		path string
		ref  *string
	}{{os.Getenv("DBL_TEST_ETCD_CLIENT_CERT"), &profile.TLSCertRef}, {os.Getenv("DBL_TEST_ETCD_CLIENT_KEY"), &profile.TLSKeyRef}} {
		if item.path == "" {
			continue
		}
		secret, err := os.ReadFile(item.path)
		if err != nil {
			t.Fatalf("read configured etcd test credential: %v", err)
		}
		ref, err := vault.Store(secret)
		clear(secret)
		if err != nil {
			t.Fatal(err)
		}
		*item.ref = string(ref)
	}
	if (profile.TLSCertRef == "") != (profile.TLSKeyRef == "") {
		t.Fatal("DBL_TEST_ETCD_CLIENT_CERT and DBL_TEST_ETCD_CLIENT_KEY must be set together")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	active, err := (Connector{Profile: profile, Vault: vault}).Connect(ctx)
	if err != nil {
		t.Fatalf("connect to etcd integration server: %v", err)
	}
	store := active.(*Store)
	defer store.Close()

	prefix := fmt.Sprintf("/dbl-integration/%d/", time.Now().UnixNano())
	otherKey := strings.TrimSuffix(prefix, "/") + "-outside-prefix"
	for i, key := range []string{prefix + "a", prefix + "b", prefix + "c"} {
		if _, err := store.PutKey(ctx, key, fmt.Sprintf("value-%d", i), 0); err != nil {
			t.Fatalf("put integration key: %v", err)
		}
	}
	if _, err := store.PutKey(ctx, otherKey, "keep", 0); err != nil {
		t.Fatalf("put boundary key: %v", err)
	}
	t.Cleanup(func() {
		cleanup, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		_, _ = store.DeleteKeys(cleanup, prefix, true)
		_, _ = store.DeleteKeys(cleanup, otherKey, false)
	})

	var pageKeys []string
	start := ""
	for pageNumber := 0; pageNumber < 4; pageNumber++ {
		page, err := store.RangeKeys(ctx, prefix, true, start, 1)
		if err != nil {
			t.Fatalf("read etcd key page: %v", err)
		}
		for _, entry := range page.Entries {
			pageKeys = append(pageKeys, entry.Key)
		}
		if !page.HasMore {
			break
		}
		if page.NextKey == "" || page.NextKey == start {
			t.Fatalf("invalid etcd continuation key %q after %q", page.NextKey, start)
		}
		start = page.NextKey
	}
	if len(pageKeys) != 3 || pageKeys[0] != prefix+"a" || pageKeys[1] != prefix+"b" || pageKeys[2] != prefix+"c" {
		t.Fatalf("paginated keys = %#v", pageKeys)
	}
	if count, err := store.CountKeys(ctx, prefix, true); err != nil || count != 3 {
		t.Fatalf("prefix count = %d, err = %v; want 3", count, err)
	}

	lease, err := store.client.Grant(ctx, 60)
	if err != nil {
		t.Fatalf("grant integration lease: %v", err)
	}
	leased, err := store.PutKey(ctx, prefix+"leased", "temporary", int64(lease.ID))
	if err != nil {
		t.Fatalf("put lease-backed key: %v", err)
	}
	leaseInfo, err := store.LeaseTimeToLive(ctx, leased.Lease)
	if err != nil || leaseInfo.TTL <= 0 || leaseInfo.GrantedTTL != 60 {
		t.Fatalf("lease ttl = %#v, err = %v", leaseInfo, err)
	}
	expiringLease, err := store.client.Grant(ctx, 2)
	if err != nil {
		t.Fatalf("grant expiring integration lease: %v", err)
	}
	expiringKey := prefix + "expires"
	if _, err := store.PutKey(ctx, expiringKey, "temporary", int64(expiringLease.ID)); err != nil {
		t.Fatalf("put expiring lease-backed key: %v", err)
	}
	expiryCtx, stopExpiry := context.WithTimeout(ctx, 10*time.Second)
	defer stopExpiry()
	expiryPoll := time.NewTicker(200 * time.Millisecond)
	defer expiryPoll.Stop()
	for {
		_, err := store.GetKey(expiryCtx, expiringKey)
		if errors.Is(err, rpctypes.ErrKeyNotFound) {
			break
		}
		if err != nil {
			t.Fatalf("wait for lease-backed key expiry: %v", err)
		}
		select {
		case <-expiryPoll.C:
		case <-expiryCtx.Done():
			t.Fatalf("lease-backed key %q did not expire: %v", expiringKey, expiryCtx.Err())
		}
	}

	watchRevision, err := store.client.Get(ctx, prefix, clientv3.WithPrefix())
	if err != nil {
		t.Fatalf("read etcd watch revision: %v", err)
	}
	watchCtx, stopWatch := context.WithCancel(ctx)
	watch := store.Watch(watchCtx, prefix, true, watchRevision.Header.Revision+1)
	watchKey := prefix + "watch"
	if _, err := store.PutKey(ctx, watchKey, "observed", 0); err != nil {
		stopWatch()
		t.Fatalf("write watched key: %v", err)
	}
	select {
	case event, ok := <-watch:
		if !ok || event.Err != nil || event.Key != watchKey {
			stopWatch()
			t.Fatalf("watch event = %#v, open = %v", event, ok)
		}
	case <-time.After(5 * time.Second):
		stopWatch()
		t.Fatal("timed out waiting for etcd watch event")
	}
	stopWatch()
	select {
	case _, ok := <-watch:
		if ok {
			for range watch {
			}
		}
	case <-time.After(5 * time.Second):
		t.Fatal("etcd watch did not stop after its context was canceled")
	}

	latest, err := store.client.Get(ctx, prefix, clientv3.WithPrefix())
	if err != nil {
		t.Fatalf("read revision before compaction: %v", err)
	}
	if _, err := store.client.Compact(ctx, latest.Header.Revision); err != nil {
		t.Fatalf("compact integration revision: %v", err)
	}
	compactedWatch := store.Watch(ctx, prefix, true, 1)
	select {
	case event, ok := <-compactedWatch:
		if !ok || !errors.Is(event.Err, rpctypes.ErrCompacted) {
			t.Fatalf("watch from compacted revision event = %#v, open = %v; want ErrCompacted", event, ok)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("watch from compacted revision did not report compaction")
	}

	deleted, err := store.DeleteKeys(ctx, prefix, true)
	if err != nil || deleted != 5 {
		t.Fatalf("delete prefix count = %d, err = %v; want 5", deleted, err)
	}
	if _, err := store.GetKey(ctx, otherKey); err != nil {
		t.Fatalf("prefix delete crossed its boundary: %v", err)
	}
	if _, err := store.GetKey(ctx, prefix+"a"); !errors.Is(err, rpctypes.ErrKeyNotFound) {
		t.Fatalf("deleted key lookup error = %v, want not found", err)
	}

	readOnly := *store
	readOnly.readOnly = true
	if _, err := readOnly.PutKey(ctx, prefix+"denied", "x", 0); err == nil {
		t.Fatal("read-only etcd store accepted a write")
	}
	if _, err := readOnly.DeleteKeys(ctx, prefix, true); err == nil {
		t.Fatal("read-only etcd store accepted a delete")
	}
}

var _ connection.Configuration = (*Store)(nil)
