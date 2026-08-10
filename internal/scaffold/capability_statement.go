package scaffold

import (
	"fmt"

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
func CapabilityStatement(language string) string {
	e, ok := registry.ByName(language)
	if !ok {
		return fmt.Sprintf("%s is not a registered language — codefit declares no capability for it.", language)
	}
	if !e.Exposure.SecurityScan {
		return CapabilityStatementForExposure(e.Canonical, false)
	}
	cap := e.New(nil).Capability()
	cs := surface.DeriveCoverage(cap.Surface)
	return fmt.Sprintf(
		"codefit-scan-security/codefit-scan-endpoint/codefit-scan-all can audit %s code: %d declared "+
			"security rule(s), %s. The DB dimension audits the schema too when database.schema_paths is configured.",
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
		"codefit-scan-security does not resolve a provider for %s — only the DB dimension "+
			"(when database.schema_paths is configured) audits this project today; %s code itself is not scanned.",
		languageName, languageName)
}
