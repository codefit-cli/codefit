package mcp_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/codefit-cli/codefit/internal/mcp"
)

// The scan path must treat a PRESENT but invalid .codefit.yaml as a hard error,
// not silently scan with no path_criticality. (copyFixture + fixture are defined
// in init_equiv_test.go, same package.)
func TestScanInvalidConfigFailsLoudly(t *testing.T) {
	root := copyFixture(t)
	writeCfg(t, root, "version: \"1\"\nproject:\n  name: \"x\"\n  language: \"typescript\"\n  framework: \"nextjs\"\n")

	_, err := mcp.HandleScanSecurity(mcp.ScanRequest{Root: root, Language: "typescript"})
	if err == nil {
		t.Fatal("scan with an invalid .codefit.yaml must fail, not scan silently")
	}
	if !strings.Contains(err.Error(), "framework") || !strings.Contains(err.Error(), "nextjs") {
		t.Errorf("scan error must explain the config problem, got: %v", err)
	}
}

// An ABSENT config is not an error — scanning falls back to defaults.
func TestScanAbsentConfigUsesDefaults(t *testing.T) {
	root := copyFixture(t) // the fixture ships no .codefit.yaml
	if _, err := mcp.HandleScanSecurity(mcp.ScanRequest{Root: root, Language: "typescript"}); err != nil {
		t.Fatalf("scan with no config must succeed using defaults, got: %v", err)
	}
}

// A VALID config scans normally.
func TestScanValidConfigScans(t *testing.T) {
	root := copyFixture(t)
	writeCfg(t, root, "version: \"1\"\nproject:\n  name: \"x\"\n  language: \"typescript\"\n  framework: \"next\"\n")
	if _, err := mcp.HandleScanSecurity(mcp.ScanRequest{Root: root, Language: "typescript"}); err != nil {
		t.Fatalf("scan with a valid config must succeed, got: %v", err)
	}
}

func writeCfg(t *testing.T, root, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, ".codefit.yaml"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
