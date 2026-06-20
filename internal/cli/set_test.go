package cli

import (
	"testing"

	"github.com/codefit-cli/codefit/internal/config"
)

func TestApplyModelChangeModelOnly(t *testing.T) {
	g := &config.GlobalConfig{Provider: "anthropic", Model: "claude-sonnet-4-6"}
	if err := applyModelChange(g, "claude-haiku-4-5", "", "", false); err != nil {
		t.Fatal(err)
	}
	if g.Model != "claude-haiku-4-5" {
		t.Errorf("Model = %q, want claude-haiku-4-5", g.Model)
	}
	if g.Provider != "anthropic" {
		t.Errorf("Provider changed unexpectedly to %q", g.Provider)
	}
}

func TestApplyModelChangeSwitchesProvider(t *testing.T) {
	g := &config.GlobalConfig{Provider: "anthropic", Model: "claude-sonnet-4-6"}
	if err := applyModelChange(g, "gpt-4o", "openai", "", false); err != nil {
		t.Fatal(err)
	}
	if g.Provider != "openai" || g.Model != "gpt-4o" {
		t.Errorf("got provider=%q model=%q, want openai/gpt-4o", g.Provider, g.Model)
	}
}

func TestApplyModelChangeLocalWithURL(t *testing.T) {
	g := &config.GlobalConfig{}
	if err := applyModelChange(g, "qwen3:30b", "ollama", "http://localhost:11434", true); err != nil {
		t.Fatal(err)
	}
	if g.Provider != "ollama" || g.BaseURL != "http://localhost:11434" {
		t.Errorf("got provider=%q url=%q, want ollama/localhost", g.Provider, g.BaseURL)
	}
}

func TestApplyModelChangeRejectsUnknownProvider(t *testing.T) {
	g := &config.GlobalConfig{}
	if err := applyModelChange(g, "x", "skynet", "", false); err == nil {
		t.Error("switching to an unknown provider should error")
	}
}
