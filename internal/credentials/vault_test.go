package credentials

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

type memoryBackend struct {
	values          map[string][]byte
	setErr          error
	storeThenSetErr bool
	setCount        int
	setFailAt       int
	getErr          error
	deleteErr       error
	account         string
}

func newMemoryBackend() *memoryBackend {
	return &memoryBackend{values: make(map[string][]byte)}
}

func (backend *memoryBackend) Set(account string, secret []byte) error {
	backend.setCount++
	backend.account = account
	backend.values[account] = append([]byte(nil), secret...)
	if backend.setErr != nil && (backend.setFailAt == 0 || backend.setCount == backend.setFailAt) {
		if !backend.storeThenSetErr {
			delete(backend.values, account)
		}
		return backend.setErr
	}
	return nil
}

func (backend *memoryBackend) Get(account string) ([]byte, error) {
	backend.account = account
	if backend.getErr != nil {
		return nil, backend.getErr
	}
	secret, ok := backend.values[account]
	if !ok {
		return nil, ErrNotFound
	}
	return append([]byte(nil), secret...), nil
}

func (backend *memoryBackend) Delete(account string) error {
	backend.account = account
	if backend.deleteErr != nil {
		return backend.deleteErr
	}
	if _, ok := backend.values[account]; !ok {
		return ErrNotFound
	}
	delete(backend.values, account)
	return nil
}

func TestVaultStoresAndLoadsByOpaqueReference(t *testing.T) {
	backend := newMemoryBackend()
	vault := New(backend)
	secret := []byte("test-token-value")
	ref, err := vault.Store(secret)
	if err != nil {
		t.Fatal(err)
	}
	if ref == "" || strings.Contains(string(ref), string(secret)) {
		t.Fatalf("credential reference is not opaque: %q", ref)
	}
	if backend.account != string(ref) {
		t.Fatalf("backend key = %q, want credential reference", backend.account)
	}
	loaded, err := vault.Load(ref)
	if err != nil {
		t.Fatal(err)
	}
	if string(loaded) != string(secret) {
		t.Fatalf("loaded secret = %q, want stored secret", loaded)
	}
	if err := vault.Delete(ref); err != nil {
		t.Fatal(err)
	}
	if _, err := vault.Load(ref); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Load after Delete error = %v, want ErrNotFound", err)
	}
	if err := vault.Delete(ref); err != nil {
		t.Fatalf("deleting absent credential should be idempotent: %v", err)
	}
}

func TestVaultValidatesInputAndReferences(t *testing.T) {
	vault := New(newMemoryBackend())
	if _, err := vault.Store(nil); !errors.Is(err, ErrEmptySecret) {
		t.Fatalf("empty secret error = %v", err)
	}
	if _, err := vault.Store(make([]byte, MaxSecretSize+1)); !errors.Is(err, ErrSecretTooLarge) {
		t.Fatalf("oversized secret error = %v", err)
	}
	for _, ref := range []Reference{"", "token", "v1:xyz", "v1:0000"} {
		if _, err := vault.Load(ref); !errors.Is(err, ErrInvalidReference) {
			t.Errorf("Load(%q) error = %v, want ErrInvalidReference", ref, err)
		}
		if err := vault.Delete(ref); !errors.Is(err, ErrInvalidReference) {
			t.Errorf("Delete(%q) error = %v, want ErrInvalidReference", ref, err)
		}
	}
}

func TestVaultChunksLargeSecretsAndKeepsLegacyReferences(t *testing.T) {
	backend := newMemoryBackend()
	vault := New(backend)
	legacy, err := vault.Store([]byte("short password"))
	if err != nil || !strings.HasPrefix(string(legacy), "v1:") {
		t.Fatalf("short secret ref/error = %q/%v, want v1 reference", legacy, err)
	}
	large := make([]byte, MaxSecretSize)
	for i := range large {
		large[i] = byte((i*31 + 7) % 251)
	}
	ref, err := vault.Store(large)
	if err != nil || !strings.HasPrefix(string(ref), "v2:") || !ref.Valid() {
		t.Fatalf("large secret ref/error = %q/%v, want v2 reference", ref, err)
	}
	for account, item := range backend.values {
		if len(item) > maxSecretChunkSize {
			t.Fatalf("vault item %q size = %d, exceeds backend limit %d", account, len(item), maxSecretChunkSize)
		}
	}
	loaded, err := vault.Load(ref)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(loaded, large) {
		t.Fatal("large secret did not round-trip exactly")
	}
	if err := vault.Delete(ref); err != nil {
		t.Fatal(err)
	}
	if _, err := vault.Load(ref); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Load after chunked Delete = %v, want ErrNotFound", err)
	}
	if len(backend.values) != 1 {
		t.Fatalf("remaining backend items = %d, want only legacy secret", len(backend.values))
	}
}

