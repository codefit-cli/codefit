package auth

import (
	"strings"
	"testing"
)

func TestProviderKeyURL(t *testing.T) {
	cases := map[string]string{
		"anthropic":  "https://console.anthropic.com/settings/keys",
		"openai":     "https://platform.openai.com/api-keys",
		"google":     "https://aistudio.google.com/apikey",
		"groq":       "https://console.groq.com/keys",
		"openrouter": "https://openrouter.ai/settings/keys",
	}
	for provider, want := range cases {
		if got := providerKeyURL(provider); got != want {
			t.Errorf("providerKeyURL(%q) = %q, want %q", provider, got, want)
		}
	}
}

func TestProviderKeyURLUnknown(t *testing.T) {
	if got := providerKeyURL("ollama"); got != "" {
		t.Errorf("providerKeyURL(ollama) = %q, want empty (local provider, no key URL)", got)
	}
}

func TestValidateAPIKey(t *testing.T) {
	if err := validateAPIKey("sk-ant-123"); err != nil {
		t.Errorf("non-empty key should be valid: %v", err)
	}
	if err := validateAPIKey("   "); err == nil {
		t.Error("blank key should be rejected")
	}
	if err := validateAPIKey(""); err == nil {
		t.Error("empty key should be rejected")
	}
}

func TestIsLocalProvider(t *testing.T) {
	if !isLocalProvider("ollama") || !isLocalProvider("lmstudio") {
		t.Error("ollama/lmstudio should be local")
	}
	if isLocalProvider("anthropic") {
		t.Error("anthropic should not be local")
	}
}

func TestProviderLabelsCoverAllProviders(t *testing.T) {
	// Every supported provider must have a human label for the picker.
	for _, p := range allProviders() {
		if lbl := providerLabel(p); lbl == "" || strings.TrimSpace(lbl) == "" {
			t.Errorf("provider %q has no label", p)
		}
	}
}
