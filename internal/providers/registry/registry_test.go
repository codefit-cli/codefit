package registry_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/codefit-cli/codefit/internal/providers/registry"
)

func TestAll_ReturnsBothEntries(t *testing.T) {
	all := registry.All()
	if len(all) != 2 {
		t.Fatalf("All() = %d entries, want 2", len(all))
	}
	names := map[string]bool{}
	for _, e := range all {
		names[e.Canonical] = true
	}
	for _, want := range []string{"go", "typescript"} {
		if !names[want] {
			t.Errorf("All() missing canonical entry %q", want)
		}
	}
}

func TestByName_ResolvesCanonicalAndAliases(t *testing.T) {
	cases := []struct {
		name      string
		wantFound bool
		wantCanon string
	}{
		{"typescript", true, "typescript"},
		{"ts", true, "typescript"},
		{"tsx", true, "typescript"},
		{"go", true, "go"},
		{"python", false, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e, ok := registry.ByName(tc.name)
			if ok != tc.wantFound {
				t.Fatalf("ByName(%q) found = %v, want %v", tc.name, ok, tc.wantFound)
			}
			if ok && e.Canonical != tc.wantCanon {
				t.Errorf("ByName(%q).Canonical = %q, want %q", tc.name, e.Canonical, tc.wantCanon)
			}
		})
	}
}

func TestByExtension_ResolvesFromRealProviderExtensions(t *testing.T) {
	cases := []struct {
		ext       string
		wantFound bool
		wantCanon string
	}{
		{".ts", true, "typescript"},
		{".tsx", true, "typescript"},
		{".go", true, "go"},
		{".py", false, ""},
	}
	for _, tc := range cases {
		t.Run(tc.ext, func(t *testing.T) {
			e, ok := registry.ByExtension(tc.ext)
			if ok != tc.wantFound {
				t.Fatalf("ByExtension(%q) found = %v, want %v", tc.ext, ok, tc.wantFound)
			}
			if ok && e.Canonical != tc.wantCanon {
				t.Errorf("ByExtension(%q).Canonical = %q, want %q", tc.ext, e.Canonical, tc.wantCanon)
			}
		})
	}
}

// TestByMarkerFile_GoModWinsOverPackageJSON locks the exact priority
// scaffold/detect.go documents at detect.go:68-84: in a polyglot root with
// both go.mod and package.json, Go wins.
func TestByMarkerFile_GoModWinsOverPackageJSON(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "package.json"), []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	e, ok := registry.ByMarkerFile(root)
	if !ok {
		t.Fatal("ByMarkerFile must find a match in a polyglot root")
	}
	if e.Canonical != "go" {
		t.Errorf("ByMarkerFile in a polyglot root = %q, want %q (go.mod wins over package.json)", e.Canonical, "go")
	}
}

func TestByMarkerFile_PackageJSONAlone(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "package.json"), []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	e, ok := registry.ByMarkerFile(root)
	if !ok || e.Canonical != "typescript" {
		t.Errorf("ByMarkerFile(package.json only) = (%+v, %v), want typescript/true", e, ok)
	}
}

func TestByMarkerFile_NoMarkersFound(t *testing.T) {
	root := t.TempDir()
	if _, ok := registry.ByMarkerFile(root); ok {
		t.Error("ByMarkerFile must return false when no marker file exists")
	}
}

// TestExposedForSecurity_GoAndTypeScript locks the exposure filter's current
// state (roadmap P4-1: the boundary that used to be "only typescript" MOVED
// to include go, in table order — go is listed first in the table).
func TestExposedForSecurity_GoAndTypeScript(t *testing.T) {
	exposed := registry.ExposedForSecurity()
	if len(exposed) != 2 {
		t.Fatalf("ExposedForSecurity() = %d entries, want 2, got %+v", len(exposed), exposed)
	}
	if exposed[0].Canonical != "go" || exposed[1].Canonical != "typescript" {
		t.Errorf("ExposedForSecurity() canonicals = [%q, %q], want [go, typescript] (table order)",
			exposed[0].Canonical, exposed[1].Canonical)
	}
}

// TestExposedForSurfaceTools_GoAndTypeScript mirrors the security exposure
// lock for the surface-tools resolver.
func TestExposedForSurfaceTools_GoAndTypeScript(t *testing.T) {
	exposed := registry.ExposedForSurfaceTools()
	if len(exposed) != 2 {
		t.Fatalf("ExposedForSurfaceTools() = %d entries, want 2, got %+v", len(exposed), exposed)
	}
	if exposed[0].Canonical != "go" || exposed[1].Canonical != "typescript" {
		t.Errorf("ExposedForSurfaceTools() canonicals = [%q, %q], want [go, typescript] (table order)",
			exposed[0].Canonical, exposed[1].Canonical)
	}
}

// TestExtensions_ReadFromProviderNotStoredField locks D2's rule that
// extensions are NEVER a table field — re-typing them would make the table a
// sixth hand-written answer to "what can this language do", the exact defect
// this change fixes.
func TestExtensions_ReadFromProviderNotStoredField(t *testing.T) {
	e, ok := registry.ByName("typescript")
	if !ok {
		t.Fatal("typescript entry must exist")
	}
	exts := e.New(nil).FileExtensions()
	want := []string{".ts", ".tsx"}
	if len(exts) != len(want) {
		t.Fatalf("FileExtensions() = %v, want %v", exts, want)
	}
	for i := range want {
		if exts[i] != want[i] {
			t.Errorf("FileExtensions()[%d] = %q, want %q", i, exts[i], want[i])
		}
	}
}
