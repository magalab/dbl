// Package credentials stores secrets in the operating system credential vault.
package credentials

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"github.com/egoist/mygo"
)

const maxSecretChunkSize = mygo.MaxSecretSize
const maxLogicalSecretSize = 64 << 10
const chunkManifestSize = 4 + 4 + sha256.Size

// MaxSecretSize is the maximum logical secret size accepted by the vault.
// Larger values are split into system-vault items no larger than MyGo's limit.
const MaxSecretSize = maxLogicalSecretSize

var (
	ErrInvalidReference = errors.New("invalid credential reference")
	ErrEmptySecret      = errors.New("credential secret is empty")
	ErrSecretTooLarge   = errors.New("credential secret exceeds 64 KiB supported size")
	ErrNotFound         = errors.New("credential was not found")
	ErrUnavailable      = errors.New("system credential vault is unavailable")
)

type Reference string

// Valid reports whether ref has this package's opaque reference format.
func (ref Reference) Valid() bool {
	_, err := accountFor(ref)
	return err == nil
}

type Backend interface {
	Set(account string, secret []byte) error
	Get(account string) ([]byte, error)
	Delete(account string) error
}

type Vault struct {
	backend Backend
}

type systemBackend struct{}

func NewSystem() *Vault {
	return New(systemBackend{})
}

func New(backend Backend) *Vault {
	return &Vault{backend: backend}
}

// Store writes a secret to the OS vault and returns a reference suitable for
// configuration files. Secret values are never included in returned errors.
func (vault *Vault) Store(secret []byte) (Reference, error) {
	if len(secret) == 0 {
		return "", ErrEmptySecret
	}
	if len(secret) > maxLogicalSecretSize {
		return "", ErrSecretTooLarge
	}
	if vault == nil || vault.backend == nil {
		return "", ErrUnavailable
	}
	version := "v1:"
	if len(secret) > maxSecretChunkSize {
		version = "v2:"
	}
	ref, err := newReference(version)
	if err != nil {
		return "", fmt.Errorf("create credential reference: %w", err)
	}
	account := string(ref)
	if version == "v1:" {
		if err := vault.backend.Set(account, secret); err != nil {
			// Some credential backends can report a late failure after creating the
			// item. Best-effort cleanup avoids leaving an unreachable secret behind.
			_ = vault.backend.Delete(account)
			return "", ErrUnavailable
		}
		return ref, nil
	}
	chunkCount := chunkCount(len(secret))
	for index := 0; index < chunkCount; index++ {
		start := index * maxSecretChunkSize
		end := min(start+maxSecretChunkSize, len(secret))
		if err := vault.backend.Set(chunkAccount(account, index), secret[start:end]); err != nil {
			vault.deleteChunks(account, chunkCount)
			return "", ErrUnavailable
		}
	}
	manifest := make([]byte, chunkManifestSize)
	copy(manifest[:4], "DBL2")
	binary.BigEndian.PutUint32(manifest[4:8], uint32(len(secret)))
	digest := sha256.Sum256(secret)
	copy(manifest[8:], digest[:])
	if err := vault.backend.Set(manifestAccount(account), manifest); err != nil {
		vault.deleteChunks(account, chunkCount)
		_ = vault.backend.Delete(manifestAccount(account))
		return "", ErrUnavailable
	}
	return ref, nil
}

func (vault *Vault) Load(ref Reference) ([]byte, error) {
	account, err := accountFor(ref)
	if err != nil {
		return nil, err
	}
	if vault == nil || vault.backend == nil {
		return nil, ErrUnavailable
	}
	if strings.HasPrefix(account, "v2:") {
		return vault.loadChunked(account)
	}
	secret, err := vault.backend.Get(account)
	if errors.Is(err, ErrNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, ErrUnavailable
	}
	if len(secret) > maxSecretChunkSize {
		clear(secret)
		return nil, ErrSecretTooLarge
	}
	return append([]byte(nil), secret...), nil
}

