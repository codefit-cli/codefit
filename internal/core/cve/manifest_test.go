package cve

import (
	"strings"
	"testing"
)

// TestParseManifests_NpmAndGo: both ecosystems present at the root are parsed and
// merged, with no notes (lockfile present → versions resolvable).
func TestParseManifests_NpmAndGo(t *testing.T) {
	dir := t.TempDir()
	writeProjectFile(t, dir, "package.json", []byte(`{"name":"x","version":"1.0.0"}`))
	writeProjectFile(t, dir, "package-lock.json", readFixture(t, "package-lock.json"))
	writeProjectFile(t, dir, "go.mod", readFixture(t, "gomod_sample.txt"))

	scan, err := ParseManifests(dir)
	if err != nil {
		t.Fatal(err)
	}
	got := depSet(scan.Dependencies)
	if got["lodash"] != "4.17.4" {
		t.Errorf("npm dep lodash missing/wrong: %+v", scan.Dependencies)
	}
	if got["github.com/gin-gonic/gin"] != "v1.6.0" {
		t.Errorf("go dep gin missing/wrong: %+v", scan.Dependencies)
	}
	var hasNpm, hasGo bool
	for _, d := range scan.Dependencies {
		hasNpm = hasNpm || d.Ecosystem == "npm"
		hasGo = hasGo || d.Ecosystem == "Go"
	}
	if !hasNpm || !hasGo {
		t.Errorf("want both npm and Go deps, got %+v", scan.Dependencies)
	}
	if len(scan.Notes) != 0 {
		t.Errorf("no notes expected when the lockfile is present, got %v", scan.Notes)
	}
}

// TestParseManifests_NoLockfile is the honest case (RF-09 decision A): a
// package.json without a package-lock.json yields NO guessed dependencies and a
// note naming the missing lockfile.
func TestParseManifests_NoLockfile(t *testing.T) {
	dir := t.TempDir()
	writeProjectFile(t, dir, "package.json", []byte(`{"name":"x","dependencies":{"lodash":"^4.17.4"}}`))

	scan, err := ParseManifests(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(scan.Dependencies) != 0 {
		t.Errorf("must NOT resolve versions from package.json ranges, got %+v", scan.Dependencies)
	}
	joined := strings.ToLower(strings.Join(scan.Notes, " "))
	if !strings.Contains(joined, "package-lock.json") {
		t.Errorf("must note the missing lockfile honestly, got %v", scan.Notes)
	}
}

// TestParseManifests_None: an empty project is noted, never silently empty.
func TestParseManifests_None(t *testing.T) {
	scan, err := ParseManifests(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if len(scan.Dependencies) != 0 {
		t.Errorf("empty project: expected no deps, got %+v", scan.Dependencies)
	}
	if len(scan.Notes) == 0 {
		t.Errorf("empty project should be noted, not silent")
	}
}
