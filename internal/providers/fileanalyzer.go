package providers

import "github.com/codefit-cli/codefit/internal/core/findings"

// FileAnalyzer is the optional capability a provider implements to analyse a
// file ONCE and say how much of it it read: the same deterministic findings
// AnalyzeSecurity returns, the same surface AnalyzeSurface returns, and the
// parse status of the one tree both were computed from (ADR 0089).
//
// It is intentionally NOT part of LanguageProvider, and the caller resolves it
// by type assertion (provider.(providers.FileAnalyzer)), exactly as
// QueryExtractor and SchemaParser are resolved. A provider that does not
// implement it is analysed through the two separate calls and its files carry
// findings.ParseNotReported() — never a status of complete it did not compute.
//
// ONE PARSE, ONE TRUTH. The status describes the tree the findings and the
// surface came from, not a second parse of the same bytes. Two parses could
// disagree (a parser stop is a function of the parse, not only of the input), and
// a status taken from one tree about findings taken from another proves nothing.
//
// The contract an implementation keeps:
//
//   - Findings and Surface are exactly what AnalyzeSecurity and AnalyzeSurface
//     return for the same SourceFile, in the same order. The combined call is an
//     optimisation of the two, never a different analysis.
//   - Parse is reported (Reported true) for every file the call returns without
//     error. An error means no analysis at all, exactly as for the two calls.
//   - It is per-file and filesystem-free, like the calls it combines.
//
// What it does NOT cover: callers that do not go through the security sensor —
// ExtractQueryFilters, the stateless file-level surface tools, Parse itself —
// still parse on their own and report no status.
type FileAnalyzer interface {
	AnalyzeFile(src SourceFile) (FileAnalysis, error)
}

// FileAnalysis is one file's analysis from a single parse.
type FileAnalysis struct {
	// Findings are the deterministic AST findings (what AnalyzeSecurity returns).
	Findings []findings.Finding
	// Surface is the mapped surface (what AnalyzeSurface returns).
	Surface []findings.SurfaceItem
	// Parse is the status of the tree both were computed from.
	Parse findings.ParseStatus
}
