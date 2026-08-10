package registry_test

import (
	"testing"

	"github.com/codefit-cli/codefit/internal/providers/registry"
)

// TestC5_ExposureSnapshot freezes the exact exposure state D3 declares:
// typescript is exposed to every resolver (SecurityScan, SurfaceTools,
// InitDetect); go is exposed to InitDetect only. This is the mechanical
// version of "Go declares narrow capability, minimal exposure" — a capable
// provider that stays deliberately unreachable everywhere but init.
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
		{"go", registry.Exposure{SecurityScan: false, SurfaceTools: false, InitDetect: true}},
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
