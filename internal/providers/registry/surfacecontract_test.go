package registry_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/codefit-cli/codefit/internal/providers"
	"github.com/codefit-cli/codefit/internal/providers/registry"
)

// TestSurfaceProducers_EmitNonNilStructuralFacts is the cross-provider contract
// gate (D1): every registered LanguageProvider that declares ANY surface
// category (Capability().Surface non-empty) MUST emit SurfaceItem.StructuralFacts
// as a non-nil map, so the field marshals to a JSON OBJECT
// ("structural_facts":{...}), never `null` — the exact wire-form regression that
// broke codefit-scan-security/codefit-surface-authz for Go. The obligation is
// driven by registry.All(), never a hardcoded provider list: a future provider
// that declares surface and ships no fixture at testdata/surface/<canonical><ext>
// fails loudly here, naming the exact path to add, instead of silently skipping
// the gate the way a per-provider test would let it.
func TestSurfaceProducers_EmitNonNilStructuralFacts(t *testing.T) {
	for _, e := range registry.All() {
		p := e.New(nil)
		if len(p.Capability().Surface) == 0 {
			continue // declares no surface category — nothing to enforce here
		}
		t.Run(e.Canonical, func(t *testing.T) {
			exts := p.FileExtensions()
			if len(exts) == 0 {
				t.Fatalf("%s: FileExtensions() is empty, cannot resolve its fixture path", e.Canonical)
			}
			path := filepath.Join("testdata", "surface", e.Canonical+exts[0])
			content, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("%s declares a surface category (%v) but has no fixture at %s — "+
					"add one that produces >=1 surface item: %v", e.Canonical, p.Capability().Surface, path, err)
			}
			items, err := p.AnalyzeSurface(providers.SourceFile{Path: path, Content: content})
			if err != nil {
				t.Fatalf("%s: AnalyzeSurface(%s): %v", e.Canonical, path, err)
			}
			if len(items) == 0 {
				t.Fatalf("%s: fixture %s produced ZERO surface items — a fixture producing nothing "+
					"proves nothing; make it produce at least one", e.Canonical, path)
			}
			for _, it := range items {
				if it.StructuralFacts == nil {
					t.Errorf("%s: surface item %+v has a NIL StructuralFacts map — it marshals to "+
						"JSON null instead of {}", e.Canonical, it)
				}
				raw, err := json.Marshal(it)
				if err != nil {
					t.Fatalf("%s: json.Marshal(item): %v", e.Canonical, err)
				}
				if !strings.Contains(string(raw), `"structural_facts":{`) {
					t.Errorf("%s: wire form has no object structural_facts — got %s", e.Canonical, raw)
				}
			}
		})
	}
}
