package scaffold

import (
	"fmt"
	"strings"

	"github.com/codefit-cli/codefit/internal/config"
	"github.com/codefit-cli/codefit/internal/core/surface"
	"github.com/codefit-cli/codefit/internal/providers/registry"
)

// CapabilityStatement renders the human-facing capability sentence `codefit
// init` prints for a detected language, derived from
// internal/providers/registry's Exposure and Capability records — never a
// hardcoded per-language string (D5, roadmap P1-1b). An exposed language
// states its REAL reach (declared security rule count, N-of-M surface
// categories mapped, and which are not — R4,
// docs/specs/declared-partial-language-exposure.md), never a bare "can
// audit" sentence that would read as parity it does not have. A
// registered-but-unexposed language states its real gap instead.
// The undetected case is answered FIRST, before registry.ByName is consulted.
// The sentinel is by construction absent from the registry, so falling through
// would render "undetected is not a registered language" — codefit talking to
// itself about its own vocabulary instead of telling the developer that no
// provider resolved and what still applies.
func CapabilityStatement(language string) string {
	if language == config.LanguageUndetected {
		return UndetectedStatement()
	}
	e, ok := registry.ByName(language)
	if !ok {
		return fmt.Sprintf("%s is not a registered language — codefit declares no capability for it.", language)
	}
	if !e.Exposure.SecurityScan {
		return CapabilityStatementForExposure(e.Canonical, false)
	}
	cap := e.New(nil).Capability()
	cs := surface.DeriveCoverage(cap.Surface)
	// cs.Note already ends with a period (surface.DeriveCoverage's own
	// sentence) — no extra period is appended here, or the rendered sentence
	// reads "...this scan.. The DB dimension...".
	return fmt.Sprintf(
		"codefit-scan-security/codefit-scan-endpoint/codefit-scan-all can audit %s code: %d declared "+
			"security rule(s), %s The DB dimension audits the schema too when database.schema_paths is configured.",
		e.Canonical, len(cap.Security.Declared), cs.Note)
}

// CapabilityStatementForExposure renders the not-exposed capability sentence
// for a language name, independent of the real registry — the pure half of
// CapabilityStatement's decision, kept callable directly so the
// registered-but-unexposed branch stays under test even when every
// CURRENTLY registered language is exposed for security scanning (both are,
// since roadmap P4-1). A future registered-but-unexposed language reuses
// this unchanged.
func CapabilityStatementForExposure(languageName string, exposed bool) string {
	if exposed {
		return fmt.Sprintf("codefit-scan-security/codefit-scan-endpoint/codefit-scan-all can audit %s code.", languageName)
	}
	return fmt.Sprintf(
		"codefit-scan-security does not resolve a provider for %s — %s; %s code itself is not scanned.",
		languageName, dbOnlyClause(), languageName)
}

// UndetectedStatement renders the THIRD declaration codefit can make about a
// project's auditability, alongside "exposed, with this real reach" and
// "registered but not exposed": no marker file under the project root resolved
// an auditable language provider at all.
//
// It states four things, because leaving any of them out turns a declaration
// into a shrug: that nothing resolved; WHICH marker files codefit looks for
// (derived from the registry — never typed here, which is exactly how the
// deleted refusal message came to name four manifests that cannot help and omit
// the one that can); that code-level scanning therefore does not run; and that
// the DB dimension still audits the schema when database.schema_paths is
// configured.
//
// It also names the root-only limit rather than letting a monorepo developer
// discover it by confusion: detection is a plain os.Stat of the root, so a
// polyglot repository runs init per sub-project root.
//
// None of the three statements may read as a pass.
func UndetectedStatement() string {
	return fmt.Sprintf(
		"codefit found no language provider it can audit here — it looks for %s directly under the "+
			"project root (in a monorepo, run codefit init per sub-project root). "+
			"codefit-scan-security/codefit-scan-endpoint therefore scan no code in this project; %s.",
		markerList(), dbOnlyClause())
}

// dbOnlyClause is the ONE sentence fragment stating that the database dimension
// still applies when database.schema_paths is configured, interpolated by both
// CapabilityStatementForExposure and UndetectedStatement.
//
// It exists so "codefit reuses one vocabulary" is mechanical rather than
// promised: two hand-written sentences saying the same thing drift, and the
// drift shows up in the artifact a developer reads to decide whether codefit
// looked at their project at all. capability_statement_test.go asserts these
// exact bytes appear in both outputs.
func dbOnlyClause() string {
	return "only the DB dimension (when database.schema_paths is configured) audits this project today"
}

// markerList renders registry.InitDetectMarkerFiles() as prose ("a, b or c").
// Every marker name in every user-facing artifact passes through here.
func markerList() string {
	markers := registry.InitDetectMarkerFiles()
	switch len(markers) {
	case 0:
		// Unreachable while any entry has InitDetect: true. Stated rather than
		// rendered as an empty gap, because an empty list in this sentence
		// would read as "codefit looks for nothing".
		return "no marker file (no language is registered for detection)"
	case 1:
		return markers[0]
	default:
		return strings.Join(markers[:len(markers)-1], ", ") + " or " + markers[len(markers)-1]
	}
}
