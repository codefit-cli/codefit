package auth

import (
	"bytes"
	"crypto/rand"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/zalando/go-keyring"
)

func TestEncryptDecryptRoundTrip(t *testing.T) {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}
	plaintext := []byte("encryption-roundtrip-sample-value")
	ct, err := encrypt(key, plaintext)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	if string(ct) == string(plaintext) {
		t.Fatal("ciphertext equals plaintext; not encrypted")
	}
	got, err := decrypt(key, ct)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}
	if string(got) != string(plaintext) {
		t.Errorf("decrypt = %q, want %q", got, plaintext)
	}
}

func TestDecryptWrongKeyFails(t *testing.T) {
	k1 := make([]byte, 32)
	k2 := make([]byte, 32)
	rand.Read(k1)
	rand.Read(k2)
	ct, _ := encrypt(k1, []byte("secret"))
	if _, err := decrypt(k2, ct); err == nil {
		t.Error("decrypt with wrong key should fail (GCM auth)")
	}
}

func TestFileStoreRoundTrip(t *testing.T) {
	key := make([]byte, 32)
	rand.Read(key)
	path := filepath.Join(t.TempDir(), "credentials")
	s := newFileStoreWithKey(path, key)

	if err := s.Set("anthropic", "sk-ant-1"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if err := s.Set("openai", "sk-oai-2"); err != nil {
		t.Fatalf("Set: %v", err)
	}

	got, err := s.Get("anthropic")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != "sk-ant-1" {
		t.Errorf("Get(anthropic) = %q, want sk-ant-1", got)
	}

	if _, err := s.Get("groq"); !errors.Is(err, ErrNotFound) {
		t.Errorf("Get(groq) error = %v, want ErrNotFound", err)
	}

	if err := s.Delete("anthropic"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := s.Get("anthropic"); !errors.Is(err, ErrNotFound) {
		t.Errorf("after Delete, Get(anthropic) error = %v, want ErrNotFound", err)
	}
	// openai must survive deleting anthropic.
	if got, _ := s.Get("openai"); got != "sk-oai-2" {
		t.Errorf("Get(openai) = %q, want sk-oai-2", got)
	}
}

func TestFileStoreCredentialsAreEncryptedAtRest(t *testing.T) {
	key := make([]byte, 32)
	rand.Read(key)
	path := filepath.Join(t.TempDir(), "credentials")
	s := newFileStoreWithKey(path, key)
	if err := s.Set("anthropic", "PLAINTEXT-CANARY-VALUE-1234"); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte("PLAINTEXT-CANARY-VALUE-1234")) {
		t.Error("credentials file contains the plaintext key; must be encrypted at rest")
	}
}

func TestKeyringStoreRoundTrip(t *testing.T) {
	keyring.MockInit()
	var s Store = keyringStore{}
	if err := s.Set("anthropic", "sk-ant-kr"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	got, err := s.Get("anthropic")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != "sk-ant-kr" {
		t.Errorf("Get = %q, want sk-ant-kr", got)
	}
	if _, err := s.Get("missing"); !errors.Is(err, ErrNotFound) {
		t.Errorf("Get(missing) = %v, want ErrNotFound", err)
	}
	if err := s.Delete("anthropic"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := s.Get("anthropic"); !errors.Is(err, ErrNotFound) {
		t.Errorf("after Delete = %v, want ErrNotFound", err)
	}
}
