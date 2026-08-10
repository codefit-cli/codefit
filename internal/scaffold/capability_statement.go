package scaffold

import (
	"fmt"

	"github.com/codefit-cli/codefit/internal/providers/registry"
)

// CapabilityStatement renders the human-facing capability sentence `codefit
// init` prints for a detected language, derived from
// internal/providers/registry's Exposure record — never a hardcoded
// per-language string (D5, roadmap P1-1b). A registered-but-unexposed
// language (Go today) states its real gap instead of implying full coverage.
func CapabilityStatement(language string) string {
	e, ok := registry.ByName(language)
	if !ok {
		return fmt.Sprintf("%s is not a registered language — codefit declares no capability for it.", language)
	}
	if e.Exposure.SecurityScan {
		return fmt.Sprintf(
			"codefit-scan-security/codefit-scan-endpoint/codefit-scan-all can audit %s code; "+
				"the DB dimension audits the schema too when database.schema_paths is configured.",
			e.Canonical)
	}
	return fmt.Sprintf(
		"codefit-scan-security does not resolve a provider for %s — only the DB dimension "+
			"(when database.schema_paths is configured) audits this project today; %s code itself is not scanned.",
		e.Canonical, e.Canonical)
}
