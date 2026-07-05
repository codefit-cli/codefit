package mcp

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/codefit-cli/codefit/internal/core/baseline"
	"github.com/codefit-cli/codefit/internal/core/findings"
	"github.com/codefit-cli/codefit/internal/providers/typescript"
	"github.com/codefit-cli/codefit/internal/sensors"
	sensordb "github.com/codefit-cli/codefit/internal/sensors/db"
	"github.com/codefit-cli/codefit/internal/sensors/security"
)

// registeredSensors is the SINGLE list of every sensor codefit ships — the one
// place to register a new one. The category-overlap invariant iterates it, so a
// sensor added here is covered automatically; a sensor added WITHOUT being listed
// here is the only gap (Go cannot auto-discover interface implementers). The
// instances are for metadata (OwnedCategories / identity); the args are irrelevant
// because that metadata never touches the provider/parser.
func registeredSensors() []sensors.Sensor {
	return []sensors.Sensor{
		security.New(typescript.New()),
		sensordb.New(typescript.New()),
		// + review, complexity, tests, … as they are built — one line each.
	}
}

// AJUSTE 1: the whole scoping mechanism rests on each category belonging to ONE
// sensor. This iterates the full registry (not a hardcoded pair), so it fails
// loudly the moment any two registered sensors declare the same category.
func TestOwnedCategories_NoOverlap(t *testing.T) {
	owner := map[string]string{}
	for _, s := range registeredSensors() {
		for _, c := range s.OwnedCategories() {
			if other, dup := owner[c]; dup {
				t.Errorf("category %q is owned by both %q and %q — scoping requires disjoint ownership", c, other, s.Name())
			}
			owner[c] = s.Name()
		}
	}
	if len(owner) == 0 {
		t.Fatal("no owned categories declared")
	}
}

// observedFrom unions multiple sensor results, deduped by fingerprint.
func TestObservedFrom_Union(t *testing.T) {
	a := findings.SensorResult{Findings: []findings.Finding{
		{ID: "SEC-001", Dimension: findings.DimensionSecurity, Fingerprint: "fp1", File: "a.ts"},
	}}
	b := findings.SensorResult{Surface: []findings.SurfaceItem{
		{Category: "db-fk-no-index", Fingerprint: "fp2", File: "schema.prisma"},
		{Category: "idor", Fingerprint: "fp1", File: "a.ts"}, // duplicate fingerprint → deduped
	}}
	obs := observedFrom(a, b)
	if len(obs) != 2 {
		t.Errorf("observedFrom union = %d observations, want 2 (fp1 deduped)", len(obs))
	}
}

// A security-only prune must not remove an item owned by a sensor that did not run.
func TestPrune_ForeignItem_NotPruned(t *testing.T) {
	root := t.TempDir()
	must(t, os.WriteFile(filepath.Join(root, ".codefit.yaml"),
		[]byte("version: \"1\"\nproject:\n  name: t\n  language: typescript\n"), 0o644))
	must(t, os.WriteFile(filepath.Join(root, "a.ts"), []byte("export const x = 1;\n"), 0o644))

	bl := &baseline.Baseline{Version: "1", Items: []baseline.Item{
		{FP: "dbfp", Category: "db-fk-no-index", File: "schema.prisma"},
	}}
	must(t, bl.Save(filepath.Join(root, baseline.Name)))

	resp, err := HandleBaselinePrune(BaselinePruneRequest{Root: root, Language: "typescript", Fingerprints: []string{"dbfp"}})
	if err != nil {
		t.Fatalf("HandleBaselinePrune: %v", err)
	}
	if len(resp.Pruned) != 0 {
		t.Errorf("a security-only prune must not remove a foreign db item, pruned=%v", resp.Pruned)
	}
	after, err := baseline.Load(filepath.Join(root, baseline.Name))
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, it := range after.Items {
		if it.FP == "dbfp" {
			found = true
		}
	}
	if !found {
		t.Error("the foreign db item must survive in the baseline after a security-only prune")
	}
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
