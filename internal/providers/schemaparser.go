package providers

import "github.com/codefit-cli/codefit/internal/core/db"

// SchemaParser is the optional capability a provider implements to parse a
// database schema into the neutral db.Schema model. It is intentionally NOT part
// of LanguageProvider: the caller resolves it by type-assertion
// (provider.(providers.SchemaParser)), exactly as codefit-coverage resolves
// CoverageManifest today — convergence into LanguageProvider waits for a second
// real parser to validate the shape (ADR 0003, ADR 0014).
//
// The interface lives here, beside LanguageProvider, while its return type lives
// in core/db — mirroring how LanguageProvider.AnalyzeSurface returns
// findings.SurfaceItem. providers depends on core/db; core/db never depends on
// providers (the one-way dependency invariant of ADR 0014).
//
// The implementation is filesystem-free: it receives the already-read SourceFiles
// (content in hand), never a path. The caller resolves cfg.Database.SchemaPaths
// from disk.
type SchemaParser interface {
	ParseSchema(sources []SourceFile) (*db.Schema, error)
}
