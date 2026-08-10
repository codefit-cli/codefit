package typescript_test

import (
	"sort"
	"testing"

	"github.com/codefit-cli/codefit/internal/core/ruleengine"
	"github.com/codefit-cli/codefit/internal/providers/typescript"
	"github.com/codefit-cli/codefit/rules"
)

// loadedSecurityRuleIDs returns the sorted rule IDs the real TypeScript
// security rule loader compiles — the same loader typescript.go's
// securityRules() uses (ruleengine.LoadFS(rules.FS, "typescript/security")),
// driven directly rather than copied, so Control A cannot silently drift from
// what AnalyzeSecurity actually runs.
func loadedSecurityRuleIDs(t *testing.T) []string {
	t.Helper()
	raw, err := ruleengine.LoadFS(rules.FS, "typescript/security")
	if err != nil {
		t.Fatalf("ruleengine.LoadFS(typescript/security): %v", err)
	}
	ids := make([]string, 0, len(raw))
	for _, r := range raw {
		ids = append(ids, r.ID)
	}
	sort.Strings(ids)
	return ids
}

// setEqual is strict, both-directions set equality — Control A's shape: a
// Declared list missing a loaded rule, OR naming a rule the loader does not
// have, is a failure either way.
func setEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	sa, sb := append([]string{}, a...), append([]string{}, b...)
	sort.Strings(sa)
	sort.Strings(sb)
	for i := range sa {
		if sa[i] != sb[i] {
			return false
		}
	}
	return true
}

// TestTypeScriptSecurityDeclaration_ControlAMechanismCatchesDrift proves the
// equality mechanism itself is not a tautology: a deliberately wrong Declared
// set (seeded here, never the real Capability) must NOT equal the real loaded
// rule IDs.
func TestTypeScriptSecurityDeclaration_ControlAMechanismCatchesDrift(t *testing.T) {
	loaded := loadedSecurityRuleIDs(t)
	wrong := []string{"SEC-999"} // deliberately wrong: not a real loaded rule ID
	if setEqual(wrong, loaded) {
		t.Fatal("test fixture is broken: a deliberately wrong Declared set must not equal the real loaded rule IDs")
	}
}

// TestTypeScriptSecurityDeclaration_MatchesLoadedRules is Control A: for the
// TypeScript provider's Enumerable:true Security RuleSet, Declared must be an
// EXACT match (both directions) of the real rule loader's IDs —
// ruleengine.LoadFS(rules.FS, "typescript/security"), the same loader
// AnalyzeSecurity uses (typescript.go's securityRules()). TypeScript
// hardcodes no rule ID in its non-test .go files (measured in preflight), so
// strict equality holds — no subset-plus-declared-gap fallback.
func TestTypeScriptSecurityDeclaration_MatchesLoadedRules(t *testing.T) {
	loaded := loadedSecurityRuleIDs(t)
	cap := typescript.New().Capability()
	if !cap.Security.Enumerable {
		t.Fatal("TypeScript's Security RuleSet must be Enumerable:true — a real rule loader exists")
	}
	declared := append([]string{}, cap.Security.Declared...)
	sort.Strings(declared)
	if !setEqual(declared, loaded) {
		t.Errorf("TypeScript Security.Declared drifted from the loaded rules:\n  Declared: %v\n  Loaded:   %v", declared, loaded)
	}
}