func (vault *Vault) Delete(ref Reference) error {
	account, err := accountFor(ref)
	if err != nil {
		return err
	}
	if vault == nil || vault.backend == nil {
		return ErrUnavailable
	}
	if strings.HasPrefix(account, "v2:") {
		return vault.deleteChunked(account)
	}
	if err := vault.backend.Delete(account); errors.Is(err, ErrNotFound) {
		return nil
	} else if err != nil {
		return ErrUnavailable
	}
	return nil
}

func newReference(version string) (Reference, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return Reference(version + hex.EncodeToString(raw[:])), nil
}

func accountFor(ref Reference) (string, error) {
	value := string(ref)
	version, id, ok := strings.Cut(value, ":")
	if !ok || (version != "v1" && version != "v2") || len(id) != 32 {
		return "", ErrInvalidReference
	}
	if _, err := hex.DecodeString(id); err != nil {
		return "", ErrInvalidReference
	}
	return value, nil
}

func chunkCount(length int) int { return (length + maxSecretChunkSize - 1) / maxSecretChunkSize }

func manifestAccount(account string) string { return account + ":manifest" }

func chunkAccount(account string, index int) string { return fmt.Sprintf("%s:%03d", account, index) }

func (vault *Vault) loadChunked(account string) ([]byte, error) {
	manifest, err := vault.backend.Get(manifestAccount(account))
	if errors.Is(err, ErrNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, ErrUnavailable
	}
	if len(manifest) != chunkManifestSize || string(manifest[:4]) != "DBL2" {
		return nil, ErrUnavailable
	}
	length := int(binary.BigEndian.Uint32(manifest[4:8]))
	if length <= maxSecretChunkSize || length > maxLogicalSecretSize {
		return nil, ErrUnavailable
	}
	count := chunkCount(length)
	secret := make([]byte, 0, length)
	defer clear(secret)
	for index := 0; index < count; index++ {
		chunk, err := vault.backend.Get(chunkAccount(account, index))
		if err != nil {
			return nil, ErrUnavailable
		}
		want := min(maxSecretChunkSize, length-len(secret))
		if len(chunk) != want {
			clear(chunk)
			return nil, ErrUnavailable
		}
		secret = append(secret, chunk...)
		clear(chunk)
	}
	digest := sha256.Sum256(secret)
	if subtle.ConstantTimeCompare(digest[:], manifest[8:]) != 1 {
		return nil, ErrUnavailable
	}
	return append([]byte(nil), secret...), nil
}

func (vault *Vault) deleteChunked(account string) error {
	manifest, err := vault.backend.Get(manifestAccount(account))
	if errors.Is(err, ErrNotFound) {
		return vault.deleteChunksResult(account, chunkCount(maxLogicalSecretSize))
	}
	if err != nil {
		return ErrUnavailable
	}
	length := 0
	if len(manifest) == chunkManifestSize && string(manifest[:4]) == "DBL2" {
		length = int(binary.BigEndian.Uint32(manifest[4:8]))
	}
	count := chunkCount(maxLogicalSecretSize)
	if length > maxSecretChunkSize && length <= maxLogicalSecretSize {
		count = chunkCount(length)
	}
	return vault.deleteChunksResult(account, count)
}

func (vault *Vault) deleteChunksResult(account string, count int) error {
	failed := false
	for index := 0; index < count; index++ {
		if err := vault.backend.Delete(chunkAccount(account, index)); err != nil && !errors.Is(err, ErrNotFound) {
			failed = true
		}
	}
	if failed {
		return ErrUnavailable
	}
	if err := vault.backend.Delete(manifestAccount(account)); err != nil && !errors.Is(err, ErrNotFound) {
		return ErrUnavailable
	}
	return nil
}

func (vault *Vault) deleteChunks(account string, count int) {
	for index := 0; index < count; index++ {
		_ = vault.backend.Delete(chunkAccount(account, index))
	}
}

func (systemBackend) Set(account string, secret []byte) error {
	return mygo.Secrets.Set(account, secret)
}

func (systemBackend) Get(account string) ([]byte, error) {
	value, err := mygo.Secrets.Get(account)
	if errors.Is(err, mygo.ErrSecretNotFound) {
		return nil, ErrNotFound
	}
	return value, err
}

func (systemBackend) Delete(account string) error {
	return mygo.Secrets.Delete(account)
}
