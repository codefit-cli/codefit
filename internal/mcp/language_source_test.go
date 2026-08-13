package mcp

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/codefit-cli/codefit/internal/config"
	"github.com/codefit-cli/codefit/internal/providers/registry"
	"github.com/codefit-cli/codefit/internal/scaffold"
)

// initDetectsButScanAllCannotAudit lists languages codefit init
// (scaffold.Detect) recognizes that codefit-scan-all cannot resolve a
// SECURITY provider for — the init-welcomes / scan-all-refuses contradiction
// made a DECLARED gap instead of a silent one. Same shape as
// deliberatelyNotInSkill (skill_tools_test.go): an entry here is a choice a
// test enforces, not an oversight nobody noticed. Roadmap P4-1
// (docs/specs/declared-partial-language-exposure.md) wired Go in and emptied
// this map; `codefit init` no longer refusing over language put one entry back,
// and this time it is permanent rather than pending a provider.
var initDetectsButScanAllCannotAudit = map[string]string{
	config.LanguageUndetected: "init writes this when no marker file resolved an auditable provider. " +
		"It is not a language and never will be one, so scan-all resolving nothing for it is the " +
		"CORRECT outcome, not a gap waiting to be closed: a scan-all on such a project reports " +
		"security as not measured, which is the honest answer. codefit init declares the same thing " +
		"up front, in the report, the generated config and the generated skill.",
}

// TestLanguageSource_LockA_ResolvableSetIncludesGo is D4's Lock A, moved
// (roadmap P4-1, docs/specs/declared-partial-language-exposure.md R3): the
// guarantee was never "Go stays out", it was "nothing is exposed without
// being declared" — Go's Capability declaration is checked by
// internal/providers/registry's TestExposedLanguageDeclaresNonEmptyCapability,
// and its response-level not-covered statement by scan_test.go /
// scanall_test.go. This lock now asserts the resolvable set is EXACTLY
// {typescript, ts, tsx, go} — adding a further language without this test
// noticing is still a RED, not a silent slide.
func TestLanguageSource_LockA_ResolvableSetIncludesGo(t *testing.T) {
	resolves := []string{"typescript", "ts", "tsx", "go"}
	for _, lang := range resolves {
		t.Run("resolves/"+lang, func(t *testing.T) {
			if p := providerForLanguage(lang, nil); p == nil {
				t.Errorf("providerForLanguage(%q) must resolve a provider, got nil", lang)
			}
		})
	}

	// The sentinel belongs here for the same reason "" does: it is a value
	// that reaches this resolver from a real generated .codefit.yaml, and
	// nothing else proves it resolves no provider. Every declaration codefit
	// makes about an undetected project rests on that being true.
	doesNotResolve := []string{"python", "java", "", config.LanguageUndetected}
	for _, lang := range doesNotResolve {
		t.Run("nil/"+lang, func(t *testing.T) {
			if p := providerForLanguage(lang, nil); p != nil {
				t.Errorf("providerForLanguage(%q) must resolve nil, got %+v", lang, p)
			}
		})
	}

	want := []string{"go", "typescript"}
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
	// nothing through providerFor either. ".go" moved INTO the loop above
	// (roadmap P4-1) — it is no longer a negative case.
	for _, ext := range []string{".py", ".java"} {
		if resolved := providerFor("file" + ext); resolved != nil {
			t.Errorf("providerFor(%q) resolved %+v, want nil (no resolvable language owns this extension)", ext, resolved)
		}
	}
}

// detectableLanguages runs the REAL scaffold.Detect over one fixture per
// marker file the registry declares for init detection, PLUS a root with no
// marker at all — the outcome Detect can now produce and previously could not.
//
// The marker set is DERIVED from registry.All(), never listed here. The
// hardcoded map this replaced named go.mod and package.json only: it never saw
// tsconfig.json, and — far worse — it could never see the undetected sentinel,
// because a map of markers has no entry for the absence of one. Lock C would
// have stayed green while covering nothing, which is precisely the class of
// silent staleness this change exists to remove. Deriving it is safe: detection
// is a plain os.Stat of the root, so an empty file at the right name is a
// faithful fixture.
func detectableLanguages(t *testing.T) map[string]bool {
	t.Helper()
	out := map[string]bool{}

	detect := func(what, root string) {
		t.Helper()
		info, err := scaffold.Detect(root)
		if err != nil {
			t.Fatalf("scaffold.Detect(%s): %v", what, err)
		}
		out[info.Language] = true
	}

	markers := 0
	for _, e := range registry.All() {
		if !e.Exposure.InitDetect {
			continue
		}
		for _, marker := range e.MarkerFiles {
			root := t.TempDir()
			// "{}" is valid for the JSON markers and irrelevant to the
			// others: Detect resolves the provider by os.Stat alone.
			if err := os.WriteFile(filepath.Join(root, marker), []byte("{}\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			detect(marker+" marker", root)
			markers++
		}
	}
	if markers == 0 {
		t.Fatal("no InitDetect marker files in the registry — this helper would report only the undetected case")
	}

	// The no-marker root. Without it the map is not DRIVEN by what Detect can
	// return, only by what someone remembered to list.
	detect("root with no marker file", t.TempDir())
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
