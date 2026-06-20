package auth

import (
	"strings"
	"testing"
)

// fakeStore is an in-memory Store for resolver tests.
type fakeStore map[string]string

func (f fakeStore) Set(p, k string) error { f[p] = k; return nil }
func (f fakeStore) Get(p string) (string, error) {
	if v, ok := f[p]; ok {
		return v, nil
	}
	return "", ErrNotFound
}
func (f fakeStore) Delete(p string) error { delete(f, p); return nil }

func TestResolvePrefersEnvVar(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "env-key")
	store := fakeStore{"anthropic": "store-key"}
	got, err := Resolve(store, "anthropic")
	if err != nil {
		t.Fatal(err)
	}
	if got != "env-key" {
		t.Errorf("Resolve = %q, want env-key (env wins over keychain)", got)
	}
}

func TestResolveGenericOverride(t *testing.T) {
	t.Setenv("CODEFIT_API_KEY", "generic-key")
	got, err := Resolve(fakeStore{}, "groq")
	if err != nil {
		t.Fatal(err)
	}
	if got != "generic-key" {
		t.Errorf("Resolve = %q, want generic-key (CODEFIT_API_KEY override)", got)
	}
}

func TestResolveFallsBackToStore(t *testing.T) {
	got, err := Resolve(fakeStore{"openai": "store-key"}, "openai")
	if err != nil {
		t.Fatal(err)
	}
	if got != "store-key" {
		t.Errorf("Resolve = %q, want store-key", got)
	}
}

func TestResolveErrorsWhenMissing(t *testing.T) {
	_, err := Resolve(fakeStore{}, "anthropic")
	if err == nil {
		t.Fatal("Resolve should error when no key is found anywhere")
	}
	// Error should be actionable: name the env var and the login command.
	if !strings.Contains(err.Error(), "ANTHROPIC_API_KEY") || !strings.Contains(err.Error(), "auth login") {
		t.Errorf("error %q should mention the env var and `codefit auth login`", err)
	}
}

func TestEnvVarFor(t *testing.T) {
	cases := map[string]string{
		"anthropic":  "ANTHROPIC_API_KEY",
		"openai":     "OPENAI_API_KEY",
		"google":     "GOOGLE_API_KEY",
		"groq":       "GROQ_API_KEY",
		"openrouter": "OPENROUTER_API_KEY",
		"ollama":     "",
	}
	for provider, want := range cases {
		if got := envVarFor(provider); got != want {
			t.Errorf("envVarFor(%q) = %q, want %q", provider, got, want)
		}
	}
}
