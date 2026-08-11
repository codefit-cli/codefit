package mcp_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/codefit-cli/codefit/internal/core/surface"
	"github.com/codefit-cli/codefit/internal/mcp"
	"github.com/codefit-cli/codefit/internal/providers/registry"
)

// goSQLiSrc is a real Go source file with a SQL injection built by string
// concatenation (SEC-010) — the exact shape the architect's motivating
// measurement drove through the real handler.
const goSQLiSrc = `package x

import "database/sql"

func f(db *sql.DB, id string) {
	db.Query("SELECT * FROM users WHERE id = " + id)
}
`

// TestHandleScanSecurity_Go_SurfaceCoverageDeclaresTheGap is R2 for
// codefit-scan-security: a Go project's response must state which of
// surface.ProviderCategories its provider mapped (authz only) and which it
// did not (idor, overfetch, nplus1) — never just surface_items with no
// caveat.
func TestHandleScanSecurity_Go_SurfaceCoverageDeclaresTheGap(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, root, "x.go", goSQLiSrc)

	resp, err := mcp.HandleScanSecurity(mcp.ScanRequest{Root: root, Language: "go"})
	if err != nil {
		t.Fatalf("HandleScanSecurity over a go project must not error, got: %v", err)
	}
	assertGoSurfaceCoverage(t, resp.SurfaceCoverage.Mapped, resp.SurfaceCoverage.NotMapped, resp.SurfaceCoverage.Note)
}

// TestHandleScanAll_Go_EndToEnd_SQLInjectionAndDeclaredGap is test contract
// item 1: the end-to-end case that motivated this change, driven through the
// REAL handler. A Go project with a SQL injection must return BOTH the
// deterministic finding AND a statement that 3 of 4 surface categories were
// not mapped — "1" and "1 of 4" are different claims, and this test proves
// the response now makes the second one.
//
// Mutation (recorded manually during apply, not committed): removing the
// SurfaceCoverage statement from SecuritySection reproduces exactly what
// this change fixes — the test fails on the STATEMENT'S ABSENCE, not on its
// wording.
func TestHandleScanAll_Go_EndToEnd_SQLInjectionAndDeclaredGap(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, root, "x.go", goSQLiSrc)

	resp, err := mcp.HandleScanAll(mcp.ScanAllRequest{Root: root, Language: "go"})
	if err != nil {
		t.Fatalf("HandleScanAll over a go project with a SQL injection must not error, got: %v", err)
	}

	// The finding: it works, it found the real defect.
	if resp.Summary.Security.DeterministicFindings == 0 {
		t.Fatal("scan-all over a go project with a string-concatenated SQL query must report a deterministic finding (SEC-010)")
	}

	// The declared gap: three of four surface categories were never searched
	// for, and the response says so.
	if !resp.Security.Measured {
		t.Fatalf("security must be measured for a go project (now exposed), got %+v", resp.Security)
	}
	if resp.Security.SurfaceCoverage == nil {
		t.Fatal("Security.SurfaceCoverage must be present when security is measured — this IS the statement test contract item 1 requires")
	}
	assertGoSurfaceCoverage(t, resp.Security.SurfaceCoverage.Mapped, resp.Security.SurfaceCoverage.NotMapped, resp.Security.SurfaceCoverage.Note)
}

// minimalSourceFor is the smallest syntactically-valid file each exposed
// provider's parser accepts, keyed by its FIRST declared extension — enough
// for a real scan to run to completion (zero findings/surface is fine; this
// lock only cares about the coverage STATEMENT, not what got found).
func minimalSourceFor(ext string) string {
	switch ext {
	case ".go":
		return "package x\n"
	default:
		return "export {}\n"
	}
}

