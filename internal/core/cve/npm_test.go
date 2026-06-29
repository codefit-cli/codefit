package cve

import "testing"

// TestParseNpmLock_PackagesV3 covers lockfileVersion 2/3: the `packages` map keyed
// by install path. Exact versions only, the root project excluded, and a nested
// name (node_modules/express/node_modules/cookie) resolved to its last segment.
func TestParseNpmLock_PackagesV3(t *testing.T) {
	deps, err := parseNpmLock(readFixture(t, "package-lock.json"))
	if err != nil {
		t.Fatal(err)
	}
	got := depSet(deps)
	want := map[string]string{"express": "4.18.2", "lodash": "4.17.4", "cookie": "0.5.0"}
	for name, ver := range want {
		if got[name] != ver {
			t.Errorf("dep %q: got version %q, want %q (all: %+v)", name, got[name], ver, deps)
		}
	}
	if _, ok := got["sample-app"]; ok {
		t.Errorf("the root project must NOT be reported as a dependency: %+v", deps)
	}
	for _, d := range deps {
		if d.Ecosystem != "npm" {
			t.Errorf("ecosystem must be %q, got %q for %s", "npm", d.Ecosystem, d.Name)
		}
	}
}

// TestParseNpmLock_DependenciesV1 covers lockfileVersion 1: the nested
// `dependencies` map. The transitive minimist@1.2.0 (under mkdirp) must be found.
func TestParseNpmLock_DependenciesV1(t *testing.T) {
	deps, err := parseNpmLock(readFixture(t, "package-lock-v1.json"))
	if err != nil {
		t.Fatal(err)
	}
	got := depSet(deps)
	if got["minimist"] != "1.2.0" {
		t.Errorf("nested v1 dep minimist: got %q, want 1.2.0 (all: %+v)", got["minimist"], deps)
	}
	if got["mkdirp"] != "0.5.1" {
		t.Errorf("v1 dep mkdirp: got %q, want 0.5.1 (all: %+v)", got["mkdirp"], deps)
	}
}
