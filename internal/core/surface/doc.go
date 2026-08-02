// Package surface is the core framework for surface mapping (PRD section 10):
// the language-agnostic home for surface categories and, in Fase 1, the
// SurfaceQuery format (codefit's own declarative format for enumerating
// auditable structural surface, distinct from the Semgrep-format rules used for
// deterministic findings — PRD section 17).
//
// Status: PARTIAL, and the gap is deliberate. Built: the surface categories and
// the shared framework (stable ids, fingerprinting). NOT built: the SurfaceQuery
// type and its runner — no such type exists in the tree, and an earlier draft of
// this comment claiming it would land in Fase 1 was wrong.
//
// The per-language enumeration lives in each provider's AnalyzeSurface (ADR
// 0001, provisional); core/surface will host the shared machinery once a second
// language exists to factor it against.
package surface
