package credentials

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/magalab/dbl/internal/privatefs"
)

const localCredentialFileMagic = "DBLC1"

// NewLocal stores encrypted credentials in a private local directory. The
// encryption key is stored beside the ciphertext and protected with user-only
// filesystem permissions, so access to an OS credential manager is not needed.
func NewLocal(dir string) (*Vault, error) {
	backend, err := newLocalBackend(dir)
	if err != nil {
		return nil, err
	}
	return New(backend), nil
}

// NewMigratingLocal stores all new credentials locally and migrates an older
// system-vault item lazily the first time its reference is loaded.
func NewMigratingLocal(dir string) (*Vault, error) {
	local, err := newLocalBackend(dir)
	if err != nil {
		return nil, err
	}
	return New(&migratingBackend{local: local, legacy: systemBackend{}}), nil
}

type localBackend struct {
	dir string
	key []byte
	mu  sync.Mutex
}

func newLocalBackend(dir string) (*localBackend, error) {
	if strings.TrimSpace(dir) == "" {
		return nil, errors.New("credential directory is empty")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("create credential directory: %w", err)
	}
	dirInfo, err := os.Lstat(dir)
	if err != nil {
		return nil, fmt.Errorf("inspect credential directory: %w", err)
	}
	if !dirInfo.IsDir() {
		return nil, errors.New("credential directory must not be a symbolic link")
	}
	if err := privatefs.Restrict(dir, true); err != nil {
		return nil, fmt.Errorf("restrict credential directory: %w", err)
	}
	keyPath := filepath.Join(dir, "key.bin")
	key, err := readOrCreateLocalKey(keyPath)
	if err != nil {
		return nil, err
	}
	return &localBackend{dir: dir, key: key}, nil
}

func readOrCreateLocalKey(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err == nil {
		if !info.Mode().IsRegular() {
			return nil, errors.New("credential key must be a regular file")
		}
		if err := privatefs.Restrict(path, false); err != nil {
			return nil, fmt.Errorf("restrict credential key: %w", err)
		}
		key, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read credential key: %w", err)
		}
		if len(key) != 32 {
			clear(key)
			return nil, errors.New("credential key has an invalid size")
		}
		return key, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("inspect credential key: %w", err)
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, fmt.Errorf("generate credential key: %w", err)
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, os.ErrExist) {
		clear(key)
		return readOrCreateLocalKey(path)
	}
	if err != nil {
		clear(key)
		return nil, fmt.Errorf("create credential key: %w", err)
	}
	if _, err = file.Write(key); err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		clear(key)
		_ = os.Remove(path)
		return nil, fmt.Errorf("write credential key: %w", err)
	}
	return key, nil
}

func (backend *localBackend) Set(account string, secret []byte) error {
	backend.mu.Lock()
	defer backend.mu.Unlock()
	block, err := aes.NewCipher(backend.key)
	if err != nil {
		return err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return err
	}
	sealed := aead.Seal(nil, nonce, secret, []byte(account))
	data := make([]byte, 0, len(localCredentialFileMagic)+len(nonce)+len(sealed))
	data = append(data, localCredentialFileMagic...)
	data = append(data, nonce...)
	data = append(data, sealed...)
	defer clear(data)
	return backend.write(account, data)
}

func (backend *localBackend) Get(account string) ([]byte, error) {
	backend.mu.Lock()
	defer backend.mu.Unlock()
	path := backend.accountPath(account)
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("credential entry must be a regular file")
	}
	maxFileSize := int64(len(localCredentialFileMagic) + 12 + maxSecretChunkSize + 16)
	if info.Size() > maxFileSize {
		return nil, errors.New("credential entry is too large")
	}
	if err := privatefs.Restrict(path, false); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	defer clear(data)
	block, err := aes.NewCipher(backend.key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	headerSize := len(localCredentialFileMagic) + aead.NonceSize()
	if len(data) < headerSize+aead.Overhead() || string(data[:len(localCredentialFileMagic)]) != localCredentialFileMagic {
		return nil, errors.New("credential entry is malformed")
	}
	nonce := data[len(localCredentialFileMagic):headerSize]
	secret, err := aead.Open(nil, nonce, data[headerSize:], []byte(account))
	if err != nil {
		return nil, errors.New("credential entry could not be decrypted")
	}
	return secret, nil
}

func (backend *localBackend) Delete(account string) error {
	backend.mu.Lock()
	defer backend.mu.Unlock()
	err := os.Remove(backend.accountPath(account))
	if errors.Is(err, os.ErrNotExist) {
		return ErrNotFound
	}
	return err
}

func (backend *localBackend) accountPath(account string) string {
	digest := sha256.Sum256([]byte(account))
	return filepath.Join(backend.dir, hex.EncodeToString(digest[:])+".enc")
}

func (backend *localBackend) write(account string, data []byte) error {
	temporary, err := os.CreateTemp(backend.dir, ".credential-*")
	if err != nil {
		return err
	}
	name := temporary.Name()
	defer os.Remove(name)
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return err
	}
	if _, err := io.Copy(temporary, bytes.NewReader(data)); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(name, backend.accountPath(account)); err != nil {
		return err
	}
	return nil
}

type migratingBackend struct {
	local  *localBackend
	legacy Backend
	mu     sync.Mutex
}

func (backend *migratingBackend) Set(account string, secret []byte) error {
	return backend.local.Set(account, secret)
}

func (backend *migratingBackend) Get(account string) ([]byte, error) {
	backend.mu.Lock()
	defer backend.mu.Unlock()
	secret, err := backend.local.Get(account)
	if !errors.Is(err, ErrNotFound) {
		return secret, err
	}
	secret, err = backend.legacy.Get(account)
	if err != nil {
		return nil, err
	}
	if err := backend.local.Set(account, secret); err != nil {
		clear(secret)
		return nil, err
	}
	return secret, nil
}

func (backend *migratingBackend) Delete(account string) error {
	return backend.local.Delete(account)
}
