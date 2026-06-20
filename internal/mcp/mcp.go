package mcp

import "context"

// Server exposes codefit's sensors as MCP tools over a transport. It is
// stateless: each tool call is independent and carries everything it needs.
//
// Skeleton: no implementation yet.
type Server interface {
	// Serve starts the MCP server until ctx is cancelled. port == 0 selects the
	// default stdio transport; a non-zero port selects HTTP/SSE.
	Serve(ctx context.Context, port int) error
}
