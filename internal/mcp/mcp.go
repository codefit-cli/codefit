package mcp

// Tool is the name of an MCP tool codefit exposes. All tools use the codefit-
// prefix; the codefit-surface-* family enumerates surface for the agent to
// reason about rather than detecting (PRD section 11).
type Tool string

const (
	ToolScanSecurity                  Tool = "codefit-scan-security"
	ToolScanDB                        Tool = "codefit-scan-db"
	ToolCheckCVEs                     Tool = "codefit-check-cves"
	ToolCheckPractices                Tool = "codefit-check-practices"
	ToolScanTests                     Tool = "codefit-scan-tests"
	ToolSurfaceIDOR                   Tool = "codefit-surface-idor"
	ToolSurfaceAuthz                  Tool = "codefit-surface-authz"
	ToolSurfaceOverfetch              Tool = "codefit-surface-overfetch"
	ToolSurfaceNPlus1                 Tool = "codefit-surface-nplus1"
	ToolConfirmSurface                Tool = "codefit-confirm-surface"
	ToolReviewCode                    Tool = "codefit-review-code"
	ToolScanAll                       Tool = "codefit-scan-all"
	ToolScanEndpoint                  Tool = "codefit-scan-endpoint"
	ToolBaselineList                  Tool = "codefit-baseline-list"
	ToolBaselineAccept                Tool = "codefit-baseline-accept"
	ToolBaselinePrune                 Tool = "codefit-baseline-prune"
	ToolBaselineRegisterAuthzHelper   Tool = "codefit-baseline-register-authz-helper"
	ToolBaselineUnregisterAuthzHelper Tool = "codefit-baseline-unregister-authz-helper"
	ToolBaselineRecordVerdict         Tool = "codefit-baseline-record-verdict"
	ToolCoverage                      Tool = "codefit-coverage"
)

// The server that exposes these tools over a transport is built by NewServer and
// run by Serve (server.go). It is stateless: each tool call is independent and
// carries everything it needs, and each tool is a thin adapter that translates a
// call into a core invocation and never reimplements audit logic (PRD §15).
// Only the stdio transport is wired today; HTTP/SSE is deferred.
