package registry_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/codefit-cli/codefit/internal/providers"
	"github.com/codefit-cli/codefit/internal/providers/registry"
)

// stubEntryNew is a placeholder LanguageProvider constructor for entries built
// only to exercise ByMarkerFile's resolution logic — these tests never call
// New(), so a nil-returning stub is enough and avoids depending on a concrete
// provider package.
func stubEntryNew(_ []string) providers.LanguageProvider { return nil }

// TestByMarkerFile_SkipsEntryWithInitDetectFalse locks the fix for the defect
// verify's WARNING 1 found: Exposure.InitDetect was declared and documented
// but never consulted by ByMarkerFile/detectLanguage. An entry whose
// InitDetect is false must be exactly as unresolvable to codefit init as an
// entry whose SecurityScan is false already is to providerForLanguage.
func TestByMarkerFile_SkipsEntryWithInitDetectFalse(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module x\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	registry.SetTableForTest(t, []registry.Entry{
		{
			Canonical:   "go",
			MarkerFiles: []string{"go.mod"},
			New:         stubEntryNew,
			Exposure:    registry.Exposure{InitDetect: false},
		},
	})

	if _, ok := registry.ByMarkerFile(root); ok {
		t.Error("ByMarkerFile must skip an entry whose Exposure.InitDetect is false, but it resolved one")
	}
}

// TestByMarkerFile_SkippingIneligibleEntryPreservesOrderOfRemaining proves the
// ordering guarantee detect.go documents (go.mod-before-package.json
// priority) survives the InitDetect filter: skipping a disqualified entry
// must fall through to the next eligible one in table order, not fail the
// whole polyglot resolution just because the highest-priority marker matched
// an entry that init cannot detect.
func TestByMarkerFile_SkippingIneligibleEntryPreservesOrderOfRemaining(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "package.json"), []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	registry.SetTableForTest(t, []registry.Entry{
		{
			Canonical:   "go",
			MarkerFiles: []string{"go.mod"},
			New:         stubEntryNew,
			Exposure:    registry.Exposure{InitDetect: false},
		},
		{
			Canonical:   "typescript",
			MarkerFiles: []string{"package.json"},
			New:         stubEntryNew,
			Exposure:    registry.Exposure{InitDetect: true},
		},
	})

	e, ok := registry.ByMarkerFile(root)
	if !ok {
		t.Fatal("ByMarkerFile must still resolve the eligible entry when a higher-priority one is skipped")
	}
	if e.Canonical != "typescript" {
		t.Errorf("ByMarkerFile = %q, want %q (go is skipped for InitDetect:false, order falls through to typescript)", e.Canonical, "typescript")
	}
}
