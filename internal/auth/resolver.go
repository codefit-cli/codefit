package auth

import (
	"fmt"
	"os"
)

// CredentialResolver resolves a provider's API key following the priority
// order env var → keychain → error (PRD §11). Local providers (Ollama, LM
// Studio) have no key — their connection URL comes from the global config.
func Resolve(store Store, provider string) (string, error) {
	if env := envVarFor(provider); env != "" {
		if v := os.Getenv(env); v != "" {
			return v, nil
		}
	}
	// Generic override, useful in CI or with custom providers.
	if v := os.Getenv("CODEFIT_API_KEY"); v != "" {
		return v, nil
	}
	key, err := store.Get(provider)
	if err != nil {
		hint := "CODEFIT_API_KEY"
		if env := envVarFor(provider); env != "" {
			hint = env
		}
		return "", fmt.Errorf("no API key for %q: set %s or run `codefit auth login`: %w",
			provider, hint, err)
	}
	return key, nil
}

// envVarFor returns the conventional API-key environment variable for a cloud
// provider, or "" for local providers and unknown names.
func envVarFor(provider string) string {
	switch provider {
	case "anthropic":
		return "ANTHROPIC_API_KEY"
	case "openai":
		return "OPENAI_API_KEY"
	case "google":
		return "GOOGLE_API_KEY"
	case "groq":
		return "GROQ_API_KEY"
	case "openrouter":
		return "OPENROUTER_API_KEY"
	default:
		return ""
	}
}
