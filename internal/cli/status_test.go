package cli

import (
	"strings"
	"testing"
)

func TestRenderSystemStatus(t *testing.T) {
	out := renderSystemStatus(systemStatus{
		Version:     "0.1.0-dev",
		ConfigFound: true,
		ConfigPath:  "./.codefit.yaml",
	})
	for _, want := range []string{"0.1.0-dev", "./.codefit.yaml"} {
		if !strings.Contains(out, want) {
			t.Errorf("status output missing %q:\n%s", want, out)
		}
	}
}

func TestRenderSystemStatusNoConfig(t *testing.T) {
	out := renderSystemStatus(systemStatus{Version: "x", ConfigFound: false, ConfigPath: "./.codefit.yaml"})
	if !strings.Contains(out, "no") {
		t.Errorf("status should report a missing config as 'no':\n%s", out)
	}
}
