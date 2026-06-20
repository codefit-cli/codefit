package cli

import (
	"strings"
	"testing"
)

func TestRenderSystemStatus(t *testing.T) {
	s := systemStatus{
		Version:         "0.1.0-dev",
		Provider:        "anthropic",
		Model:           "claude-sonnet-4-6",
		KeyConfigured:   true,
		DockerAvailable: false,
		ConfigFound:     true,
		ConfigPath:      "./.codefit.yaml",
	}
	out := renderSystemStatus(s)
	for _, want := range []string{"0.1.0-dev", "anthropic", "claude-sonnet-4-6", "./.codefit.yaml"} {
		if !strings.Contains(out, want) {
			t.Errorf("status output missing %q:\n%s", want, out)
		}
	}
	// Docker is unavailable here -> must read "no".
	if !strings.Contains(strings.ToLower(out), "no") {
		t.Errorf("status output should report docker as unavailable:\n%s", out)
	}
}

func TestRenderAuthStatusNeverLeaksKey(t *testing.T) {
	// renderAuthStatus only takes a boolean, so by construction it cannot print
	// the key. Assert it reports the human-readable state instead.
	configured := renderAuthStatus("anthropic", "claude-sonnet-4-6", true)
	if !strings.Contains(strings.ToLower(configured), "configured") {
		t.Errorf("expected 'configured' state, got:\n%s", configured)
	}
	missing := renderAuthStatus("anthropic", "claude-sonnet-4-6", false)
	if !strings.Contains(strings.ToLower(missing), "not found") {
		t.Errorf("expected 'not found' state, got:\n%s", missing)
	}
}

func TestRenderAuthStatusShowsProviderAndModel(t *testing.T) {
	out := renderAuthStatus("groq", "llama-3.3-70b-versatile", true)
	if !strings.Contains(out, "groq") || !strings.Contains(out, "llama-3.3-70b-versatile") {
		t.Errorf("auth status should show provider and model:\n%s", out)
	}
}
