package preferences

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/pelletier/go-toml"
)

func TestRemoteConnectionProfileCRUDCopyAndPersistence(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "dbl")
	store, err := OpenAt(dir)
	if err != nil {
		t.Fatal(err)
	}
	profile, err := store.SaveConnectionProfile(ConnectionProfile{
		Name:          "Analytics",
		Group:         "Development",
		Driver:        DriverPostgreSQL,
		Host:          "db.example.test",
		Database:      "analytics",
		TLSCAPath:     "/etc/dbl/postgres-ca.pem",
		Username:      "reader",
		CredentialRef: "v1:0123456789abcdef0123456789abcdef",
		ReadOnly:      true,
		Favorite:      true,
	}, 1)
	if err != nil {
		t.Fatal(err)
	}
	if profile.Port != 5432 || profile.ID == "" {
		t.Fatalf("created profile defaults = %#v", profile)
	}

	contents, err := os.ReadFile(filepath.Join(dir, "settings.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.ToLower(string(contents)), "password") || strings.Contains(string(contents), "temporary-dbl") {
		t.Fatalf("settings contain secret material: %s", contents)
	}
	var persisted Settings
	if err := toml.NewDecoder(strings.NewReader(string(contents))).SetTagName("json").Decode(&persisted); err != nil {
		t.Fatal(err)
	}
	if len(persisted.ConnectionProfiles) != 1 || !reflect.DeepEqual(persisted.ConnectionProfiles[0], profile) {
		t.Fatalf("persisted profiles = %#v, want %#v", persisted.ConnectionProfiles, profile)
	}

	copied, err := store.CopyConnectionProfile(profile.ID, "Analytics copy", 2)
	if err != nil {
		t.Fatal(err)
	}
	if copied.ID == profile.ID || copied.Name != "Analytics copy" || copied.CredentialRef != profile.CredentialRef || copied.TLSCAPath != profile.TLSCAPath {
		t.Fatalf("copied profile = %#v", copied)
	}

	mysql, err := store.SaveConnectionProfile(ConnectionProfile{
		Name: "Analytics", Driver: DriverMySQL, Host: "mysql.example.test", TLSCAPath: "/etc/dbl/mysql-ca.pem",
	}, 3)
	if err != nil {
		t.Fatalf("same display name on a different driver rejected: %v", err)
	}
	if mysql.Port != 3306 {
		t.Fatalf("MySQL default port = %d, want 3306", mysql.Port)
	}
	if err := store.DeleteConnectionProfile(profile.ID, 4); err != nil {
		t.Fatal(err)
	}
	mongo, err := store.SaveConnectionProfile(ConnectionProfile{
		Name: "Catalog", Driver: DriverMongoDB, Host: "mongo.example.test", TLSCAPath: "/etc/dbl/mongo-ca.pem",
	}, 5)
	if err != nil {
		t.Fatal(err)
	}

	reloaded, err := OpenAt(dir)
	if err != nil {
		t.Fatal(err)
	}
	got := reloaded.Settings().ConnectionProfiles
	if len(got) != 3 || got[0].ID != copied.ID || got[1].ID != mysql.ID || got[2].ID != mongo.ID {
		t.Fatalf("reloaded profiles after delete = %#v", got)
	}
	if got[0].TLSCAPath != "/etc/dbl/postgres-ca.pem" || got[1].TLSCAPath != "/etc/dbl/mysql-ca.pem" || got[2].TLSCAPath != "/etc/dbl/mongo-ca.pem" {
		t.Fatalf("reloaded private CA paths = %q, %q and %q", got[0].TLSCAPath, got[1].TLSCAPath, got[2].TLSCAPath)
	}
}

func TestLegacySQLiteProfilesMigrateToConnectionProfiles(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "dbl")
	legacy := SQLiteProfile{ID: "legacy-id", Name: "Old local", Group: "Local", Path: "/db/old.sqlite", ReadOnly: true}
	if err := writeSettings(dir, Settings{Version: 1, SQLiteProfiles: []SQLiteProfile{legacy}}); err != nil {
		t.Fatal(err)
	}
	store, err := OpenAt(dir)
	if err != nil {
		t.Fatal(err)
	}
	profiles := store.Settings().ConnectionProfiles
	want := connectionProfile(legacy)
	want.Environment = EnvironmentDevelopment
	if len(profiles) != 1 || !reflect.DeepEqual(profiles[0], want) {
		t.Fatalf("migrated profiles = %#v, want %#v", profiles, want)
	}

	if _, err := store.SaveConnectionProfile(ConnectionProfile{
		Name: "Postgres", Driver: DriverPostgreSQL, Host: "localhost",
	}, 1); err != nil {
		t.Fatal(err)
	}
	reloaded, err := OpenAt(dir)
	if err != nil {
		t.Fatal(err)
	}
	profiles = reloaded.Settings().ConnectionProfiles
	if len(profiles) != 2 || profiles[0].ID != legacy.ID || profiles[1].Driver != DriverPostgreSQL {
		t.Fatalf("profiles after migrating and saving remote profile = %#v", profiles)
	}
	legacyProfiles := reloaded.Settings().SQLiteProfiles
	if len(legacyProfiles) != 1 || legacyProfiles[0] != legacy {
		t.Fatalf("legacy SQLite profile mirror = %#v, want %#v", legacyProfiles, legacy)
	}
}

