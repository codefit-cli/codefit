package cli

import (
	"strings"
	"testing"
)

func TestRenderVersion(t *testing.T) {
	out := renderVersion("0.1.0", "abc1234", "2026-06-21T00:00:00Z")
	for _, want := range []string{"0.1.0", "abc1234", "2026-06-21T00:00:00Z"} {
		if !strings.Contains(out, want) {
			t.Errorf("version output missing %q:\n%s", want, out)
		}
	}
}
