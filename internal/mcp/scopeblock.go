package mcp

import (
	"fmt"

	"github.com/codefit-cli/codefit/internal/core/scope"
)

// The two scan modes. A response ALWAYS carries one of them: the field's
// presence is not conditional, so a consumer never has to infer the mode from an
// absence.
const (
	ScopeModeFull    = "full"
	ScopeModePartial = "partial"
)

// ScopeBlock is a scan's account of HOW MUCH of the project it looked at. It
// exists because a partial audit that is indistinguishable from a full one is a
// lying auditor: the whole point of narrowing is that the narrowing stays
// visible.
//
//   - Requested is how many distinct files the caller asked for (canonical, so a
//     path spelled twice counts once); 0 under a full scan.
//   - Audited is how many of them the pass actually examined, and AuditableTotal
//     how many it COULD have — the denominator is the whole project, never the
//     scope, or "3 of 412" collapses into a self-flattering "3 of 3".
//   - Unmatched are the requested paths the audit never reached: deleted, wrong
//     extension, outside the project, inside a skipped directory. Without it an
//     agent that passes three wrong paths gets "0 findings" and reads it as
//     clean. Requested always equals Audited + len(Unmatched).
//   - Note is MANDATORY when partial and FORBIDDEN when full (see Validate).
type ScopeBlock struct {
	Mode           string   `json:"mode"`
	Requested      int      `json:"requested"`
	Audited        int      `json:"audited"`
	AuditableTotal int      `json:"auditable_total"`
	Unmatched      []string `json:"unmatched,omitempty"`
	Note           string   `json:"note"`
}

// Validate enforces the honesty invariant in BOTH directions: a partial scan
// must declare itself in prose, and a full scan must not carry a caveat it has
// no basis for. Handlers call it before returning, so a violation is a loud
// error rather than an unlabelled partial result an agent would read as a full
// one — the failure this whole block exists to prevent.
func (s ScopeBlock) Validate() error {
	switch s.Mode {
	case ScopeModePartial:
		if s.Note == "" {
			return fmt.Errorf("codefit internal: a partial scan reported no note — a partial audit must declare itself")
		}
	case ScopeModeFull:
		if s.Note != "" {
			return fmt.Errorf("codefit internal: a full scan reported the note %q — a full audit has nothing to caveat", s.Note)
		}
	default:
		return fmt.Errorf("codefit internal: unknown scan scope mode %q", s.Mode)
	}
	return nil
}

// scopeBlockFor builds the block from the pass's scope, the files it examined,
// and how many it could have. auditedFiles may come from more than one dimension
// (the security walk plus the DB dimension's configured schema sources), so a
// caller unions them before calling.
func scopeBlockFor(scp scope.Scope, auditedFiles []string, auditableTotal int) ScopeBlock {
	if !scp.Narrows() {
		return ScopeBlock{
			Mode:           ScopeModeFull,
			Audited:        auditableTotal,
			AuditableTotal: auditableTotal,
		}
	}

	examined := make(map[string]bool, len(auditedFiles))
	for _, f := range auditedFiles {
		examined[scope.Canon(f)] = true
	}
	requested := scp.Files() // canonical and sorted
	audited := 0
	var unmatched []string
	for _, f := range requested {
		if examined[f] {
			audited++
			continue
		}
		unmatched = append(unmatched, f)
	}

	return ScopeBlock{
		Mode:           ScopeModePartial,
		Requested:      len(requested),
		Audited:        audited,
		AuditableTotal: auditableTotal,
		Unmatched:      unmatched,
		Note:           partialNote(audited, auditableTotal, unmatched),
	}
}

// partialNote states what the numbers mean for the agent reading them. It names
// `blocked` on purpose (R2): scoring.IsBlocked is unchanged and stays
// non-configurable, but under a partial scope blocked:false means "no critical
// in the audited slice", not "no critical". blocked:true needs no caveat — a
// critical found is a critical found.
func partialNote(audited, auditableTotal int, unmatched []string) string {
	note := fmt.Sprintf("Partial audit: %d of %d auditable files were in scope. Findings, score and `blocked` "+
		"describe only those files; the rest were not examined in this pass.", audited, auditableTotal)
	if len(unmatched) > 0 {
		note += fmt.Sprintf(" %d requested path(s) were never reached and are listed in `unmatched` "+
			"(deleted, not an auditable extension, or inside a skipped directory): they were NOT audited, "+
			"which is not the same as audited and clean.", len(unmatched))
	}
	return note
}
