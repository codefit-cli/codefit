package mcp

import "context"

// Tool is the name of an MCP tool codefit exposes. All tools use the codefit-
// prefix; the codefit-surface-* family enumerates surface for the agent to
// reason about rather than detecting (PRD section 11).
type Tool string

const (
	ToolScanSecurity     Tool = "codefit-scan-security"
	ToolScanDB           Tool = "codefit-scan-db"
	ToolCheckCVEs        Tool = "codefit-check-cves"
	ToolCheckPractices   Tool = "codefit-check-practices"
	ToolScanTests        Tool = "codefit-scan-tests"
	ToolSurfaceIDOR      Tool = "codefit-surface-idor"
	ToolSurfaceAuthz     Tool = "codefit-surface-authz"
	ToolSurfaceOverfetch Tool = "codefit-surface-overfetch"
	ToolConfirmSurface   Tool = "codefit-confirm-surface"
	ToolReviewCode       Tool = "codefit-review-code"
	ToolScanAll          Tool = "codefit-scan-all"
	ToolBaseline         Tool = "codefit-baseline"
	ToolCoverage         Tool = "codefit-coverage"
)

// Server exposes codefit's sensors as the MCP tools above over a transport. It
// is stateless: each tool call is independent and carries everything it needs,
// and is a thin adapter — it translates a tool call into a core invocation and
// never reimplements audit logic (PRD section 15).
//
// Skeleton: the transport (stdio / HTTP-SSE) and the tool dispatch are
// implemented in Fase 1.
type Server interface {
	// Serve starts the MCP server until ctx is cancelled. port == 0 selects the
	// default stdio transport; a non-zero port selects HTTP/SSE.
	Serve(ctx context.Context, port int) error
}
