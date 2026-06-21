// Package surface is the core framework for surface mapping (PRD section 10):
// the language-agnostic home for surface categories and, in Fase 1, the
// SurfaceQuery format (codefit's own declarative format for enumerating
// auditable structural surface, distinct from the Semgrep-format rules used for
// deterministic findings — PRD section 17).
//
// Status: SKELETON. Today it declares the surface categories. The aggregation
// framework and the SurfaceQuery type and runner are implemented in Fase 1.
// The per-language enumeration currently lives in each provider's
// AnalyzeSurface (ADR 0001, provisional); core/surface will host the shared
// machinery once a second language exists to factor it against.
package surface
