package cve

import (
	"os"
	"path/filepath"
	"testing"
)

// readFixture loads a testdata file or fails the test.
func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return data
}

// depSet indexes parsed dependencies by name → version for terse assertions.
func depSet(deps []Dependency) map[string]string {
	m := make(map[string]string, len(deps))
	for _, d := range deps {
		m[d.Name] = d.Version
	}
	return m
}

// writeProjectFile writes a manifest into a temp project dir under its canonical
// name, so ParseManifests detects it the way it would in a real project.
func writeProjectFile(t *testing.T, dir, name string, content []byte) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), content, 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}
