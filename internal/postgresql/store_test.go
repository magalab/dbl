package postgresql

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/magalab/dbl/internal/connection"
	"github.com/magalab/dbl/internal/credentials"
	"github.com/magalab/dbl/internal/preferences"
)

func TestBuildConfigUsesStrictTLSAndProfileSettings(t *testing.T) {
	profile := preferences.ConnectionProfile{
		Driver: preferences.DriverPostgreSQL, Host: "db.example.test", Database: "analytics",
		Username: "reader", ReadOnly: true,
	}
	config, err := buildConfig(profile, "example-secret")
	if err != nil {
		t.Fatal(err)
	}
	if config.Host != profile.Host || config.Port != defaultPort || config.Database != profile.Database || config.User != profile.Username || config.Password != "example-secret" {
		t.Fatalf("connection config does not match profile: host=%q port=%d database=%q user=%q", config.Host, config.Port, config.Database, config.User)
	}
	if config.TLSConfig == nil || config.TLSConfig.RootCAs == nil || config.TLSConfig.ServerName != profile.Host {
		t.Fatalf("PostgreSQL TLS config does not use verified system roots and host name: %#v", config.TLSConfig)
	}
	if config.RuntimeParams["default_transaction_read_only"] != "on" {
		t.Fatalf("read-only profile runtime parameter = %q", config.RuntimeParams["default_transaction_read_only"])
	}
	if config.RuntimeParams["application_name"] != "DBL" {
		t.Fatalf("application_name = %q", config.RuntimeParams["application_name"])
	}
}

func TestQuoteSearchPathIdentifier(t *testing.T) {
	for _, test := range []struct{ input, want string }{
		{"public", `"public"`},
		{`schema"; DROP SCHEMA public; --`, `"schema""; DROP SCHEMA public; --"`},
	} {
		if got := quoteSearchPathIdentifier(test.input); got != test.want {
			t.Errorf("quoteSearchPathIdentifier(%q) = %q, want %q", test.input, got, test.want)
		}
	}
}

type memoryVault struct {
	mu      sync.Mutex
	secrets map[string][]byte
}

func (vault *memoryVault) Set(account string, secret []byte) error {
	vault.mu.Lock()
	defer vault.mu.Unlock()
	if vault.secrets == nil {
		vault.secrets = make(map[string][]byte)
	}
	vault.secrets[account] = append([]byte(nil), secret...)
	return nil
}

func (vault *memoryVault) Get(account string) ([]byte, error) {
	vault.mu.Lock()
	defer vault.mu.Unlock()
	secret, ok := vault.secrets[account]
	if !ok {
		return nil, credentials.ErrNotFound
	}
	return append([]byte(nil), secret...), nil
}

func (vault *memoryVault) Delete(account string) error {
	vault.mu.Lock()
	defer vault.mu.Unlock()
	delete(vault.secrets, account)
	return nil
}

func TestConnectorLoadsVaultSecretAndRedactsConnectionFailure(t *testing.T) {
	backend := &memoryVault{}
	vault := credentials.New(backend)
	secret := []byte("pg-test-secret")
	ref, err := vault.Store(secret)
	if err != nil {
		t.Fatal(err)
	}
	profile := preferences.ConnectionProfile{
		Driver: preferences.DriverPostgreSQL, Host: "127.0.0.1", Port: 1,
		Username: "test", CredentialRef: string(ref),
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, err = (Connector{Profile: profile, Vault: vault}).Connect(ctx)
	if err == nil {
		t.Fatal("connection unexpectedly succeeded on closed localhost port")
	}
	if strings.Contains(err.Error(), string(secret)) {
		t.Fatalf("connection error exposed password: %q", err)
	}
}

func TestBuildConfigRejectsInvalidPort(t *testing.T) {
	profile := preferences.ConnectionProfile{Driver: preferences.DriverPostgreSQL, Host: "localhost", Port: 65536}
	if _, err := buildConfig(profile, ""); err == nil {
		t.Fatal("invalid PostgreSQL port was accepted")
	}
}

func TestClassifyPostgreSQLErrorsBySQLState(t *testing.T) {
	store := &Store{}
	tests := []struct {
		code string
		want connection.ErrorKind
	}{
		{code: "57014", want: connection.ErrorCanceled},
		{code: "42601", want: connection.ErrorQuery},
		{code: "3F000", want: connection.ErrorQuery},
		{code: "42501", want: connection.ErrorPermission},
		{code: "40001", want: connection.ErrorBusy},
		{code: "40P01", want: connection.ErrorBusy},
		{code: "08006", want: connection.ErrorUnknown},
	}
	for _, test := range tests {
		t.Run(test.code, func(t *testing.T) {
			got := store.ClassifyError(&pgconn.PgError{Code: test.code})
			if got != test.want {
				t.Fatalf("ClassifyError(%s) = %v, want %v", test.code, got, test.want)
			}
		})
	}
}

func TestRedactErrorDoesNotExposePassword(t *testing.T) {
	secret := "top-secret"
	err := redactError("connect", errors.New("dial failed using password="+secret), secret)
	if strings.Contains(err.Error(), secret) || !strings.Contains(err.Error(), "[redacted]") {
		t.Fatalf("redacted error = %q", err)
	}
}
