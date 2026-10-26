package preferences

import (
	"errors"
	"fmt"
	"strings"

	"github.com/magalab/dbl/internal/credentials"
)

const (
	EnvironmentDevelopment = "development"
	EnvironmentStaging     = "staging"
	EnvironmentProduction  = "production"

	DriverSQLite     = "sqlite"
	DriverPostgreSQL = "postgresql"
	DriverMySQL      = "mysql"
	DriverRedis      = "redis"
	DriverMongoDB    = "mongodb"
	DriverEtcd       = "etcd"
)

// ConnectionProfile stores connection metadata and an optional opaque
// credential reference. It must never contain a password or other secret.
type ConnectionProfile struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Group         string `json:"group,omitempty"`
	Environment   string `json:"environment,omitempty"`
	Driver        string `json:"driver"`
	Path          string `json:"path,omitempty"`
	Host          string `json:"host,omitempty"`
	Port          int    `json:"port,omitempty"`
	TLSMode       string `json:"tls_mode,omitempty"`
	Database      string `json:"database,omitempty"`
	RedisDB       int    `json:"redis_db,omitempty"`
	AuthSource    string `json:"auth_source,omitempty"`
	TLSCAPath     string `json:"tls_ca_path,omitempty"`
	TLSCertRef    string `json:"tls_cert_ref,omitempty"`
	TLSKeyRef     string `json:"tls_key_ref,omitempty"`
	Username      string `json:"username,omitempty"`
	CredentialRef string `json:"credential_ref,omitempty"`
	ReadOnly      bool   `json:"read_only,omitempty"`
	Favorite      bool   `json:"favorite,omitempty"`
}

