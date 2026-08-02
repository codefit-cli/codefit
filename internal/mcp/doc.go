// Package mcp implements the MCP server mode: a thin, stateless adapter that
// exposes the same core sensors as MCP tools (PRD section 10). It reimplements
// no audit logic — every tool call translates to the same engine the CLI uses —
// and relies on prompt caching to neutralize the stateless overhead.
//
// Status: BUILT. NewServer registers the tool set and Serve runs it over the
// stdio transport; every tool is a thin adapter onto a core handler. The
// HTTP/SSE transport is the one part still unbuilt — `mcp serve --port` returns
// an explicit error rather than falling back silently.
package mcp
