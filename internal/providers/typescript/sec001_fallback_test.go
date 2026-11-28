package typescript_test

import "testing"

// SEC-001 and a literal secret used as a FALLBACK (issue #148, verifier advisory
// A7 of 2026-11-28). `process.env.KEY || "s3cr3t"` puts a hardcoded credential in
// the source as surely as `"s3cr3t"` alone, and SEC-001 does not see it: the
// rule's $VALUE regex wants the bound text to start AND end with a quote, and a
// fallback expression starts with its first operand. This was present before the
// value veto and was declared nowhere.
//
// This is the NEGATIVE fixture: measured, declared in coverage.go and
// COVERAGE.md, listed in docs/roadmap.md as unscheduled, and NOT fixed here. If a
// quiet row starts firing, SEC-001 widened: move the row and update coverage.go
// and COVERAGE.md in the same commit.

// fallbackRows are the three fallback operators, each holding the same secret in
// the position a default value takes. want is the number of SEC-001 at every
// site; every row says why.
var fallbackRows = []struct {
	name   string
	value  string // TypeScript source for the whole bound expression
	want   int
	reason string
}{
	{"logical or", `process.env.SIGNING_KEY || "s3cr3t"`, 0,
		"the bound text starts with an identifier, not a quote, so the $VALUE regex refuses it"},
	{"nullish coalescing", `process.env.SIGNING_KEY ?? "s3cr3t"`, 0,
		"the same: the expression starts with its first operand, not a quote"},
	{"ternary", `isProduction ? process.env.SIGNING_KEY : "s3cr3t"`, 0,
		"the condition comes first, so the bound text does not start with a quote"},
	// The boundary of the blind spot, measured: when the WHOLE expression starts
	// and ends with a quoted literal, the regex admits it, the decoder refuses it
	// as not one literal, and it fires, like a concatenation of two literals.
	{"logical or between two literals", `"" || "s3cr3t"`, 1,
		"the bound text starts and ends with a quote, so the rule binds it; not one literal, so the veto proves nothing and it fires"},
}

// TestTSSEC001FallbackOperandIsUnseen measures each fallback at every site
// SEC-001 declares, through AnalyzeFile with the file parsed whole.
func TestTSSEC001FallbackOperandIsUnseen(t *testing.T) {
	for _, row := range fallbackRows {
		if row.reason == "" {
			t.Errorf("%s: no reason written", row.name)
			continue
		}
		for _, site := range tsSites {
			t.Run(row.name+"/"+site.shape, func(t *testing.T) {
				// The probe value is not what keeps a row quiet: the same site
				// fires on the bare literal.
				requireTSSiteReachesTheGate(t, site)
				if got := analyzeWhole(t, site.src(tsGatedName, row.value)); len(got) != row.want {
					t.Errorf("got %d SEC-001 on %s, want %d — %s", len(got), row.value, row.want, row.reason)
				}
			})
		}
	}
}
