package providers

import "github.com/codefit-cli/codefit/internal/core/query"

// QueryExtractor is the optional capability a provider implements to extract the
// neutral query-filter facts from application code — the CODE side of the
// code↔schema cross (index-vs-query), mirroring how SchemaParser produces the
// SCHEMA side. It is intentionally NOT part of LanguageProvider: the caller
// resolves it by type-assertion (provider.(providers.QueryExtractor)), exactly as
// SchemaParser and CoverageManifest are resolved today (ADR 0014, ADR 0029).
// Convergence into LanguageProvider waits for a second real extractor to validate
// the shape.
//
// It is per-file and filesystem-free, mirroring LanguageProvider.AnalyzeSurface:
// it receives one already-read SourceFile and returns the query filters found in
// it; the caller walks the project. A file with no column-filtering query yields
// nil — a query that filters nothing has nothing to cross against an index.
type QueryExtractor interface {
	ExtractQueryFilters(src SourceFile) ([]query.QueryFilter, error)
}
