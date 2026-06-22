// Package cache defines the content-hash finding cache (PRD section 15,
// optimization 2). Each file (and optionally each function) is hashed; if the
// hash is unchanged since the last run, its findings are reused instead of
// recomputed, so a recurring full scan costs about the same as an incremental.
//
// [Cache] hashes file content with SHA-256 and stores the findings for each
// hash as a JSON file under its directory, so unchanged files reuse their
// findings instead of being recomputed.
//
// Status: INERT. Built and tested, but not yet wired to any consumer. The MCP
// orchestrator will use it in Fase 1; kept in the core per PRD section 15.
package cache
