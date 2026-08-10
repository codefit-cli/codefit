package registry_test

import (
	"testing"

	"github.com/codefit-cli/codefit/internal/providers/registry"
)

// TestC5_ExposureSnapshot freezes the exact exposure state this change
// declares: both typescript and go are exposed to every resolver
// (SecurityScan, SurfaceTools, InitDetect) — the boundary P0-5/D3 drew ("Go
// declares narrow capability, minimal exposure") MOVED here (roadmap P4-1,
// docs/specs/declared-partial-language-exposure.md R3): the guarantee was
// never "Go stays out", it was "nothing is exposed without being declared",
// and Go's declaration (6 security rule ids, 1 of 4 surface categories) is
// now checked by TestExposedLanguageDeclaresNonEmptyCapability below, and its
// response-level declaration by internal/mcp's surface-coverage statement
// (R2).
//
// This test only asserts the field's VALUE, never its EFFECT — that gap
// (InitDetect declared but not enforced by ByMarkerFile) was WARNING 1 in
// this change's own verify report. initdetect_test.go carries the effect
// proof: it drives ByMarkerFile itself with a hand-built entry whose
// InitDetect is false and asserts it is skipped, exactly as this snapshot's
// SecurityScan/SurfaceTools values are already backed by Lock A/Lock B
// exercising providerForLanguage/providerFor directly.
func TestC5_ExposureSnapshot(t *testing.T) {
	cases := []struct {
		lang string
		want registry.Exposure
	}{
		{"typescript", registry.Exposure{SecurityScan: true, SurfaceTools: true, InitDetect: true}},
		{"go", registry.Exposure{SecurityScan: true, SurfaceTools: true, InitDetect: true}},
	}
	for _, tc := range cases {
		t.Run(tc.lang, func(t *testing.T) {
			e, ok := registry.ByName(tc.lang)
			if !ok {
				t.Fatalf("registry.ByName(%q) not found", tc.lang)
			}
			if e.Exposure != tc.want {
				t.Errorf("%s exposure = %+v, want %+v", tc.lang, e.Exposure, tc.want)
			}
		})
	}
}

// TestExposedLanguageDeclaresNonEmptyCapability is R3's REPLACEMENT lock —
// the boundary Lock A/B/C used to hold ("the resolvable set is exactly
// TypeScript") moved here rather than disappearing when Go crossed it: any
// entry exposed to ANY resolver that puts a provider's OWN analysis in front
// of an agent (SecurityScan or SurfaceTools — InitDetect only identifies a
// language, it runs no analysis) must declare a non-empty Capability. A
// language that got exposed with nothing declared would be reachable and
// silent — exactly the "exposure without declaration" failure
// docs/specs/declared-partial-language-exposure.md exists to forbid. (The
// response-level half of the replacement guarantee — every unmapped surface
// category appears in the response's not-covered statement — is checked at
// internal/mcp, where the response is actually built.)
func TestExposedLanguageDeclaresNonEmptyCapability(t *testing.T) {
	for _, e := range registry.All() {
		if !e.Exposure.SecurityScan && !e.Exposure.SurfaceTools {
			continue // not exposed to an analysis resolver — nothing to declare here
		}
		t.Run(e.Canonical, func(t *testing.T) {
			cap := e.New(nil).Capability()
			empty := len(cap.Security.Declared) == 0 && len(cap.Practices.Declared) == 0 && len(cap.Surface) == 0
			if empty {
				t.Errorf("%s is exposed (SecurityScan=%v, SurfaceTools=%v) but declares an EMPTY Capability — "+
					"an exposed language must declare what it covers",
					e.Canonical, e.Exposure.SecurityScan, e.Exposure.SurfaceTools)
			}
		})
	}
}
