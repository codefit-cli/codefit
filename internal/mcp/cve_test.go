package mcp

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/codefit-cli/codefit/internal/core/cve"
)

// fakeCVEClient returns canned vulnerabilities without touching the network.
type fakeCVEClient struct {
	byKey  map[string][]cve.Vulnerability
	called bool
}

func (f *fakeCVEClient) Query(_ context.Context, _ []cve.Dependency) (map[string][]cve.Vulnerability, error) {
	f.called = true
	return f.byKey, nil
}

// TestHandleCheckCVEs_Vulnerable: a project with a lockfile resolves to exact
// versions; the handler queries OSV (mocked) and reports the vulnerable dep with
// its vulnerability detail, keyed correctly by name@version.
func TestHandleCheckCVEs_Vulnerable(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "package-lock.json", `{
	  "lockfileVersion": 3,
	  "packages": {
	    "": {"name":"app","version":"1.0.0"},
	    "node_modules/lodash": {"version":"4.17.4"}
	  }
	}`)

	fake := &fakeCVEClient{byKey: map[string][]cve.Vulnerability{
		"lodash@4.17.4": {{ID: "GHSA-x", Severity: "HIGH", FixedIn: "4.17.5"}},
	}}
	restore := swapCVEClient(func() cve.Client { return fake })
	defer restore()

	resp, err := HandleCheckCVEs(CheckCVEsRequest{Root: dir})
	if err != nil {
		t.Fatal(err)
	}
	if resp.DependenciesScanned != 1 {
		t.Errorf("dependencies_scanned: got %d, want 1", resp.DependenciesScanned)
	}
	if len(resp.Vulnerable) != 1 {
		t.Fatalf("vulnerable: got %d, want 1: %+v", len(resp.Vulnerable), resp)
	}
	vd := resp.Vulnerable[0]
	if vd.Name != "lodash" || vd.Version != "4.17.4" || vd.Ecosystem != "npm" {
		t.Errorf("vulnerable dep wrong: %+v", vd)
	}
	if len(vd.Vulnerabilities) != 1 || vd.Vulnerabilities[0].ID != "GHSA-x" {
		t.Errorf("vuln detail wrong: %+v", vd.Vulnerabilities)
	}
}

// TestHandleCheckCVEs_NoLockfileNote: a package.json without a lockfile yields the
// honest note and NEVER queries OSV (no resolvable versions to ask about).
func TestHandleCheckCVEs_NoLockfileNote(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "package.json", `{"name":"app","dependencies":{"lodash":"^4.17.4"}}`)

	fake := &fakeCVEClient{}
	restore := swapCVEClient(func() cve.Client { return fake })
	defer restore()

	resp, err := HandleCheckCVEs(CheckCVEsRequest{Root: dir})
	if err != nil {
		t.Fatal(err)
	}
	if fake.called {
		t.Errorf("OSV must NOT be queried when no lockfile resolves versions")
	}
	if len(resp.Vulnerable) != 0 {
		t.Errorf("expected no vulnerable deps, got %+v", resp.Vulnerable)
	}
	if len(resp.Notes) == 0 {
		t.Errorf("expected the missing-lockfile note to pass through")
	}
}

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}
