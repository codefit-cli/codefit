package baseline_test

import (
	"path/filepath"
	"testing"

	"github.com/codefit-cli/codefit/internal/core/baseline"
	"github.com/codefit-cli/codefit/internal/core/scope"
)

func TestRegisterAuthzHelper(t *testing.T) {
	b := &baseline.Baseline{Version: "1"}

	added, err := b.RegisterAuthzHelper("requirePermission", "typescript", "project RBAC wrapper", "2026-06-28")
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	if !added {
		t.Fatalf("first registration must report added=true")
	}
	if len(b.AuthzHelpers) != 1 || b.AuthzHelpers[0].By != "human" {
		t.Fatalf("helper must be recorded by:human, got %+v", b.AuthzHelpers)
	}

	// Idempotent: same (language, name) is a no-op.
	added, err = b.RegisterAuthzHelper("requirePermission", "typescript", "again", "2026-06-29")
	if err != nil || added {
		t.Errorf("re-registering must be a no-op (added=false, no error), got added=%v err=%v", added, err)
	}
	if len(b.AuthzHelpers) != 1 {
		t.Errorf("re-registering must not duplicate, got %d", len(b.AuthzHelpers))
	}
}

func TestRegisterAuthzHelper_RequiresFields(t *testing.T) {
	b := &baseline.Baseline{Version: "1"}
	if _, err := b.RegisterAuthzHelper("", "typescript", "r", "d"); err == nil {
		t.Error("empty name must error")
	}
	if _, err := b.RegisterAuthzHelper("x", "", "r", "d"); err == nil {
		t.Error("empty language must error")
	}
	if _, err := b.RegisterAuthzHelper("x", "typescript", "  ", "d"); err == nil {
		t.Error("empty reason must error (a human's justification is mandatory, like Accept)")
	}
}

func TestRecognizedAuthzHelpers_ByLanguage(t *testing.T) {
	b := &baseline.Baseline{Version: "1"}
	_, _ = b.RegisterAuthzHelper("requirePermission", "typescript", "r", "d")
	_, _ = b.RegisterAuthzHelper("getSalonId", "typescript", "r", "d")
	_, _ = b.RegisterAuthzHelper("requireRole", "go", "r", "d")

	ts := b.RecognizedAuthzHelpers("typescript")
	if len(ts) != 2 {
		t.Fatalf("want 2 typescript helpers, got %v", ts)
	}
	if got := b.RecognizedAuthzHelpers("go"); len(got) != 1 || got[0] != "requireRole" {
		t.Errorf("language scoping broken, got %v", got)
	}
}

func TestUnregisterAuthzHelper(t *testing.T) {
	b := &baseline.Baseline{Version: "1"}
	_, _ = b.RegisterAuthzHelper("requirePermission", "typescript", "r", "d")

	if !b.UnregisterAuthzHelper("requirePermission", "typescript") {
		t.Fatalf("unregister must remove and report true")
	}
	if len(b.AuthzHelpers) != 0 {
		t.Errorf("helper must be gone, got %+v", b.AuthzHelpers)
	}
	if b.UnregisterAuthzHelper("requirePermission", "typescript") {
		t.Errorf("unregistering an absent helper must report false")
	}
}

// Registered helpers survive a scan's Save/Load round-trip and the Diff that
// produces the next baseline (project knowledge, not a per-scan observation).
func TestAuthzHelpers_SurviveSaveLoadAndDiff(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, baseline.Name)
	b := &baseline.Baseline{Version: "1"}
	_, _ = b.RegisterAuthzHelper("requirePermission", "typescript", "project RBAC", "2026-06-28")
	if err := b.Save(path); err != nil {
		t.Fatal(err)
	}

	loaded, err := baseline.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := loaded.RecognizedAuthzHelpers("typescript"); len(got) != 1 {
		t.Fatalf("helper lost across Save/Load, got %v", got)
	}

	// A scan diff must carry the helpers into the next baseline, even though they
	// are not among the observed items.
	diff := baseline.Diff(loaded, []baseline.Observed{
		{FP: "abc123", Category: "idor", File: "app/x/route.ts", Snippet: "GET", Affirms: false},
	}, secScope(), scope.Full())
	if got := diff.Next.RecognizedAuthzHelpers("typescript"); len(got) != 1 {
		t.Errorf("Diff dropped the registered helpers from the next baseline, got %v", got)
	}
}
