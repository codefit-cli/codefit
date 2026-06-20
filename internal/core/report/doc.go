// Package report defines the canonical audit report and the renderers that
// present it. The JSON report is the single source of truth (it carries its own
// schema_version, independent of the codefit version); plain-text, TUI and HTML
// are interchangeable renderers over it, chosen by TTY detection (PRD
// section 18).
//
// It defines [AuditReport] and three [Renderer] implementations — JSON
// (canonical), plain text, and an HTML placeholder — plus TTY detection so the
// plain renderer is used in pipes/CI/MCP.
package report