// TestReplacementLock_ExposedLanguageDeclaresCompleteGap is R3's response-
// level half of the replacement guarantee (test contract item 5): for EVERY
// language the registry exposes for security scanning — not just Go — the
// scan-security response's SurfaceCoverage must be EXACTLY the complement of
// its declared Capability().Surface against the full, locked
// surface.ProviderCategories vocabulary. Generic over the registry (not
// Go-specific), so a FUTURE exposed language is covered by construction, not
// by remembering to add another hand-written case.
//
// Mutation (recorded manually during apply, not committed): a
// surfaceCoverageFor that returned the zero CoverageStatement for a
// resolved provider (mapped-category gap silently missing from the
// response) fails this test — NotMapped would be empty/nil for a language
// that manifestly doesn't map every category.
func TestReplacementLock_ExposedLanguageDeclaresCompleteGap(t *testing.T) {
	for _, e := range registry.ExposedForSecurity() {
		t.Run(e.Canonical, func(t *testing.T) {
			p := e.New(nil)
			cap := p.Capability()
			exts := p.FileExtensions()
			if len(exts) == 0 {
				t.Fatalf("%s declares no FileExtensions — cannot build a fixture", e.Canonical)
			}
			root := t.TempDir()
			mustWrite(t, root, "x"+exts[0], minimalSourceFor(exts[0]))

			resp, err := mcp.HandleScanSecurity(mcp.ScanRequest{Root: root, Language: e.Canonical})
			if err != nil {
				t.Fatalf("HandleScanSecurity(%s): %v", e.Canonical, err)
			}

			wantMapped := len(cap.Surface)
			if len(resp.SurfaceCoverage.Mapped) != wantMapped {
				t.Errorf("%s: SurfaceCoverage.Mapped has %d entries, want %d (Capability().Surface)",
					e.Canonical, len(resp.SurfaceCoverage.Mapped), wantMapped)
			}
			wantNotMapped := len(surface.ProviderCategories) - wantMapped
			if len(resp.SurfaceCoverage.NotMapped) != wantNotMapped {
				t.Errorf("%s: SurfaceCoverage.NotMapped has %d entries, want %d (vocabulary size minus mapped) — "+
					"a missing/incomplete gap statement is exactly what R3's replacement lock forbids",
					e.Canonical, len(resp.SurfaceCoverage.NotMapped), wantNotMapped)
			}
			for _, c := range cap.Surface {
				if !slices.Contains(resp.SurfaceCoverage.Mapped, c) {
					t.Errorf("%s: SurfaceCoverage.Mapped is missing declared category %q", e.Canonical, c)
				}
			}
			if resp.SurfaceCoverage.Note == "" {
				t.Errorf("%s: SurfaceCoverage.Note must be non-empty", e.Canonical)
			}
		})
	}
}

// assertGoSurfaceCoverage is the shared assertion for Go's known Capability
// (authz mapped; idor, overfetch, nplus1 not mapped) — factored out so the
// scan-security and scan-all end-to-end tests assert the identical claim.
func assertGoSurfaceCoverage(t *testing.T, mapped, notMapped []surface.Category, note string) {
	t.Helper()
	if len(mapped) != 1 || mapped[0] != surface.CategoryAuthz {
		t.Errorf("SurfaceCoverage.Mapped = %v, want [authz]", mapped)
	}
	if len(notMapped) != 3 {
		t.Fatalf("SurfaceCoverage.NotMapped = %v, want 3 categories (idor, overfetch, nplus1)", notMapped)
	}
	for _, want := range []surface.Category{surface.CategoryIDOR, surface.CategoryOverfetch, surface.CategoryNPlus1} {
		found := false
		for _, c := range notMapped {
			if c == want {
				found = true
			}
		}
		if !found {
			t.Errorf("SurfaceCoverage.NotMapped missing %q: %v", want, notMapped)
		}
	}
	if note == "" {
		t.Error("SurfaceCoverage.Note must be non-empty — the prose statement is what survives into an agent's reasoning (R2)")
	}
	if !strings.Contains(note, "1 of 4") {
		t.Errorf("SurfaceCoverage.Note must state the 1-of-4 claim, got %q", note)
	}
}
