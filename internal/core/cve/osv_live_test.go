//go:build osvlive

package cve

import (
	"context"
	"testing"
)

// TestOSVLive hits the REAL api.osv.dev to validate the wire-format assumptions
// the mocked unit tests cannot: the response shape, severity/fixed extraction,
// and the Go version normalization (the "v" strip). It is opt-in and NOT run in
// CI — run it manually:
//
//	go test -tags osvlive -run TestOSVLive ./internal/core/cve/ -v
//
// It queries dependencies with known advisories (an npm and a Go module) plus a
// control, asserting OSV reports vulns for the known-vulnerable ones.
func TestOSVLive(t *testing.T) {
	client := NewOSVClient()
	deps := []Dependency{
		{Name: "lodash", Version: "4.17.4", Ecosystem: "npm"},
		{Name: "github.com/dgrijalva/jwt-go", Version: "v3.2.0+incompatible", Ecosystem: "Go"},
		{Name: "express", Version: "4.18.2", Ecosystem: "npm"},
	}
	res, err := client.Query(context.Background(), deps)
	if err != nil {
		t.Fatalf("live OSV query failed: %v", err)
	}

	lodash := res["lodash@4.17.4"]
	if len(lodash) == 0 {
		t.Errorf("npm: expected OSV to report vulns for lodash@4.17.4, got none")
	} else {
		t.Logf("npm lodash@4.17.4: %d vulns; first id=%s severity=%q fixed=%q refs=%d",
			len(lodash), lodash[0].ID, lodash[0].Severity, lodash[0].FixedIn, len(lodash[0].References))
	}

	jwt := res["github.com/dgrijalva/jwt-go@v3.2.0+incompatible"]
	if len(jwt) == 0 {
		t.Errorf("Go: expected OSV to report vulns for dgrijalva/jwt-go@v3.2.0+incompatible — none returned, so the Go ecosystem format or the v-prefix strip may be wrong")
	} else {
		t.Logf("Go jwt-go: %d vulns; first id=%s severity=%q fixed=%q",
			len(jwt), jwt[0].ID, jwt[0].Severity, jwt[0].FixedIn)
	}
}
