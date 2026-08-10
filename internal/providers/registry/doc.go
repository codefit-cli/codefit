// Package registry is the ONE table that maps language -> provider,
// answering FOUR independent queries (by name, by extension, by marker file,
// and the complete set) so internal/mcp and internal/scaffold never build a
// concrete provider on their own and never hand-diverge into a fifth answer
// to "what can codefit do for this language" (CLAUDE.md's layering rule).
//
// The table distinguishes CAPABILITY — what a provider declares it
// implements, via LanguageProvider.Capability(), over the vocabulary that
// already lives in internal/core/surface — from EXPOSURE — which resolvers
// currently admit it (Entry.Exposure). A language can be registered here and
// deliberately NOT exposed to a given resolver; that gap is exactly what
// codefit declares to the agent rather than hiding.
//
// internal/mcp/schemaparser.go stays outside this table by design (ADR
// 0018): it resolves by the shape of the INPUT (.prisma/.sql), not by
// language.
//
// Layering: this package imports internal/providers/golang and
// internal/providers/typescript, but internal/providers (the root package,
// which owns the LanguageProvider interface and the Capability record) MUST
// NEVER import this package — that is the one edge that would let a concrete
// provider leak into the core's dependency graph. layering_test.go proves
// this holds by reading `go list -deps`, not by convention.
package registry
