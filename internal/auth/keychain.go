package auth

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/denisbrodbeck/machineid"
	"github.com/zalando/go-keyring"
)

// ErrNotFound is returned by a Store when no credential exists for a provider.
var ErrNotFound = errors.New("credential not found")

// keyringService is the service name codefit uses in the OS keychain.
const keyringService = "codefit"

// NewStore returns the best available credential store: the OS keychain when
// reachable, otherwise an AES-256-GCM encrypted file fallback (PRD §11).
func NewStore() Store {
	if keyringAvailable() {
		return keyringStore{}
	}
	fs, err := newFileStore()
	if err != nil {
		// As a last resort keep the keyring store; its calls will surface a
		// clear error to the user.
		return keyringStore{}
	}
	return fs
}

// keyringAvailable probes the OS keychain by writing and removing a tiny entry.
func keyringAvailable() bool {
	const probeAccount = "__codefit_probe__"
	if err := keyring.Set(keyringService, probeAccount, "1"); err != nil {
		return false
	}
	_ = keyring.Delete(keyringService, probeAccount)
	return true
}

// keyringStore stores credentials in the OS keychain, one entry per provider.
type keyringStore struct{}

func (keyringStore) Set(provider, apiKey string) error {
	if err := keyring.Set(keyringService, provider, apiKey); err != nil {
		return fmt.Errorf("saving %q credential to keychain: %w", provider, err)
	}
	return nil
}

func (keyringStore) Get(provider string) (string, error) {
	v, err := keyring.Get(keyringService, provider)
	if errors.Is(err, keyring.ErrNotFound) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("reading %q credential from keychain: %w", provider, err)
	}
	return v, nil
}

func (keyringStore) Delete(provider string) error {
	err := keyring.Delete(keyringService, provider)
	if errors.Is(err, keyring.ErrNotFound) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("deleting %q credential from keychain: %w", provider, err)
	}
	return nil
}

// fileStore is the encrypted-file fallback. All provider keys live in a single
// AES-256-GCM blob keyed by a machine-derived key, with 0600 permissions.
type fileStore struct {
	path string
	key  []byte // 32 bytes
}

func newFileStore() (*fileStore, error) {
	key, err := deriveKey()
	if err != nil {
		return nil, err
	}
	path, err := credentialsPath()
	if err != nil {
		return nil, err
	}
	return newFileStoreWithKey(path, key), nil
}

// newFileStoreWithKey builds a fileStore with an explicit path and key (used by
// tests to inject a known key and a temp path).
func newFileStoreWithKey(path string, key []byte) *fileStore {
	return &fileStore{path: path, key: key}
}

func (s *fileStore) Set(provider, apiKey string) error {
	m, err := s.load()
	if err != nil {
		return err
	}
	m[provider] = apiKey
	return s.save(m)
}

func (s *fileStore) Get(provider string) (string, error) {
	m, err := s.load()
	if err != nil {
		return "", err
	}
	v, ok := m[provider]
	if !ok {
		return "", ErrNotFound
	}
	return v, nil
}

func (s *fileStore) Delete(provider string) error {
	m, err := s.load()
	if err != nil {
		return err
	}
	if _, ok := m[provider]; !ok {
		return ErrNotFound
	}
	delete(m, provider)
	return s.save(m)
}

func (s *fileStore) load() (map[string]string, error) {
	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return map[string]string{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading credentials file: %w", err)
	}
	plain, err := decrypt(s.key, data)
	if err != nil {
		return nil, fmt.Errorf("decrypting credentials file: %w", err)
	}
	m := map[string]string{}
	if err := json.Unmarshal(plain, &m); err != nil {
		return nil, fmt.Errorf("parsing credentials file: %w", err)
	}
	return m, nil
}

func (s *fileStore) save(m map[string]string) error {
	plain, err := json.Marshal(m)
	if err != nil {
		return fmt.Errorf("encoding credentials: %w", err)
	}
	ct, err := encrypt(s.key, plain)
	if err != nil {
		return fmt.Errorf("encrypting credentials: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return fmt.Errorf("creating credentials dir: %w", err)
	}
	if err := os.WriteFile(s.path, ct, 0o600); err != nil {
		return fmt.Errorf("writing credentials file: %w", err)
	}
	return nil
}

// encrypt seals plaintext with AES-256-GCM, prepending the random nonce.
func encrypt(key, plaintext []byte) ([]byte, error) {
	gcm, err := newGCM(key)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("generating nonce: %w", err)
	}
	return gcm.Seal(nonce, nonce, plaintext, nil), nil
}

// decrypt opens an AES-256-GCM blob produced by encrypt.
func decrypt(key, ciphertext []byte) ([]byte, error) {
	gcm, err := newGCM(key)
	if err != nil {
		return nil, err
	}
	ns := gcm.NonceSize()
	if len(ciphertext) < ns {
		return nil, errors.New("ciphertext too short")
	}
	nonce, data := ciphertext[:ns], ciphertext[ns:]
	plain, err := gcm.Open(nil, nonce, data, nil)
	if err != nil {
		return nil, fmt.Errorf("authenticating ciphertext: %w", err)
	}
	return plain, nil
}

func newGCM(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("creating cipher: %w", err)
	}
	return cipher.NewGCM(block)
}

// deriveKey derives a stable 32-byte key from a machine-specific identifier so
// the fallback file is bound to this machine.
func deriveKey() ([]byte, error) {
	id, err := machineid.ProtectedID("codefit")
	if err != nil {
		// Fall back to the hostname; still machine-bound enough for the
		// fallback path (the keychain is preferred anyway).
		host, herr := os.Hostname()
		if herr != nil {
			return nil, fmt.Errorf("deriving machine key: %w", err)
		}
		id = host
	}
	sum := sha256.Sum256([]byte("codefit-cred-v1:" + id))
	return sum[:], nil
}

// credentialsPath is ~/.config/codefit/credentials.
func credentialsPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("resolving user config dir: %w", err)
	}
	return filepath.Join(dir, "codefit", "credentials"), nil
}
