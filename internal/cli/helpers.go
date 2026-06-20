package cli

import (
	"os"

	"github.com/codefit-cli/codefit/internal/auth"
	"github.com/codefit-cli/codefit/internal/config"
	"github.com/codefit-cli/codefit/internal/sandbox"
)

// loadGlobalConfig resolves the global config path and loads it (an empty
// config when the file does not exist yet).
func loadGlobalConfig() (string, *config.GlobalConfig, error) {
	path, err := config.GlobalConfigPath()
	if err != nil {
		return "", nil, err
	}
	g, err := config.LoadGlobal(path)
	if err != nil {
		return path, nil, err
	}
	return path, g, nil
}

// globalConfigPath returns the path to the per-user config file.
func globalConfigPath() (string, error) {
	return config.GlobalConfigPath()
}

// fileExists reports whether path exists and is readable.
func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// dockerAvailable reports whether Docker is available, via the sandbox manager
// (the single source of truth for Docker detection).
func dockerAvailable() bool {
	s, err := sandbox.NewSandbox()
	return err == nil && s.IsAvailable()
}

// credentialConfigured reports whether a usable credential exists for the
// provider, without revealing it. Local providers need a base URL rather than
// a key.
func credentialConfigured(g *config.GlobalConfig) bool {
	if g.Provider == "" {
		return false
	}
	if g.IsLocal() {
		return g.BaseURL != ""
	}
	_, err := auth.Resolve(auth.NewStore(), g.Provider)
	return err == nil
}
