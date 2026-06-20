// Package providers defines the [LanguageProvider] contract — the extensibility
// seam (PRD section 14). Each language implements this interface (tree-sitter
// queries, ecosystem best-practice rules, schema/ORM parsing, benchmark
// harness, test detection, review prompt context). Adding a language means
// implementing a provider; it never touches the core, the sensors, the MCP
// server, the CLI or the reporting.
//
// The interface is parser-agnostic (ADR 0001): the provider owns its parser and
// returns findings via AnalyzeSecurity/AnalyzePractices, so adding a language
// never touches the core or the sensors. The Go provider (codefit's self-audit
// bootstrap) backs it with go/ast.
package providers
