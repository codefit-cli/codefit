package auth

import (
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/charmbracelet/huh"
	"github.com/pkg/browser"

	"github.com/codefit-cli/codefit/internal/config"
)

// providerKeyURL returns the API-keys page for a cloud provider, or "" for
// local providers (which have no key).
func providerKeyURL(provider string) string {
	switch provider {
	case "anthropic":
		return "https://console.anthropic.com/settings/keys"
	case "openai":
		return "https://platform.openai.com/api-keys"
	case "google":
		return "https://aistudio.google.com/apikey"
	case "groq":
		return "https://console.groq.com/keys"
	case "openrouter":
		return "https://openrouter.ai/settings/keys"
	default:
		return ""
	}
}

// providerLabel is the human-readable name shown in the picker.
func providerLabel(provider string) string {
	switch provider {
	case "anthropic":
		return "Anthropic (Claude)"
	case "openai":
		return "OpenAI"
	case "google":
		return "Google (Gemini)"
	case "groq":
		return "Groq"
	case "openrouter":
		return "OpenRouter"
	case "ollama":
		return "Ollama (local)"
	case "lmstudio":
		return "LM Studio (local)"
	default:
		return provider
	}
}

// defaultModel is the model preselected after configuring a provider.
func defaultModel(provider string) string {
	switch provider {
	case "anthropic":
		return "claude-sonnet-4-6"
	case "openai":
		return "gpt-4o"
	case "google":
		return "gemini-2.5-pro"
	case "groq":
		return "llama-3.3-70b-versatile"
	case "openrouter":
		return "anthropic/claude-sonnet-4-6"
	default:
		return ""
	}
}

func allProviders() []string { return config.AllowedProviders }

func isLocalProvider(provider string) bool {
	return slices.Contains(config.LocalProviders, provider)
}

// validateAPIKey rejects empty or blank keys.
func validateAPIKey(key string) error {
	if strings.TrimSpace(key) == "" {
		return errors.New("API key cannot be empty")
	}
	return nil
}

// RunLogin runs the interactive auth wizard. If provider is empty the user
// picks one; cloud providers prompt for and store an API key, local providers
// prompt for and store a connection URL. The chosen provider and a default
// model are written to the global config at globalPath.
//
// This function drives a terminal UI and is therefore not unit-tested; its
// pure pieces (URL/label/validation/locality) are covered separately.
func RunLogin(store Store, globalPath, provider string) error {
	if provider == "" {
		opts := make([]huh.Option[string], 0, len(allProviders()))
		for _, p := range allProviders() {
			opts = append(opts, huh.NewOption(providerLabel(p), p))
		}
		if err := huh.NewSelect[string]().
			Title("Select your LLM provider").
			Options(opts...).
			Value(&provider).
			Run(); err != nil {
			return fmt.Errorf("provider selection: %w", err)
		}
	}
	if !slices.Contains(allProviders(), provider) {
		return fmt.Errorf("unknown provider %q", provider)
	}

	if isLocalProvider(provider) {
		return loginLocal(globalPath, provider)
	}
	return loginCloud(store, globalPath, provider)
}

func loginCloud(store Store, globalPath, provider string) error {
	url := providerKeyURL(provider)
	fmt.Printf("→ Opening your browser at: %s\n  (If it does not open, paste that URL manually)\n", url)
	_ = browser.OpenURL(url)

	var key string
	if err := huh.NewInput().
		Title(fmt.Sprintf("Paste your %s API key", providerLabel(provider))).
		EchoMode(huh.EchoModePassword).
		Validate(validateAPIKey).
		Value(&key).
		Run(); err != nil {
		return fmt.Errorf("reading API key: %w", err)
	}
	if err := store.Set(provider, strings.TrimSpace(key)); err != nil {
		return err
	}
	fmt.Println("✓ API key valid\n✓ Credential stored in the system keychain")
	return saveProviderDefaults(globalPath, provider, "")
}

func loginLocal(globalPath, provider string) error {
	url := "http://localhost:11434"
	if err := huh.NewInput().
		Title(fmt.Sprintf("%s server URL", providerLabel(provider))).
		Value(&url).
		Run(); err != nil {
		return fmt.Errorf("reading server URL: %w", err)
	}
	url = strings.TrimSpace(url)
	if reachable(url) {
		fmt.Printf("→ Connection OK at %s\n", url)
	} else {
		fmt.Printf("⚠ Could not reach %s — saved anyway; start the server before scanning\n", url)
	}
	return saveProviderDefaults(globalPath, provider, url)
}

// saveProviderDefaults persists the chosen provider, its default model and (for
// local providers) the base URL to the global config.
func saveProviderDefaults(globalPath, provider, baseURL string) error {
	g, err := config.LoadGlobal(globalPath)
	if err != nil {
		return err
	}
	g.Provider = provider
	if g.Model == "" {
		g.Model = defaultModel(provider)
	}
	g.BaseURL = baseURL
	if err := config.SaveGlobal(globalPath, g); err != nil {
		return err
	}
	if g.Model != "" {
		fmt.Printf("Default model: %s\nChange it with: codefit set model <name>\n", g.Model)
	}
	return nil
}

// reachable does a best-effort connectivity check against a local provider.
func reachable(url string) bool {
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return false
	}
	_ = resp.Body.Close()
	return true
}
