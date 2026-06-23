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
	for _, key := range []string{"id", "category", "file", "line", "snippet", "structural_signals", "reason_to_review"} {
		if _, ok := m[key]; !ok {
			t.Errorf("surface item JSON missing key %q: %s", key, data)
		}
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
