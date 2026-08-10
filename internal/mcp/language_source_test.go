package mcp

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/codefit-cli/codefit/internal/providers/registry"
	"github.com/codefit-cli/codefit/internal/scaffold"
)

// initDetectsButScanAllCannotAudit lists languages codefit init
// (scaffold.Detect) recognizes that codefit-scan-all cannot (yet) resolve a
// SECURITY provider for — the init-welcomes / scan-all-refuses contradiction
// (proposal-scope-decision finding: three independent language switches that
// disagreed), made a DECLARED gap instead of a silent one. Same shape as
// deliberatelyNotInSkill (skill_tools_test.go): an entry here is a choice a
// test enforces, not an oversight nobody noticed.
var initDetectsButScanAllCannotAudit = map[string]string{
	"go": "codefit-scan-all can measure the DB dimension for a Go project with a " +
		"configured schema (P0-5), but no security provider is wired into " +
		"providerForLanguage yet — that wiring is roadmap P4-1, deliberately out " +
		"of this change's scope",
}

// TestLanguageSource_LockA_ResolvableSetIsExactlyTypeScript is D4's Lock A: the
// resolvable language set codefit-scan-all's provider resolution recognizes is
// EXACTLY {typescript, ts, tsx}, all aliasing the single canonical provider.
// Adding a case (e.g. "go") to providerForLanguage without this test noticing
// is exactly the scope creep P0-5 forbids (roadmap P4-1 owns wiring
// golang.New() in, as an explicit later decision) — this test turns that
// smuggling into a RED instead of a silent slide.
func TestLanguageSource_LockA_ResolvableSetIsExactlyTypeScript(t *testing.T) {
	resolves := []string{"typescript", "ts", "tsx"}
	for _, lang := range resolves {
		t.Run("resolves/"+lang, func(t *testing.T) {
			if p := providerForLanguage(lang, nil); p == nil {
				t.Errorf("providerForLanguage(%q) must resolve a provider, got nil", lang)
			}
		})
	}

	doesNotResolve := []string{"go", "python", "java", ""}
	for _, lang := range doesNotResolve {
		t.Run("nil/"+lang, func(t *testing.T) {
			if p := providerForLanguage(lang, nil); p != nil {
				t.Errorf("providerForLanguage(%q) must resolve nil, got %+v", lang, p)
			}
		})
	}

	want := []string{"typescript"}
	got := SupportedLanguageNames()
	if !slices.Equal(got, want) {
		t.Errorf("SupportedLanguageNames() = %v, want %v", got, want)
	}
}

// TestLanguageSource_LockB_ProviderForDivergence is D4's Lock B: surface.go's
// extension-based providerFor must resolve the SAME language every entry in
// languageProviders resolves by name, and must resolve NOTHING outside that
// union. providerForLanguage and providerFor are two independent switches over
// the same underlying capability (ADR finding #1411) — this test makes their
// agreement mechanical instead of assumed. Green today; RED the moment either
// switch gains a language the other does not know.
func TestLanguageSource_LockB_ProviderForDivergence(t *testing.T) {
	seen := map[string]bool{}
	for _, e := range registry.ExposedForSecurity() {
		p := e.New(nil)
		canonical := p.Language()
		if seen[canonical] {
			continue // alias already checked
		}
		seen[canonical] = true
		for _, ext := range p.FileExtensions() {
			t.Run("ext"+ext, func(t *testing.T) {
				resolved := providerFor("file" + ext)
				if resolved == nil {
					t.Fatalf("providerFor(%q) resolved nil, but a languageProviders entry resolves %q for this extension",
						ext, canonical)
				}
				if resolved.Language() != canonical {
					t.Errorf("providerFor(%q).Language() = %q, want %q", ext, resolved.Language(), canonical)
				}
			})
		}
	}

	// Positive probe: extensions OUTSIDE the resolvable set must resolve
	// nothing through providerFor either.
	for _, ext := range []string{".go", ".py"} {
		if resolved := providerFor("file" + ext); resolved != nil {
			t.Errorf("providerFor(%q) resolved %+v, want nil (no resolvable language owns this extension)", ext, resolved)
		}
	}
}

// detectableLanguages runs the REAL scaffold.Detect over minimal marker
// fixtures for the languages it recognizes today — driven, not
// re-implemented, so this test tracks scaffold/detect.go's actual switch
// rather than a copy of it.
func detectableLanguages(t *testing.T) map[string]bool {
	t.Helper()
	markers := map[string]string{
		"go.mod":       "module x\n",
		"package.json": "{}\n",
	}
	out := map[string]bool{}
	for marker, content := range markers {
		root := t.TempDir()
		if err := os.WriteFile(filepath.Join(root, marker), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		info, err := scaffold.Detect(root)
		if err != nil {
			t.Fatalf("scaffold.Detect(%s marker): %v", marker, err)
		}
		out[info.Language] = true
	}
	return out
}

// TestLanguageSource_LockC_InitVsScanAllDivergence is D4's Lock C: every
// language scaffold.Detect (codefit init) can identify from real marker files
// must be either in codefit-scan-all's resolvable set (SupportedLanguageNames)
// or explicitly declared in initDetectsButScanAllCannotAudit — never silently
// both welcomed by init and refused by scan-all. Checked in BOTH directions,
// mirroring TestSkillNamesEveryRegisteredTool / TestSkillOmissionAllowlistHas
// NoGhosts (skill_tools_test.go): an undeclared gap fails, and a stale
// allowlist entry (a language init no longer detects, or one scan-all now
// resolves) fails too.
func TestLanguageSource_LockC_InitVsScanAllDivergence(t *testing.T) {
	supported := map[string]bool{}
	for _, n := range SupportedLanguageNames() {
		supported[n] = true
	}
	detected := detectableLanguages(t)

	for lang := range detected {
		_, declared := initDetectsButScanAllCannotAudit[lang]
		switch {
		case !supported[lang] && !declared:
			t.Errorf("scaffold.Detect recognizes %q, codefit-scan-all cannot resolve a security provider for it, "+
				"and it is not declared in initDetectsButScanAllCannotAudit — either wire it or declare the gap", lang)
		case supported[lang] && declared:
			t.Errorf("%q is BOTH scan-all-resolvable and declared in initDetectsButScanAllCannotAudit — "+
				"drop the stale allowlist entry", lang)
		}
	}
	for lang := range initDetectsButScanAllCannotAudit {
		if !detected[lang] {
			t.Errorf("initDetectsButScanAllCannotAudit names %q, which scaffold.Detect no longer recognizes — "+
				"remove the stale entry", lang)
		}
	}
}
