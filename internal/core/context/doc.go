// Package context defines [AuditContext], the immutable struct that carries
// everything a sensor needs about the project under audit: where it lives, its
// language and framework, the parsed config, and the run modifiers (incremental
// ref, no-LLM mode).
//
// It lives in its own package — not in findings — so the leaf findings package
// stays dependency-free: AuditContext references config, and a finding type
// must never depend on the config parser.
package context
