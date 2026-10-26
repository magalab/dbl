package mysql

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	mysqldriver "github.com/go-sql-driver/mysql"
	"github.com/magalab/dbl/internal/connection"
	"github.com/magalab/dbl/internal/credentials"
	"github.com/magalab/dbl/internal/preferences"
)

func TestBuildConfigUsesStrictTLSAndProfileSettings(t *testing.T) {
	profile := preferences.ConnectionProfile{
		Driver: preferences.DriverMySQL, Host: "db.example.test", Database: "analytics",
		Username: "reader",
	}
	config, err := buildConfig(profile, "example-secret")
	if err != nil {
		t.Fatal(err)
	}
	if config.Addr != "db.example.test:3306" || config.DBName != "analytics" || config.User != "reader" || config.Passwd != "example-secret" {
		t.Fatalf("MySQL connection config does not match profile: addr=%q database=%q user=%q", config.Addr, config.DBName, config.User)
	}
	if config.TLS == nil || config.TLS.RootCAs == nil || config.TLS.ServerName != profile.Host {
		t.Fatalf("MySQL TLS config does not use verified system roots and host name: %#v", config.TLS)
	}
	if !config.ParseTime || !strings.Contains(config.FormatDSN(), "charset=utf8mb4") {
		t.Fatalf("MySQL text/time settings = parseTime:%v dsn:%q", config.ParseTime, config.FormatDSN())
	}
}

func TestBuildMySQLTLSModes(t *testing.T) {
	tests := []struct {
		mode           string
		wantTLS        bool
		wantSkipVerify bool
		wantFallback   bool
	}{
		{mode: "verify-full", wantTLS: true},
		{mode: "verify-ca", wantTLS: true, wantSkipVerify: true},
		{mode: "require", wantTLS: true, wantSkipVerify: true},
		{mode: "prefer", wantTLS: true, wantSkipVerify: true, wantFallback: true},
		{mode: "disable"},
	}
	for _, test := range tests {
		t.Run(test.mode, func(t *testing.T) {
			config, err := buildConfig(preferences.ConnectionProfile{
				Driver: preferences.DriverMySQL, Host: "db.example.test", TLSMode: test.mode,
			}, "")
			if err != nil {
				t.Fatal(err)
			}
			if (config.TLS != nil) != test.wantTLS || config.AllowFallbackToPlaintext != test.wantFallback {
				t.Fatalf("TLS config mode=%v fallback=%v", config.TLS != nil, config.AllowFallbackToPlaintext)
			}
			if config.TLS != nil && config.TLS.InsecureSkipVerify != test.wantSkipVerify {
				t.Fatalf("InsecureSkipVerify = %v, want %v", config.TLS.InsecureSkipVerify, test.wantSkipVerify)
			}
			if test.mode == "verify-ca" && config.TLS.VerifyConnection == nil {
				t.Fatal("verify-ca mode does not verify the certificate chain")
			}
		})
	}
	if _, _, err := buildTLSConfig("unknown", "db.example.test"); err == nil {
		t.Fatal("unsupported MySQL TLS mode was accepted")
	}
}

func TestBuildConfigRejectsInvalidPort(t *testing.T) {
	profile := preferences.ConnectionProfile{Driver: preferences.DriverMySQL, Host: "localhost", Port: 65536}
	if _, err := buildConfig(profile, ""); err == nil {
		t.Fatal("invalid MySQL port was accepted")
	}
}

func TestClassifyMySQLErrors(t *testing.T) {
	store := &Store{}
	tests := []struct {
		number   uint16
		sqlState [5]byte
		want     connection.ErrorKind
	}{
		{number: 1317, want: connection.ErrorCanceled},
		{number: 3024, want: connection.ErrorTimeout},
		{number: 1045, want: connection.ErrorPermission},
		{number: 1142, want: connection.ErrorPermission},
		{number: 1049, want: connection.ErrorNotFound},
		{number: 1146, want: connection.ErrorNotFound},
		{number: 1205, want: connection.ErrorBusy},
		{number: 1213, want: connection.ErrorBusy},
		{number: 1064, sqlState: [5]byte{'4', '2', '0', '0', '0'}, want: connection.ErrorQuery},
		{number: 2003, sqlState: [5]byte{'H', 'Y', '0', '0', '0'}, want: connection.ErrorUnknown},
	}
	for _, test := range tests {
		err := &mysqldriver.MySQLError{Number: test.number, SQLState: test.sqlState}
		if got := store.ClassifyError(err); got != test.want {
			t.Errorf("ClassifyError(%d) = %v, want %v", test.number, got, test.want)
		}
	}
}

type memoryVault struct{ secret []byte }

func (vault *memoryVault) Set(_ string, secret []byte) error {
	vault.secret = append([]byte(nil), secret...)
	return nil
}
func (vault *memoryVault) Get(_ string) ([]byte, error) {
	if vault.secret == nil {
		return nil, credentials.ErrNotFound
	}
	return append([]byte(nil), vault.secret...), nil
}
func (vault *memoryVault) Delete(string) error { return nil }

func TestConnectorLoadsVaultSecretAndRedactsConnectionFailure(t *testing.T) {
	secret := []byte("mysql-test-secret")
	vault := credentials.New(&memoryVault{})
	ref, err := vault.Store(secret)
	if err != nil {
		t.Fatal(err)
	}
	profile := preferences.ConnectionProfile{
		Driver: preferences.DriverMySQL, Host: "127.0.0.1", Port: 1,
		Username: "test", CredentialRef: string(ref), TLSMode: "disable",
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

func TestRedactErrorDoesNotExposePassword(t *testing.T) {
	secret := "top-secret"
	err := redactError("connect", errors.New("dial failed using password="+secret), secret)
	if strings.Contains(err.Error(), secret) || !strings.Contains(err.Error(), "[redacted]") {
		t.Fatalf("redacted error = %q", err)
	}
}
