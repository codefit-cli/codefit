package context

import "github.com/codefit-cli/codefit/internal/config"

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
	// Since is a git ref for incremental (--since) mode; empty means full scan.
	Since string
	// NoLLM disables every sensor layer that would require an LLM call.
	NoLLM bool
	// FailOn is the severity at or above which the run should fail (and the
	// pipeline may early-exit before the LLM layer). One of critical|high|medium.
	FailOn string
	// Interactive records whether stdout is an interactive terminal (resolved
	// once via TTY detection), so renderers and prompts can adapt.
	Interactive bool
}
