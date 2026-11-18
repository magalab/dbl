package app

import (
	"crypto/sha256"
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
	"github.com/magalab/dbl/internal/safeerror"
	"github.com/magalab/dbl/internal/sqlite"
)

const (
	profileTestNone uint8 = iota
	profileTestRunning
	profileTestPassed
	profileTestFailed
)

func (app *workbench) resetConnectionProfileTest() {
	app.profileTestState = profileTestNone
	app.profileTestMessage = ""
	app.profileTestDetails = ""
	app.profileTestFingerprint = ""
}

func (app *workbench) connectionProfileTestFingerprint() string {
	profile, err := app.connectionProfileDraft()
	if err != nil {
		return ""
	}
	profile.Name = ""
	profile.Group = ""
	profile.Environment = ""
	profile.Favorite = false
	profile.RedisDatabaseAliases = nil
	secret := app.profileSecret
	if app.profileSecretFromVault {
		secret = ""
	}
	value := fmt.Sprintf("%#v|%s|%s|%s", profile, secret, app.profileTLSCertPath, app.profileTLSKeyPath)
	return fmt.Sprintf("%x", sha256.Sum256([]byte(value)))
}

func (app *workbench) testConnectionProfileFromForm() {
	if app.busy || app.exporting || app.dialogOpen {
		return
	}
	profile, cleanup, err := app.prepareConnectionProfileTest()
	if err != nil {
		cleanup()
		app.profileTestState = profileTestFailed
		app.profileTestMessage = "连接测试失败。"
		app.profileTestDetails = safeerror.Display(err)
		app.profileTestFingerprint = app.connectionProfileTestFingerprint()
		return
	}

	var connector connection.Connector
	var timeout time.Duration
	switch profile.Driver {
	case preferences.DriverSQLite:
		access := sqlite.ReadWriteExisting
		if profile.ReadOnly {
			access = sqlite.ReadOnly
		}
		connector = sqlite.Connector{Path: profile.Path, Access: access}
		timeout = 10 * time.Second
	case preferences.DriverPostgreSQL:
		connector = postgresql.Connector{Profile: profile, Vault: app.vault}
		timeout = 20 * time.Second
	case preferences.DriverMySQL:
		connector = mysqladapter.Connector{Profile: profile, Vault: app.vault}
		timeout = 20 * time.Second
	case preferences.DriverRedis:
		connector = redisadapter.Connector{Profile: profile, Vault: app.vault}
		timeout = 15 * time.Second
	case preferences.DriverMongoDB:
		connector = mongoadapter.Connector{Profile: profile, Vault: app.vault}
		timeout = 20 * time.Second
	case preferences.DriverEtcd:
		connector = etcdadapter.Connector{Profile: profile, Vault: app.vault}
		timeout = 20 * time.Second
	default:
		cleanup()
		app.profileTestState = profileTestFailed
		app.profileTestMessage = "连接测试失败。"
		app.profileTestDetails = safeerror.Display(fmt.Errorf("unsupported database driver: %s", profile.Driver))
		app.profileTestFingerprint = app.connectionProfileTestFingerprint()
		return
	}
	app.runConnectionProfileTest(connector, timeout, cleanup)
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

	if app.profileSecret != "" && !app.profileSecretFromVault {
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

func (app *workbench) runConnectionProfileTest(connector connection.Connector, timeout time.Duration, cleanup func()) {
	if app.busy || app.exporting || app.dialogOpen {
		cleanup()
		return
	}
	app.busy = true
	app.profileTestState = profileTestRunning
	app.profileTestMessage = "正在测试连接…"
	app.profileTestDetails = ""
	app.profileTestFingerprint = app.connectionProfileTestFingerprint()
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
				app.profileTestState = profileTestFailed
				app.profileTestMessage = "连接测试失败。"
				app.profileTestDetails = safeerror.Display(err)
				return
			}
			app.profileTestState = profileTestPassed
			app.profileTestMessage = "连接测试成功。"
			app.profileTestDetails = ""
		})
	}()
}