func normalizeConnectionProfile(profile ConnectionProfile) (ConnectionProfile, error) {
	profile.ID = strings.TrimSpace(profile.ID)
	profile.Name = strings.TrimSpace(profile.Name)
	profile.Group = strings.TrimSpace(profile.Group)
	profile.Environment = strings.ToLower(strings.TrimSpace(profile.Environment))
	if profile.Environment == "" {
		profile.Environment = EnvironmentDevelopment
	}
	switch profile.Environment {
	case EnvironmentDevelopment, EnvironmentStaging, EnvironmentProduction:
	default:
		return ConnectionProfile{}, errors.New("unsupported connection environment")
	}
	profile.Driver = strings.ToLower(strings.TrimSpace(profile.Driver))
	profile.Path = strings.TrimSpace(profile.Path)
	profile.Host = strings.TrimSpace(profile.Host)
	profile.Database = strings.TrimSpace(profile.Database)
	profile.AuthSource = strings.TrimSpace(profile.AuthSource)
	profile.TLSCAPath = strings.TrimSpace(profile.TLSCAPath)
	profile.TLSCertRef = strings.TrimSpace(profile.TLSCertRef)
	profile.TLSKeyRef = strings.TrimSpace(profile.TLSKeyRef)
	profile.Username = strings.TrimSpace(profile.Username)
	profile.CredentialRef = strings.TrimSpace(profile.CredentialRef)
	profile.TLSMode = strings.ToLower(strings.TrimSpace(profile.TLSMode))
	if profile.Name == "" {
		return ConnectionProfile{}, errors.New("connection profile name is required")
	}
	switch profile.Driver {
	case DriverSQLite:
		if profile.TLSMode != "" {
			return ConnectionProfile{}, errors.New("SQLite profile contains remote connection settings")
		}
		if profile.Path == "" {
			return ConnectionProfile{}, errors.New("SQLite profile path is required")
		}
		if profile.Host != "" || profile.Port != 0 || profile.Database != "" || profile.Username != "" || profile.RedisDB != 0 || profile.AuthSource != "" || profile.TLSCAPath != "" || profile.TLSCertRef != "" || profile.TLSKeyRef != "" {
			return ConnectionProfile{}, errors.New("SQLite profile contains remote connection fields")
		}
	case DriverPostgreSQL, DriverMySQL, DriverRedis, DriverMongoDB, DriverEtcd:
		if profile.Host == "" {
			return ConnectionProfile{}, errors.New("remote connection host is required")
		}
		if profile.Driver != DriverEtcd && (strings.ContainsAny(profile.Host, " \t\r\n/@?#=") || strings.Contains(profile.Host, "://")) {
			return ConnectionProfile{}, errors.New("remote connection host must not contain a URL, DSN, or credentials")
		}
		if profile.Path != "" {
			return ConnectionProfile{}, errors.New("remote connection profile contains a local path")
		}
		if profile.Driver == DriverRedis && profile.Database != "" {
			return ConnectionProfile{}, errors.New("Redis profile must use the database index, not a database name")
		}
		if profile.Driver != DriverRedis && profile.RedisDB != 0 {
			return ConnectionProfile{}, errors.New("non-Redis profile contains a Redis database index")
		}
		if profile.Driver != DriverMongoDB && profile.AuthSource != "" {
			return ConnectionProfile{}, errors.New("non-MongoDB profile contains an authentication database")
		}
		if profile.TLSCertRef != "" || profile.TLSKeyRef != "" {
			if profile.Driver != DriverEtcd {
				return ConnectionProfile{}, errors.New("client certificate settings are supported only for etcd profiles")
			}
		}
		if profile.TLSCAPath != "" && profile.Driver != DriverEtcd && profile.Driver != DriverPostgreSQL && profile.Driver != DriverMySQL && profile.Driver != DriverMongoDB {
			return ConnectionProfile{}, errors.New("custom CA certificates are supported only for PostgreSQL, MySQL, MongoDB, and etcd profiles")
		}
		if profile.Driver == DriverEtcd && ((profile.TLSCertRef == "") != (profile.TLSKeyRef == "")) {
			return ConnectionProfile{}, errors.New("etcd client certificate and key must be configured together")
		}
		if profile.Driver == DriverMongoDB && strings.Contains(profile.Host, ",") {
			return ConnectionProfile{}, errors.New("MongoDB profile supports one host per connection")
		}
		if profile.Driver == DriverEtcd {
			for _, endpoint := range strings.Split(profile.Host, ",") {
				endpoint = strings.TrimSpace(endpoint)
				if endpoint == "" || strings.ContainsAny(endpoint, " \t\r\n?#") || strings.Contains(endpoint, "@") {
					return ConnectionProfile{}, errors.New("etcd endpoints must be comma-separated host:port or http(s) URLs without credentials")
				}
				if strings.Contains(endpoint, "://") && !strings.HasPrefix(endpoint, "http://") && !strings.HasPrefix(endpoint, "https://") {
					return ConnectionProfile{}, errors.New("etcd endpoint URL must use http or https")
				}
			}
		}
		if profile.RedisDB < 0 || profile.RedisDB > 2147483647 {
			return ConnectionProfile{}, errors.New("Redis database index must be a non-negative 32-bit integer")
		}
		if profile.TLSMode == "" {
			profile.TLSMode = "verify-full"
		}
		switch profile.TLSMode {
		case "verify-full", "verify-ca", "require", "prefer", "disable":
		default:
			return ConnectionProfile{}, errors.New("unsupported remote connection TLS mode")
		}
		if profile.Driver == DriverRedis && profile.TLSMode == "prefer" {
			return ConnectionProfile{}, errors.New("Redis does not support TLS prefer mode")
		}
		if profile.Driver == DriverMongoDB && profile.TLSMode == "prefer" {
			return ConnectionProfile{}, errors.New("MongoDB profile does not support TLS prefer mode")
		}
		if profile.Driver == DriverEtcd && profile.TLSMode != "verify-full" && profile.TLSMode != "disable" {
			return ConnectionProfile{}, errors.New("etcd supports verify-full or disable TLS modes")
		}
		if profile.TLSCAPath != "" && profile.TLSMode != "verify-full" && profile.TLSMode != "verify-ca" {
			return ConnectionProfile{}, errors.New("custom CA requires verify-full or verify-ca TLS mode")
		}
		if profile.Port == 0 {
			if profile.Driver == DriverPostgreSQL {
				profile.Port = 5432
			} else {
				if profile.Driver == DriverMySQL {
					profile.Port = 3306
				} else if profile.Driver == DriverMongoDB {
					profile.Port = 27017
				} else if profile.Driver == DriverEtcd {
					profile.Port = 2379
				} else {
					profile.Port = 6379
				}
			}
		}
		if profile.Port < 1 || profile.Port > 65535 {
			return ConnectionProfile{}, errors.New("connection profile port must be between 1 and 65535")
		}
	default:
		return ConnectionProfile{}, fmt.Errorf("unsupported connection profile driver %q", profile.Driver)
	}
	if profile.CredentialRef != "" && !credentials.Reference(profile.CredentialRef).Valid() {
		return ConnectionProfile{}, errors.New("connection profile credential reference is invalid")
	}
	for _, ref := range []string{profile.TLSCertRef, profile.TLSKeyRef} {
		if ref != "" && !credentials.Reference(ref).Valid() {
			return ConnectionProfile{}, errors.New("connection profile TLS credential reference is invalid")
		}
	}
	return profile, nil
}

func sqliteProfile(profile ConnectionProfile) SQLiteProfile {
	legacy := SQLiteProfile{
		ID: profile.ID, Name: profile.Name, Group: profile.Group, Path: profile.Path,
		ReadOnly: profile.ReadOnly, Favorite: profile.Favorite,
	}
	if profile.Environment != "" && profile.Environment != EnvironmentDevelopment {
		legacy.Environment = profile.Environment
	}
	return legacy
}

func connectionProfile(profile SQLiteProfile) ConnectionProfile {
	return ConnectionProfile{
		ID: profile.ID, Name: profile.Name, Group: profile.Group, Driver: DriverSQLite,
		Path: profile.Path, Environment: profile.Environment, ReadOnly: profile.ReadOnly, Favorite: profile.Favorite,
	}
}

func legacySQLiteProfiles(profiles []ConnectionProfile) []SQLiteProfile {
	legacy := make([]SQLiteProfile, 0)
	for _, profile := range profiles {
		if profile.Driver == DriverSQLite {
			legacy = append(legacy, sqliteProfile(profile))
		}
	}
	return legacy
}
