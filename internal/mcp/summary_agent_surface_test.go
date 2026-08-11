package mcp_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/codefit-cli/codefit/internal/mcp"
	"github.com/codefit-cli/codefit/internal/scaffold"
)

// TestSummaryShapeIsTaughtOnBothAgentFacingSurfaces is the drift lock between
// the summary's WIRE SHAPE and the two things an agent reads before it ever
// calls the tool: codefit-scan-all's description (internal/mcp/server.go) and
// the skill codefit generates (internal/scaffold/skill.go).
//
// Why a reflect-driven lock rather than three assertions on three strings: the
// sub-block names are derived from ScanAllSummary's own JSON tags, so a FUTURE
// dimension added to the response without being taught fails here too. The
// pattern is core/surface/vocabulary_test.go's — assert against the declaration
// itself, never against a second hand-written list, because a second list is
// one more place to drift.
//
// The existing locks in skill_tools_test.go do not reach this:
// TestSkillNamesEveryRegisteredTool matches tool NAMES only. Nothing in this
// repo asserted anything about `summary` on either surface before this test —
// grep for it in skill.go returned zero matches, which is precisely how the
// summary came to count one dimension while describing itself as the response's
// summary.
//
// Mutation that MUST turn this red: delete the summary.db sentence from either
// surface.
func TestSummaryShapeIsTaughtOnBothAgentFacingSurfaces(t *testing.T) {
	keys := summarySubBlockKeys(t)
	if len(keys) < 2 {
		t.Fatalf("ScanAllSummary declares %d dimension sub-block(s) (%v) — the probe itself is broken; "+
			"this lock is meaningless with fewer than two", len(keys), keys)
	}

	skill, err := scaffold.RenderSkill(scaffold.ProjectInfo{Name: "x", Language: "typescript"})
	if err != nil {
		t.Fatalf("RenderSkill: %v", err)
	}

	surfaces := map[string]string{
		"the codefit-scan-all tool description": toolDescription(t, string(mcp.ToolScanAll)),
		"the generated skill":                   string(skill),
	}

	for name, body := range surfaces {
		if body == "" {
			t.Fatalf("%s is empty — the probe itself is broken", name)
		}
		// Every dimension sub-block is named, qualified: "summary.db", not a
		// bare "db" that already appears in both texts for other reasons.
		for _, key := range keys {
			want := "summary." + key
			if !strings.Contains(body, want) {
				t.Errorf("%s never mentions %q. An agent reading only this surface cannot tell whether "+
					"`summary` counts that dimension, which is the question this change exists to answer.\n"+
					"Teach it in internal/mcp/server.go and internal/scaffold/skill.go.", name, want)
			}
		}
		// The roll-up, and the one sentence that makes a null readable.
		if !strings.Contains(body, "summary.totals") {
			t.Errorf("%s never mentions summary.totals — the cross-dimension number an agent skims", name)
		}
		if !mentionsNullMeansNotMeasured(body) {
			t.Errorf("%s does not teach that a null summary sub-block means NOT MEASURED. Without that "+
				"sentence a reader has no way to distinguish it from zero, and a zero that means "+
				"'nobody looked' is the defect this shape removes.", name)
		}
	}
}

// summarySubBlockKeys reads the JSON tag of every POINTER field of
// ScanAllSummary — the per-dimension sub-blocks, which are pointers precisely
// so they can be null. Value fields (totals, note) are asserted separately
// because they are never null and carry a different obligation.
func summarySubBlockKeys(t *testing.T) []string {
	t.Helper()
	typ := reflect.TypeOf(mcp.ScanAllSummary{})
	var keys []string
	for i := 0; i < typ.NumField(); i++ {
		f := typ.Field(i)
		if f.Type.Kind() != reflect.Pointer {
			continue
		}
		name, _, _ := strings.Cut(f.Tag.Get("json"), ",")
		if name == "" || name == "-" {
			t.Fatalf("ScanAllSummary.%s is a sub-block with no json tag", f.Name)
		}
		if strings.Contains(f.Tag.Get("json"), "omitempty") {
			t.Errorf("ScanAllSummary.%s is omitempty: a nil sub-block would vanish from the wire, and an "+
				"ABSENT key is not the same statement as an explicit null", f.Name)
		}
		keys = append(keys, name)
	}
	return keys
}

// mentionsNullMeansNotMeasured accepts either phrasing the two surfaces use
// today for the same lesson. It is deliberately about the STATEMENT, not the
// wording: the test must fail when the teaching disappears, not when someone
// rewrites the sentence.
func mentionsNullMeansNotMeasured(body string) bool {
	lower := strings.ToLower(body)
	if !strings.Contains(lower, "null") {
		return false
	}
	for _, phrase := range []string{"not measured", "was not measured", "nobody looked", "not looked"} {
		if strings.Contains(lower, phrase) {
			return true
		}
	}
	return false
}
