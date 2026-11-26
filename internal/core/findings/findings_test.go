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

// TestParseStatus_StatesAreDistinctAndTheZeroValueIsNeverComplete pins the three
// states a provider can report about a file's parse, and the one property the
// whole design leans on: a status nobody set — the zero value, or a field
// dropped on the way to a cache entry — reads as NOT REPORTED, never as
// complete. A zero value that meant "complete" would be a clean verdict nobody
// computed.
func TestParseStatus_StatesAreDistinctAndTheZeroValueIsNeverComplete(t *testing.T) {
	var zero findings.ParseStatus
	if zero.Reported || zero.Complete {
		t.Errorf("zero ParseStatus = %+v, want neither reported nor complete", zero)
	}
	// The zero value is also NOT reproducible: a status nobody vouched for is
	// never cached (ADR 0089).
	if zero.Reproducible {
		t.Errorf("zero ParseStatus = %+v, want not reproducible", zero)
	}
	if want := (findings.ParseStatus{Reproducible: true}); findings.ParseNotReported() != want {
		t.Errorf("ParseNotReported() = %+v, want %+v — not reported, and cached as before",
			findings.ParseNotReported(), want)
	}

	complete := findings.ParseComplete()
	if want := (findings.ParseStatus{Reported: true, Complete: true, Reproducible: true}); complete != want {
		t.Errorf("ParseComplete() = %+v, want %+v", complete, want)
	}

	incomplete := findings.ParseIncomplete(findings.ParseReasonMissingNode, 7)
	want := findings.ParseStatus{Reported: true, Complete: false, Reason: "missing_node", Line: 7, Reproducible: true}
	if incomplete != want {
		t.Errorf("ParseIncomplete(missing_node, 7) = %+v, want %+v", incomplete, want)
	}

	transient := findings.ParseIncompleteNotReproducible("stopped:timeout", 3)
	wantTransient := findings.ParseStatus{Reported: true, Complete: false, Reason: "stopped:timeout", Line: 3}
	if transient != wantTransient {
		t.Errorf("ParseIncompleteNotReproducible(stopped:timeout, 3) = %+v, want %+v", transient, wantTransient)
	}
}

// TestParseStatus_JSONShape locks the wire shape a cache entry stores: reported,
// complete and reproducible are always written (an absent field must not be
// read as a value), reason and line only when set.
func TestParseStatus_JSONShape(t *testing.T) {
	cases := []struct {
		name string
		in   findings.ParseStatus
		want string
	}{
		{"not reported", findings.ParseNotReported(), `{"reported":false,"complete":false,"reproducible":true}`},
		{"complete", findings.ParseComplete(), `{"reported":true,"complete":true,"reproducible":true}`},
		{"incomplete", findings.ParseIncomplete("stopped:memory_budget", 1),
			`{"reported":true,"complete":false,"reason":"stopped:memory_budget","line":1,"reproducible":true}`},
		{"incomplete, not reproducible", findings.ParseIncompleteNotReproducible("stopped:memory_budget", 1),
			`{"reported":true,"complete":false,"reason":"stopped:memory_budget","line":1,"reproducible":false}`},
		{"zero value", findings.ParseStatus{}, `{"reported":false,"complete":false,"reproducible":false}`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			data, err := json.Marshal(c.in)
			if err != nil {
				t.Fatal(err)
			}
			if string(data) != c.want {
				t.Errorf("JSON = %s, want %s", data, c.want)
			}
		})
	}
}
