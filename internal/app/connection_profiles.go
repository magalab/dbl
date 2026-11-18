package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
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

func existingSQLitePath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return ""
	}
	return path
}

func (app *workbench) loadConnectionProfile(profile preferences.ConnectionProfile) {
	app.resetConnectionProfileTest()
	app.selectedProfileID = profile.ID
	app.profileDriver = profile.Driver
	app.profileName = profile.Name
	app.profileGroup = profile.Group
	app.profileEnvironment = profile.Environment
	if app.profileEnvironment == "" {
		app.profileEnvironment = preferences.EnvironmentDevelopment
	}
	app.profileReadOnly = profile.ReadOnly
	app.profileFavorite = profile.Favorite
	if profile.Driver == preferences.DriverSQLite {
		app.path = profile.Path
	}
	app.profileHost = profile.Host
	app.profileTLSMode = tlsModeLabel(profile.TLSMode)
	app.profilePort = ""
	if profile.Port != 0 {
		app.profilePort = strconv.Itoa(profile.Port)
	}
	app.profileDatabase = profile.Database
	app.profileRedisDB = strconv.Itoa(profile.RedisDB)
	app.profileRedisAliases = cloneRedisDatabaseAliases(profile.RedisDatabaseAliases)
	app.profileAuthSource = profile.AuthSource
	app.profileTLSCAPath = profile.TLSCAPath
	app.profileTLSCertPath, app.profileTLSKeyPath = "", ""
	app.profileUsername = profile.Username
	app.profileCredential = profile.CredentialRef
	app.resetProfileSecret()
	app.removeCredential = false
	app.readOnlyMode = profile.ReadOnly
}

// connectProfileForTree opens a profile without changing the active workspace.
func (app *workbench) connectProfileForTree(ctx context.Context, profile preferences.ConnectionProfile) (connection.Connection, error) {
	switch profile.Driver {
	case preferences.DriverSQLite:
		access := sqlite.ReadWriteExisting
		if profile.ReadOnly {
			access = sqlite.ReadOnly
		}
		return (sqlite.Connector{Path: profile.Path, Access: access}).Connect(ctx)
	case preferences.DriverPostgreSQL:
		return (postgresql.Connector{Profile: profile, Vault: app.vault}).Connect(ctx)
	case preferences.DriverMySQL:
		return (mysqladapter.Connector{Profile: profile, Vault: app.vault}).Connect(ctx)
	case preferences.DriverRedis:
		return (redisadapter.Connector{Profile: profile, Vault: app.vault}).Connect(ctx)
	case preferences.DriverMongoDB:
		return (mongoadapter.Connector{Profile: profile, Vault: app.vault}).Connect(ctx)
	case preferences.DriverEtcd:
		return (etcdadapter.Connector{Profile: profile, Vault: app.vault}).Connect(ctx)
	default:
		return nil, fmt.Errorf("unsupported database driver: %s", profile.Driver)
	}
}

func tlsModeLabel(mode string) string {
	switch mode {
	case "verify-full":
		return "验证证书和主机名"
	case "verify-ca":
		return "验证证书"
	case "require":
		return "加密，不验证证书"
	case "prefer":
		return "优先 TLS，可回退明文"
	case "disable":
		return "关闭 TLS"
	default:
		return "验证证书和主机名"
	}
}

func profileTLSMode(label string) string {
	switch label {
	case "验证证书和主机名":
		return "verify-full"
	case "验证证书":
		return "verify-ca"
	case "加密，不验证证书":
		return "require"
	case "优先 TLS，可回退明文":
		return "prefer"
	case "关闭 TLS":
		return "disable"
	default:
		return "verify-full"
	}
}

func environmentLabel(environment string) string {
	switch environment {
	case preferences.EnvironmentProduction:
		return "生产"
	case preferences.EnvironmentStaging:
		return "预发"
	case preferences.EnvironmentDevelopment:
		return "开发"
	case "":
		return "开发"
	default:
		return "未标记"
	}
}

