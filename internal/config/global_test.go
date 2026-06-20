package config_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/codefit-cli/codefit/internal/config"
)

func TestLoadGlobalMissingReturnsEmpty(t *testing.T) {
	g, err := config.LoadGlobal(filepath.Join(t.TempDir(), "nope.yaml"))
	if err != nil {
		t.Fatalf("LoadGlobal on missing file should not error, got: %v", err)
	}
	if g.Provider != "" || g.Model != "" {
		t.Errorf("missing global config should be empty, got %+v", g)
	}
}

func TestSaveAndLoadGlobalRoundTrip(t *testing.T) {
	p := filepath.Join(t.TempDir(), "sub", "config.yaml")
	want := &config.GlobalConfig{Provider: "anthropic", Model: "claude-sonnet-4-6"}
	if err := config.SaveGlobal(p, want); err != nil {
		t.Fatalf("SaveGlobal: %v", err)
	}
	got, err := config.LoadGlobal(p)
	if err != nil {
		t.Fatalf("LoadGlobal: %v", err)
	}
	if got.Provider != want.Provider || got.Model != want.Model {
		t.Errorf("round trip = %+v, want %+v", got, want)
	}
}

func TestSaveGlobalUses0600(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.yaml")
	if err := config.SaveGlobal(p, &config.GlobalConfig{Provider: "groq", Model: "x"}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != fs.FileMode(0o600) {
		t.Errorf("global config perms = %o, want 600", perm)
	}
}

func TestGlobalValidateProvider(t *testing.T) {
	if err := (&config.GlobalConfig{Provider: "anthropic"}).Validate(); err != nil {
		t.Errorf("anthropic should be valid: %v", err)
	}
	if err := (&config.GlobalConfig{Provider: "ollama"}).Validate(); err != nil {
		t.Errorf("ollama should be valid: %v", err)
	}
	if err := (&config.GlobalConfig{Provider: "skynet"}).Validate(); err == nil {
		t.Error("unknown provider should be rejected")
	}
}

func TestGlobalConfigPathHonorsXDG(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	got, err := config.GlobalConfigPath()
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(dir, "codefit", "config.yaml")
	if got != want {
		t.Errorf("GlobalConfigPath() = %q, want %q", got, want)
	}
}
