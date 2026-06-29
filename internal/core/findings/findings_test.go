package findings_test

import (
	"encoding/json"
	"testing"

	"github.com/codefit-cli/codefit/internal/core/findings"
)

// TestSurfaceItemJSON locks the canonical JSON shape of a surface item
// (PRD section 11): id, category, file, line, snippet, structural_signals,
// reason_to_review.
func TestSurfaceItemJSON(t *testing.T) {
	item := findings.SurfaceItem{
		ID:                "abc123def456",
		Category:          "idor",
		File:              "src/routes/plants.ts",
		Line:              34,
		Snippet:           "router.get('/plants/:id', ...)",
		StructuralSignals: []string{"reads params.id", "accesses prisma.plant.findUnique"},
		StructuralFacts:   map[string]bool{"local_access_detected": true},
		ReasonToReview:    "Endpoint accesses a resource by ID; verify ownership.",
	}
	data, err := json.Marshal(item)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"id", "category", "file", "line", "snippet", "structural_signals", "structural_facts", "reason_to_review"} {
		if _, ok := m[key]; !ok {
			t.Errorf("surface item JSON missing key %q: %s", key, data)
		}
	}
}

// TestSurfaceItemIndirectAccess locks the Option C shape: when a handler reaches
// a resource through an external call (the Prisma access lives in another file,
// e.g. an Express controller delegating to a service), codefit emits the queryable
// fact indirect_access=true plus indirect_call naming the callee — it does NOT
// follow the call cross-file; the agent reasons over the named function.
func TestSurfaceItemIndirectAccess(t *testing.T) {
	indirect := findings.SurfaceItem{
		ID:              "def456abc789",
		Category:        "idor",
		File:            "src/controllers/article.controller.ts",
		Line:            110,
		Snippet:         "router.put('/articles/:slug', auth.required, ...)",
		StructuralFacts: map[string]bool{"local_access_detected": false, "indirect_access": true},
		IndirectCall:    "updateArticle",
		ReasonToReview:  "Endpoint reaches a resource by client-supplied slug through updateArticle; verify ownership.",
	}
	data, err := json.Marshal(indirect)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	if got, _ := m["indirect_call"].(string); got != "updateArticle" {
		t.Errorf("indirect_call = %q, want %q: %s", got, "updateArticle", data)
	}
	facts, _ := m["structural_facts"].(map[string]any)
	if ind, _ := facts["indirect_access"].(bool); !ind {
		t.Errorf("structural_facts.indirect_access = false, want true: %s", data)
	}

	// indirect_call is omitempty: a local access (the common case) must not emit it.
	local := findings.SurfaceItem{ID: "x", Category: "idor", File: "f.ts", Line: 1}
	data, err = json.Marshal(local)
	if err != nil {
		t.Fatal(err)
	}
	m = map[string]any{}
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	if _, ok := m["indirect_call"]; ok {
		t.Errorf("indirect_call must be omitted when empty: %s", data)
	}
}

// TestSensorResultCarriesSurface verifies a SensorResult serializes its surface
// list under the "surface" key alongside findings.
func TestSensorResultCarriesSurface(t *testing.T) {
	res := findings.SensorResult{
		Sensor:   "security",
		Findings: []findings.Finding{{ID: "SEC-001"}},
		Surface:  []findings.SurfaceItem{{Category: "authz", File: "h.go", Line: 1}},
	}
	data, err := json.Marshal(res)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	if _, ok := m["surface"]; !ok {
		t.Errorf("sensor result JSON missing 'surface': %s", data)
	}
}