func TestConnectionProfileValidation(t *testing.T) {
	tests := []struct {
		name    string
		profile ConnectionProfile
	}{
		{name: "unknown driver", profile: ConnectionProfile{Name: "x", Driver: "unknown"}},
		{name: "postgres without host", profile: ConnectionProfile{Name: "x", Driver: DriverPostgreSQL}},
		{name: "mysql invalid port", profile: ConnectionProfile{Name: "x", Driver: DriverMySQL, Host: "localhost", Port: 65536}},
		{name: "remote host with DSN credentials", profile: ConnectionProfile{Name: "x", Driver: DriverPostgreSQL, Host: "postgres://user:secret@localhost/db"}},
		{name: "SQLite without path", profile: ConnectionProfile{Name: "x", Driver: DriverSQLite}},
		{name: "malformed credential reference", profile: ConnectionProfile{Name: "x", Driver: DriverPostgreSQL, Host: "localhost", CredentialRef: "plain secret"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := normalizeConnectionProfile(test.profile); err == nil {
				t.Fatalf("invalid profile was accepted: %#v", test.profile)
			}
		})
	}
	if _, err := normalizeConnectionProfile(ConnectionProfile{Name: "SQLite with TCP fields", Driver: DriverSQLite, Path: "/db/a", Host: "localhost"}); err == nil {
		t.Fatal("SQLite profile with remote fields was accepted")
	}
}

func TestRemoteConnectionProfileNormalizesTLSMode(t *testing.T) {
	profile, err := normalizeConnectionProfile(ConnectionProfile{
		Name: "Postgres", Driver: DriverPostgreSQL, Host: "localhost", TLSMode: " VERIFY-CA ",
	})
	if err != nil {
		t.Fatal(err)
	}
	if profile.TLSMode != "verify-ca" {
		t.Fatalf("normalized TLS mode = %q, want verify-ca", profile.TLSMode)
	}
	profile, err = normalizeConnectionProfile(ConnectionProfile{Name: "Postgres", Driver: DriverPostgreSQL, Host: "localhost"})
	if err != nil {
		t.Fatal(err)
	}
	if profile.TLSMode != "verify-full" {
		t.Fatalf("default TLS mode = %q, want verify-full", profile.TLSMode)
	}
}

func TestRedisConnectionProfileValidationAndDefaults(t *testing.T) {
	profile, err := normalizeConnectionProfile(ConnectionProfile{Name: "Cache", Driver: DriverRedis, Host: "redis.example.test"})
	if err != nil {
		t.Fatal(err)
	}
	if profile.Port != 6379 || profile.TLSMode != "verify-full" || profile.RedisDB != 0 {
		t.Fatalf("Redis defaults = %#v", profile)
	}
	for _, invalid := range []ConnectionProfile{
		{Name: "bad db", Driver: DriverRedis, Host: "localhost", RedisDB: -1},
		{Name: "bad tls", Driver: DriverRedis, Host: "localhost", TLSMode: "prefer"},
		{Name: "named db", Driver: DriverRedis, Host: "localhost", Database: "cache"},
	} {
		if _, err := normalizeConnectionProfile(invalid); err == nil {
			t.Fatalf("invalid Redis profile accepted: %#v", invalid)
		}
	}
}

func TestMongoConnectionProfileDefaultsAndValidation(t *testing.T) {
	profile, err := normalizeConnectionProfile(ConnectionProfile{Name: "Catalog", Driver: DriverMongoDB, Host: "mongo.example.test", Database: "catalog", Username: "reader", AuthSource: "admin", TLSCAPath: " /etc/dbl/mongo-ca.pem "})
	if err != nil {
		t.Fatal(err)
	}
	if profile.Port != 27017 || profile.TLSMode != "verify-full" || profile.AuthSource != "admin" || profile.TLSCAPath != "/etc/dbl/mongo-ca.pem" {
		t.Fatalf("MongoDB profile = %#v", profile)
	}
	for _, invalid := range []ConnectionProfile{
		{Name: "cluster", Driver: DriverMongoDB, Host: "mongo-a,mongo-b"},
		{Name: "fallback", Driver: DriverMongoDB, Host: "localhost", TLSMode: "prefer"},
		{Name: "CA without verification", Driver: DriverMongoDB, Host: "localhost", TLSMode: "require", TLSCAPath: "/etc/dbl/ca.pem"},
		{Name: "authSource on postgres", Driver: DriverPostgreSQL, Host: "localhost", AuthSource: "admin"},
	} {
		if _, err := normalizeConnectionProfile(invalid); err == nil {
			t.Fatalf("invalid profile accepted: %#v", invalid)
		}
	}
}
