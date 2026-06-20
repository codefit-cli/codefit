// Package mcp implements the MCP server mode: a thin, stateless adapter that
// exposes the same core sensors as MCP tools (PRD section 10). It reimplements
// no audit logic — every tool call translates to the same engine the CLI uses —
// and relies on prompt caching to neutralize the stateless overhead.
//
// Skeleton: this declares the [Server] contract. No transport (stdio/HTTP) or
// tool dispatch is implemented yet.
package mcp
