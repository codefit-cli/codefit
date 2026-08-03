package context

import (
	"github.com/codefit-cli/codefit/internal/config"
	"github.com/codefit-cli/codefit/internal/core/scope"
)

// AuditContext encapsulates all the information about the project being audited
// and is passed to every sensor. It is assembled once by the orchestrator and
// treated as read-only by sensors.
type AuditContext struct {
	// ProjectRoot is the absolute path to the project being audited.
	ProjectRoot string
	// Language is the project language (e.g. "go", "typescript"); selects the
	// LanguageProvider.
	Language string
	// Framework is the detected framework (e.g. "react"), optional.
	Framework string
	// Config is the parsed .codefit.yaml for the project.
	Config *config.Config
	// Scope is layer 0 of the filtering pyramid: the files this pass may examine.
	// The caller supplies it (codefit never asks git — see the scope package
	// doc). A walk must gate on scope.Scope.Narrows, NOT on Includes: the zero
	// value includes nothing, so gating on Includes would turn an AuditContext
	// assembled without a scope into a silent no-op audit reporting score 100.
	// Both the unset value and scope.Full mean "audit everything".
	//
	// It replaces a dead `Since string` that promised a git ref for an
	// incremental mode codefit never had, and that never had a reader or a
	// writer. A field naming a capability codefit does not have is the same class
	// of lie as a manifest that over-promises.
	Scope scope.Scope
}