func TestVaultChunkStoreFailureCleansUnreachableItems(t *testing.T) {
	backend := newMemoryBackend()
	backend.setErr = errors.New("late failure")
	backend.storeThenSetErr = true
	backend.setFailAt = 2
	secret := make([]byte, maxSecretChunkSize*2+3)
	if _, err := New(backend).Store(secret); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("Store error = %v, want ErrUnavailable", err)
	}
	if len(backend.values) != 0 {
		t.Fatalf("backend retained %d items after failed chunked Store", len(backend.values))
	}
}

func TestVaultChunkLoadRejectsMissingOrCorruptChunks(t *testing.T) {
	backend := newMemoryBackend()
	vault := New(backend)
	secret := make([]byte, maxSecretChunkSize+8)
	for i := range secret {
		secret[i] = byte(i % 251)
	}
	ref, err := vault.Store(secret)
	if err != nil {
		t.Fatal(err)
	}
	account := string(ref)
	delete(backend.values, chunkAccount(account, 0))
	if _, err := vault.Load(ref); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("Load with missing chunk = %v, want ErrUnavailable", err)
	}
	if err := vault.Delete(ref); err != nil {
		t.Fatal(err)
	}
	ref, err = vault.Store(secret)
	if err != nil {
		t.Fatal(err)
	}
	account = string(ref)
	backend.values[chunkAccount(account, 0)][0] ^= 0xff
	if _, err := vault.Load(ref); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("Load with digest-mismatched chunk = %v, want ErrUnavailable", err)
	}
	if err := vault.Delete(ref); err != nil {
		t.Fatal(err)
	}
}

func TestVaultDoesNotExposeBackendErrorsOrSecret(t *testing.T) {
	const secret = "never-print-this-token"
	backend := newMemoryBackend()
	backend.setErr = fmt.Errorf("backend rejected %s", secret)
	vault := New(backend)
	if _, err := vault.Store([]byte(secret)); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("Store error = %v, want ErrUnavailable", err)
	} else if strings.Contains(err.Error(), secret) {
		t.Fatalf("Store error leaked the secret: %v", err)
	}

	backend.getErr = fmt.Errorf("backend rejected %s", secret)
	if _, err := vault.Load("v1:00112233445566778899aabbccddeeff"); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("Load error = %v, want ErrUnavailable", err)
	} else if strings.Contains(err.Error(), secret) {
		t.Fatalf("Load error leaked the secret: %v", err)
	}
}

func TestVaultCleansUpItemWhenBackendSetFailsLate(t *testing.T) {
	backend := newMemoryBackend()
	backend.setErr = errors.New("late backend failure")
	backend.storeThenSetErr = true
	if _, err := New(backend).Store([]byte("token")); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("Store error = %v, want ErrUnavailable", err)
	}
	if len(backend.values) != 0 {
		t.Fatalf("backend retained unreachable credential after failed Store: %d entries", len(backend.values))
	}
}

func TestVaultUnavailableBackendFailsClosed(t *testing.T) {
	var vault *Vault
	if _, err := vault.Store([]byte("token")); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("Store on nil vault = %v, want ErrUnavailable", err)
	}
	if _, err := vault.Load("v1:00112233445566778899aabbccddeeff"); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("Load on nil vault = %v, want ErrUnavailable", err)
	}
	if err := vault.Delete("v1:00112233445566778899aabbccddeeff"); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("Delete on nil vault = %v, want ErrUnavailable", err)
	}
}