func (app *workbench) checkConnection() {
	if app.busy || app.exporting || app.dialogOpen || (app.store == nil && app.keyStore == nil && app.documentStore == nil && app.etcdStore == nil) {
		return
	}
	if app.etcdCancel != nil {
		app.etcdCancel()
	}
	app.etcdCancel, app.etcdWatching = nil, false
	app.etcdStore = nil
	app.etcdEntries, app.etcdSelectedKey = nil, ""
	app.etcdSelected = connection.EtcdEntry{}
	app.etcdLeaseInfo = connection.EtcdLeaseInfo{}
	app.etcdLeaseInfoErr = ""
	app.etcdLeaseInfoLoading = false
	app.etcdWatchEvents = nil
	app.etcdDeleteReady, app.etcdWriteReady = false, false
	app.etcdWriteConfirmation = etcdWriteConfirmation{}
	app.busy = true
	app.status = app.localized("正在检查数据库连接…")
	store := app.store
	go func() {
		ctx, cancel := app.withTaskTimeout(5 * time.Second)
		defer cancel()
		err := store.HealthCheck(ctx)
		app.window.Update(func() {
			app.busy = false
			if app.store != store {
				return
			}
			if err != nil {
				switch store.ClassifyError(err) {
				case connection.ErrorTimeout:
					app.status = app.localized("连接健康检查超时。")
				case connection.ErrorCanceled:
					app.status = app.localized("连接健康检查已取消。")
				case connection.ErrorBusy:
					app.reportError("数据库正忙", err)
				default:
					app.reportError("连接健康检查失败", err)
				}
				return
			}
			app.status = strings.ToUpper(string(store.Driver())) + " 连接正常。"
		})
	}()
}

func (app *workbench) connectionProfileDraft() (preferences.ConnectionProfile, error) {
	profile := preferences.ConnectionProfile{
		ID: app.selectedProfileID, Name: app.profileName, Group: app.profileGroup,
		Environment: app.profileEnvironment,
		Driver:      app.profileDriver, Path: app.path, Host: app.profileHost,
		TLSMode:  profileTLSMode(app.profileTLSMode),
		Database: app.profileDatabase, Username: app.profileUsername,
		RedisDatabaseAliases: cloneRedisDatabaseAliases(app.profileRedisAliases),
		AuthSource:           app.profileAuthSource,
		TLSCAPath:            app.profileTLSCAPath,
		CredentialRef:        app.profileCredential, ReadOnly: app.profileReadOnly,
		Favorite: app.profileFavorite,
	}
	for _, existing := range app.connectionProfiles {
		if existing.ID == app.selectedProfileID {
			profile.TLSCertRef, profile.TLSKeyRef = existing.TLSCertRef, existing.TLSKeyRef
			if profile.Driver == preferences.DriverRedis && len(profile.RedisDatabaseAliases) == 0 {
				profile.RedisDatabaseAliases = cloneRedisDatabaseAliases(existing.RedisDatabaseAliases)
			}
			break
		}
	}
	if profile.Driver == preferences.DriverRedis {
		profile.Database = ""
		if strings.TrimSpace(app.profileRedisDB) != "" {
			dbIndex, err := strconv.Atoi(strings.TrimSpace(app.profileRedisDB))
			if err != nil {
				return preferences.ConnectionProfile{}, errors.New("Redis DB 索引必须是数字")
			}
			profile.RedisDB = dbIndex
		}
	} else {
		profile.RedisDB = 0
	}
	if profile.Driver == preferences.DriverEtcd {
		profile.Database = ""
	}
	if profile.Driver != preferences.DriverEtcd && profile.Driver != preferences.DriverPostgreSQL && profile.Driver != preferences.DriverMySQL && profile.Driver != preferences.DriverMongoDB {
		profile.TLSCAPath = ""
	}
	if profile.Driver != preferences.DriverEtcd {
		profile.TLSCertRef, profile.TLSKeyRef = "", ""
	}
	if profile.Driver != preferences.DriverMongoDB {
		profile.AuthSource = ""
	}
	if profile.Driver != preferences.DriverSQLite {
		profile.Path = ""
	} else {
		profile.TLSMode = ""
	}
	if profile.Driver != preferences.DriverSQLite && strings.TrimSpace(app.profilePort) != "" {
		port, err := strconv.Atoi(strings.TrimSpace(app.profilePort))
		if err != nil {
			return preferences.ConnectionProfile{}, errors.New("端口必须是数字")
		}
		profile.Port = port
	}
	if app.removeCredential {
		profile.CredentialRef = ""
	}
	return profile, nil
}

