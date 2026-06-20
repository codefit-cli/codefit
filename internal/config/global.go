package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"

	"gopkg.in/yaml.v3"
)

// AllowedProviders is the set of LLM providers codefit can authenticate with.
var AllowedProviders = []string{
	"anthropic", "openai", "google", "groq", "openrouter", "ollama", "lmstudio",
}

// LocalProviders are providers served locally: they need a base URL instead of
// an API key.
var LocalProviders = []string{"ollama", "lmstudio"}

// GlobalConfig is the per-user configuration at ~/.config/codefit/config.yaml.
// It never stores credentials — API keys live in the OS keychain (PRD §11).
type GlobalConfig struct {
	Provider string `yaml:"provider"`
	Model    string `yaml:"model"`
	// BaseURL is the endpoint for local providers (Ollama, LM Studio); empty
	// for cloud providers.
	BaseURL string `yaml:"base_url,omitempty"`
}

// IsLocal reports whether the configured provider is a local one.
func (g *GlobalConfig) IsLocal() bool {
	return slices.Contains(LocalProviders, g.Provider)
}

// Validate checks that the provider, when set, is one codefit supports.
func (g *GlobalConfig) Validate() error {
	if g.Provider != "" && !slices.Contains(AllowedProviders, g.Provider) {
		return fmt.Errorf("invalid provider %q (allowed: %v)", g.Provider, AllowedProviders)
	}
	return nil
}

// GlobalConfigPath returns the path to the per-user config, honoring
// XDG_CONFIG_HOME via os.UserConfigDir.
func GlobalConfigPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("resolving user config dir: %w", err)
	}
	return filepath.Join(dir, "codefit", "config.yaml"), nil
}

// LoadGlobal reads the per-user config. A missing file is not an error: it
// returns an empty config so the first run works before `auth login`.
func LoadGlobal(path string) (*GlobalConfig, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return &GlobalConfig{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading global config %q: %w", path, err)
	}
	var g GlobalConfig
	if err := yaml.Unmarshal(data, &g); err != nil {
		return nil, fmt.Errorf("parsing global config %q: %w", path, err)
	}
	return &g, nil
}

// SaveGlobal writes the per-user config, creating the directory and using 0600
// permissions (the file lives next to credentials and should not be world
// readable).
func SaveGlobal(path string, g *GlobalConfig) error {
	if err := g.Validate(); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("creating config dir: %w", err)
	}
	data, err := yaml.Marshal(g)
	if err != nil {
		return fmt.Errorf("marshaling global config: %w", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("writing global config %q: %w", path, err)
	}
	return nil
}
