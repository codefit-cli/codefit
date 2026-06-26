package mcp_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/codefit-cli/codefit/internal/mcp"
	"github.com/codefit-cli/codefit/internal/scaffold"
)

const fixture = "../scaffold/testdata/sample-next"

func copyFixture(t *testing.T) string {
	t.Helper()
	dst := t.TempDir()
	err := filepath.WalkDir(fixture, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(fixture, path)
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
	if err != nil {
		t.Fatalf("copyFixture: %v", err)
	}
	return dst
}

// buckets returns the sorted file lists per bucket, the comparable fingerprint of
// a scan-all run.
func buckets(t *testing.T, root string) (actionable, clean, frontier []string) {
	t.Helper()
	// Remove any baseline so each call is a first scan (everything new, nothing
	// silenced) — this test compares raw classification, not baseline filtering.
	_ = os.Remove(filepath.Join(root, ".codefit-baseline"))
	resp, err := mcp.HandleScanAll(mcp.ScanAllRequest{Root: root, Language: "typescript"})
	if err != nil {
		t.Fatalf("HandleScanAll(%s): %v", root, err)
	}
	for _, e := range resp.Actionable {
		actionable = append(actionable, e.File)
	}
	for _, e := range resp.ResolvedClean.Endpoints {
		clean = append(clean, e.File)
	}
	for _, e := range resp.FrontierPending.Endpoints {
		frontier = append(frontier, e.File)
	}
	sort.Strings(actionable)
	sort.Strings(clean)
	sort.Strings(frontier)
	return actionable, clean, frontier
}

// TestInitConfigDrivesScanAll closes the loop Lucas asked for: a config GENERATED
// by init must not merely parse — it must make scan-all find the real endpoints
// and split them into the three buckets. The fixture is built so each bucket has
// exactly one endpoint (users=actionable, profile=resolved_clean, feed=frontier).
func TestInitConfigDrivesScanAll(t *testing.T) {
	root := copyFixture(t)
	if _, err := scaffold.Generate(scaffold.Options{Root: root}); err != nil {
		t.Fatalf("Generate: %v", err)
	}

	actionable, clean, frontier := buckets(t, root)

	if len(actionable) != 1 || filepath.Base(filepath.Dir(actionable[0])) != "users" {
		t.Errorf("actionable = %v, want exactly app/users/route.ts", actionable)
	}
	if len(clean) != 1 || filepath.Base(filepath.Dir(clean[0])) != "profile" {
		t.Errorf("resolved_clean = %v, want exactly app/profile/route.ts", clean)
	}
	if len(frontier) != 1 || filepath.Base(filepath.Dir(frontier[0])) != "feed" {
		t.Errorf("frontier_pending = %v, want exactly app/feed/route.ts", frontier)
	}
}

// TestInitConfigFunctionallyEquivalentToHandWritten proves the generated config
// is not just valid but equivalent: scan-all produces the SAME buckets with the
// init-generated config as with a hand-written one. A config can round-trip yet
// have a wrong path that breaks scanning — this guards against that.
func TestInitConfigFunctionallyEquivalentToHandWritten(t *testing.T) {
	root := copyFixture(t)
	if _, err := scaffold.Generate(scaffold.Options{Root: root}); err != nil {
		t.Fatalf("Generate: %v", err)
	}
	genA, genC, genF := buckets(t, root)

	handWritten := `version: "1"
project:
  name: "sample-next"
  language: "typescript"
  framework: "next"
  path_criticality:
    production:
      - "app/**"
      - "src/**"
    test:
      - "**/*.test.ts"
      - "**/*.spec.ts"
database:
  orm: "prisma"
  type: "postgresql"
  paradigm: "oltp"
  schema_paths:
    - "prisma/schema.prisma"
`
	if err := os.WriteFile(filepath.Join(root, ".codefit.yaml"), []byte(handWritten), 0o644); err != nil {
		t.Fatal(err)
	}
	handA, handC, handF := buckets(t, root)

	if !equal(genA, handA) || !equal(genC, handC) || !equal(genF, handF) {
		t.Errorf("generated config is not functionally equivalent to hand-written:\n"+
			"  actionable: gen=%v hand=%v\n  clean: gen=%v hand=%v\n  frontier: gen=%v hand=%v",
			genA, handA, genC, handC, genF, handF)
	}
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
