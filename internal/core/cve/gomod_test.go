package cve

import "testing"

// TestParseGoMod covers the require forms: a require( ) block with a direct and
// an // indirect dependency, and a single-line require. Versions are exact (Go
// pins them via MVS), the +incompatible suffix is preserved, the module line is
// not a dependency, and the replace directive is ignored (a declared limit).
func TestParseGoMod(t *testing.T) {
	deps, err := parseGoMod(readFixture(t, "gomod_sample.txt"))
	if err != nil {
		t.Fatal(err)
	}
	got := depSet(deps)
	want := map[string]string{
		"github.com/gin-gonic/gin":    "v1.6.0",
		"gopkg.in/yaml.v2":            "v2.2.2",              // // indirect is still a real installed dep
		"github.com/dgrijalva/jwt-go": "v3.2.0+incompatible", // single-line require, +incompatible preserved
	}
	for name, ver := range want {
		if got[name] != ver {
			t.Errorf("dep %q: got version %q, want %q (all: %+v)", name, got[name], ver, deps)
		}
	}
	if _, ok := got["github.com/sample/app"]; ok {
		t.Errorf("the module itself must NOT be reported as a dependency: %+v", deps)
	}
	for _, d := range deps {
		if d.Ecosystem != "Go" {
			t.Errorf("ecosystem must be %q, got %q for %s", "Go", d.Ecosystem, d.Name)
		}
	}
}