func (app *workbench) saveConnectionProfile(connectAfterSave bool) {
	if app.busy || app.preferences == nil {
		return
	}
	if app.profileTestState != profileTestPassed || app.profileTestFingerprint != app.connectionProfileTestFingerprint() {
		app.profileTestMessage = "请先测试连接；测试成功且配置未变化后才能保存。"
		app.profileTestState = profileTestFailed
		return
	}
	profile, err := app.connectionProfileDraft()
	if err != nil {
		app.reportError("保存连接配置失败", err)
		return
	}
	var secret []byte
	if !app.profileSecretFromVault {
		secret = []byte(app.profileSecret)
	}
	app.resetProfileSecret()
	app.preferenceRevision++
	revision := app.preferenceRevision
	store := app.preferences
	previousRef := credentials.Reference(app.profileCredential)
	previousTLSCertRef, previousTLSKeyRef := credentials.Reference(profile.TLSCertRef), credentials.Reference(profile.TLSKeyRef)
	certPath, keyPath := strings.TrimSpace(app.profileTLSCertPath), strings.TrimSpace(app.profileTLSKeyPath)
	app.busy = true
	app.status = app.localized("正在保存连接配置…")
	go func() {
		defer func() {
			for i := range secret {
				secret[i] = 0
			}
		}()
		vault := app.vault
		var newRef credentials.Reference
		var newTLSCertRef, newTLSKeyRef credentials.Reference
		storeFileCredential := func(path string) (credentials.Reference, error) {
			file, err := os.Open(path)
			if err != nil {
				return "", fmt.Errorf("read TLS credential file: %w", err)
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
			return vault.Store(data)
		}
		if certPath != "" {
			var err error
			newTLSCertRef, err = storeFileCredential(certPath)
			if err != nil {
				app.window.Update(func() { app.busy = false; app.reportError("保存 etcd 客户端证书失败", err) })
				return
			}
			profile.TLSCertRef = string(newTLSCertRef)
		}
		if keyPath != "" {
			var err error
			newTLSKeyRef, err = storeFileCredential(keyPath)
			if err != nil {
				if newTLSCertRef != "" {
					_ = vault.Delete(newTLSCertRef)
				}
				app.window.Update(func() { app.busy = false; app.reportError("保存 etcd 客户端私钥失败", err) })
				return
			}
			profile.TLSKeyRef = string(newTLSKeyRef)
		}
		if len(secret) > 0 {
			var err error
			newRef, err = vault.Store(secret)
			if err != nil {
				if newTLSCertRef != "" {
					_ = vault.Delete(newTLSCertRef)
				}
				if newTLSKeyRef != "" {
					_ = vault.Delete(newTLSKeyRef)
				}
				app.window.Update(func() {
					app.busy = false
					app.reportError("保存系统凭据失败", err)
				})
				return
			}
			profile.CredentialRef = string(newRef)
		}
		saved, err := store.SaveConnectionProfile(profile, revision)
		if err != nil && newRef != "" {
			_ = vault.Delete(newRef)
		}
		if err != nil {
			if newTLSCertRef != "" {
				_ = vault.Delete(newTLSCertRef)
			}
			if newTLSKeyRef != "" {
				_ = vault.Delete(newTLSKeyRef)
			}
		}
		settings := store.Settings()
		var cleanupErr error
		if err == nil {
			cleanupErr = deleteUnusedCredentials(vault, []credentials.Reference{previousRef, previousTLSCertRef, previousTLSKeyRef}, settings.ConnectionProfiles)
		}
		app.window.Update(func() {
			app.busy = false
			if err != nil {
				app.reportError("保存连接配置失败", err)
				return
			}
			app.connectionProfiles = settings.ConnectionProfiles
			app.selectedProfileID = saved.ID
			app.profileCredential = saved.CredentialRef
			app.showConnectionEditor = false
			app.discardConnectionEditorSecrets()
			if cleanupErr != nil {
				app.reportError("连接已保存，但旧凭据未能清理", cleanupErr)
				return
			}
			if connectAfterSave {
				app.closeProfileConnection(saved)
				for _, live := range app.liveConnections {
					if live.profile.ID == saved.ID {
						return
					}
				}
				app.openSavedProfile(saved)
				return
			}
			app.status = app.localized("连接配置已保存。")
		})
	}()
}

func (app *workbench) deleteConnectionProfile(id string) {
	if app.busy || app.preferences == nil {
		return
	}
	var credentialRefs []credentials.Reference
	for _, profile := range app.connectionProfiles {
		if profile.ID == id {
			credentialRefs = []credentials.Reference{credentials.Reference(profile.CredentialRef), credentials.Reference(profile.TLSCertRef), credentials.Reference(profile.TLSKeyRef)}
			break
		}
	}
	app.preferenceRevision++
	revision := app.preferenceRevision
	store := app.preferences
	app.busy = true
	app.status = app.localized("正在删除连接配置…")
	go func() {
		err := store.DeleteConnectionProfile(id, revision)
		settings := store.Settings()
		var cleanupErr error
		if err == nil {
			vault := app.vault
			cleanupErr = deleteUnusedCredentials(vault, credentialRefs, settings.ConnectionProfiles)
		}
		app.window.Update(func() {
			app.busy = false
			if err != nil {
				app.reportError("删除连接配置失败", err)
				return
			}
			app.connectionProfiles = settings.ConnectionProfiles
			if app.selectedProfileID == id {
				app.discardConnectionEditorSecrets()
				app.selectedProfileID = ""
				app.profileName = ""
				app.profileGroup = ""
				app.profileHost = ""
				app.profilePort = ""
				app.profileDatabase = ""
				app.profileAuthSource = ""
				app.profileTLSCAPath, app.profileTLSCertPath, app.profileTLSKeyPath = "", "", ""
				app.profileUsername = ""
				app.profileCredential = ""
				app.profileReadOnly = false
				app.profileFavorite = false
			}
			if cleanupErr != nil {
				app.reportError("连接已删除，但凭据清理未完成", cleanupErr)
				return
			}
			app.status = app.localized("连接配置已删除。")
		})
	}()
}

func deleteUnusedCredentials(vault *credentials.Vault, refs []credentials.Reference, profiles []preferences.ConnectionProfile) error {
	seen := make(map[credentials.Reference]struct{}, len(refs))
	var cleanupErr error
	for _, ref := range refs {
		if ref == "" {
			continue
		}
		if _, duplicate := seen[ref]; duplicate {
			continue
		}
		seen[ref] = struct{}{}
		used := false
		for _, profile := range profiles {
			if profile.CredentialRef == string(ref) || profile.TLSCertRef == string(ref) || profile.TLSKeyRef == string(ref) {
				used = true
				break
			}
		}
		if !used {
			cleanupErr = errors.Join(cleanupErr, vault.Delete(ref))
		}
	}
	return cleanupErr
}
