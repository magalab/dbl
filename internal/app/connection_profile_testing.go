package app

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/magalab/dbl/internal/connection"
	"github.com/magalab/dbl/internal/credentials"
	etcdadapter "github.com/magalab/dbl/internal/etcd"
	mongoadapter "github.com/magalab/dbl/internal/mongodb"
	mysqladapter "github.com/magalab/dbl/internal/mysql"
	"github.com/magalab/dbl/internal/postgresql"
	"github.com/magalab/dbl/internal/preferences"
	redisadapter "github.com/magalab/dbl/internal/redis"
	"github.com/magalab/dbl/internal/sqlite"
)

func (app *workbench) testConnectionProfileFromForm() {
	if app.busy || app.exporting || app.dialogOpen {
		return
	}
	profile, cleanup, err := app.prepareConnectionProfileTest()
	if err != nil {
		cleanup()
		app.reportError("测试连接失败", err)
		return
	}

	var connector connection.Connector
	var timeout time.Duration
	label := profile.Driver
	switch profile.Driver {
	case preferences.DriverSQLite:
		access := sqlite.ReadWriteExisting
		if profile.ReadOnly {
			access = sqlite.ReadOnly
		}
		connector = sqlite.Connector{Path: profile.Path, Access: access}
		timeout, label = 10*time.Second, "SQLite"
	case preferences.DriverPostgreSQL:
		connector = postgresql.Connector{Profile: profile, Vault: app.vault}
		timeout, label = 20*time.Second, "PostgreSQL"
	case preferences.DriverMySQL:
		connector = mysqladapter.Connector{Profile: profile, Vault: app.vault}
		timeout, label = 20*time.Second, "MySQL"
	case preferences.DriverRedis:
		connector = redisadapter.Connector{Profile: profile, Vault: app.vault}
		timeout, label = 15*time.Second, "Redis"
	case preferences.DriverMongoDB:
		connector = mongoadapter.Connector{Profile: profile, Vault: app.vault}
		timeout, label = 20*time.Second, "MongoDB"
	case preferences.DriverEtcd:
		connector = etcdadapter.Connector{Profile: profile, Vault: app.vault}
		timeout, label = 20*time.Second, "etcd"
	default:
		cleanup()
		app.reportError("测试连接失败", fmt.Errorf("unsupported database driver: %s", profile.Driver))
		return
	}
	app.runConnectionProfileTest(label, connector, timeout, cleanup)
}

func (app *workbench) prepareConnectionProfileTest() (preferences.ConnectionProfile, func(), error) {
	profile, err := app.connectionProfileDraft()
	refs := make([]credentials.Reference, 0, 3)
	cleanup := func() {
		if app.vault == nil {
			return
		}
		for _, ref := range refs {
			_ = app.vault.Delete(ref)
		}
		refs = nil
	}
	if err != nil {
		return preferences.ConnectionProfile{}, cleanup, err
	}
	storeSecret := func(secret []byte) (credentials.Reference, error) {
		ref, err := app.vault.Store(secret)
		clear(secret)
		if err == nil {
			refs = append(refs, ref)
		}
		return ref, err
	}

	if app.profileSecret != "" {
		ref, err := storeSecret([]byte(app.profileSecret))
		if err != nil {
			return profile, cleanup, fmt.Errorf("store temporary connection credential: %w", err)
		}
		profile.CredentialRef = string(ref)
	}
	if profile.Driver == preferences.DriverEtcd {
		if path := strings.TrimSpace(app.profileTLSCertPath); path != "" {
			ref, err := app.storeTemporaryTLSFile(path)
			if err != nil {
				return profile, cleanup, fmt.Errorf("read client certificate: %w", err)
			}
			refs = append(refs, ref)
			profile.TLSCertRef = string(ref)
		}
		if path := strings.TrimSpace(app.profileTLSKeyPath); path != "" {
			ref, err := app.storeTemporaryTLSFile(path)
			if err != nil {
				return profile, cleanup, fmt.Errorf("read client private key: %w", err)
			}
			refs = append(refs, ref)
			profile.TLSKeyRef = string(ref)
		}
	}
	return profile, cleanup, nil
}

func (app *workbench) storeTemporaryTLSFile(path string) (credentials.Reference, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open TLS credential file: %w", err)
	}
	data, err := io.ReadAll(io.LimitReader(file, credentials.MaxSecretSize+1))
	closeErr := file.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		clear(data)
		return "", fmt.Errorf("read TLS credential file: %w", err)
	}
	if len(data) > credentials.MaxSecretSize {
		clear(data)
		return "", credentials.ErrSecretTooLarge
	}
	defer clear(data)
	return app.vault.Store(data)
}

func (app *workbench) runConnectionProfileTest(label string, connector connection.Connector, timeout time.Duration, cleanup func()) {
	if app.busy || app.exporting || app.dialogOpen {
		cleanup()
		return
	}
	app.busy = true
	app.status = app.localized("正在测试 ") + label + " 连接配置…"
	go func() {
		defer cleanup()
		ctx, cancel := app.withTaskTimeout(timeout)
		defer cancel()
		adapter, err := connector.Connect(ctx)
		if err == nil {
			err = adapter.HealthCheck(ctx)
			closeErr := adapter.Close()
			if err == nil {
				err = closeErr
			}
		}
		app.window.Update(func() {
			app.busy = false
			if err != nil {
				app.reportError(label+" 连接测试失败", err)
				return
			}
			app.status = label + " 连接配置测试成功。"
		})
	}()
}
