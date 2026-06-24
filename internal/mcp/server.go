package mcp

import (
	"context"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/codefit-cli/codefit/internal/version"
)

// NewServer builds the codefit MCP server with its tools registered. Each tool is
// a THIN adapter: it hands the SDK's typed request to the core handler that
// already exists and is tested, and returns the core's result as structured
// output. No audit logic lives here — the MCP layer only connects the protocol
// to the engine (PRD §15). The server is stateless: every tool call is
// independent and carries everything it needs.
func NewServer() *mcpsdk.Server {
	s := mcpsdk.NewServer(&mcpsdk.Implementation{Name: "codefit", Version: version.Version}, nil)

	addTool(s, string(ToolScanSecurity),
		"Run the deterministic security rules and the mapped surface over a project. Input: {root, language}. Returns findings + surface + score + blocked.",
		HandleScanSecurity)
	addTool(s, string(ToolScanAll),
		"The complete picture aggregated per endpoint: deterministic findings and surface concerns of each handler together, with three certainty levels, ordered by actionable gap. Input: {root, language}.",
		HandleScanAll)
	addTool(s, string(ToolSurfaceIDOR),
		"Enumerate the IDOR surface (id→resource endpoints) for the agent to reason about ownership checks. Input: {files:[{path, content}]}.",
		HandleSurfaceIDOR)
	addTool(s, string(ToolSurfaceAuthz),
		"Enumerate the broken-authorization surface (handlers doing something sensitive), ordered unchecked-first. Input: {files:[{path, content}]}.",
		HandleSurfaceAuthz)
	addTool(s, string(ToolSurfaceOverfetch),
		"Enumerate the over-fetching surface (domain-object serializations), ordered by structural certainty. Input: {files:[{path, content}]}.",
		HandleSurfaceOverfetch)
	addTool(s, string(ToolConfirmSurface),
		"Integrate the agent's verdicts on surface items: vulnerable ones become probabilistic findings (confidence < 1.0) anchored to the item. Stateless: codefit recomputes the id to validate.",
		func(in ConfirmSurfaceRequest) (ConfirmSurfaceResponse, error) { return HandleConfirmSurface(in), nil })
	addTool(s, string(ToolCoverage),
		"Return the coverage manifest for a language: what codefit audits deterministically vs reasons over surface vs does not cover. Input: {language}.",
		HandleCoverage)

	return s
}

// Serve runs the codefit MCP server over the stdio transport until ctx is
// cancelled. (HTTP/SSE is deferred; the SDK abstracts the transport, so it is
// added later without a refactor.)
func Serve(ctx context.Context) error {
	return NewServer().Run(ctx, &mcpsdk.StdioTransport{})
}

// addTool registers a codefit core handler (func(In) (Out, error)) as an MCP
// tool, wrapping it in the SDK's typed handler signature. The SDK derives the
// input/output JSON schema from the In/Out types and marshals both sides — the
// adapter only forwards.
func addTool[In, Out any](s *mcpsdk.Server, name, desc string, h func(In) (Out, error)) {
	mcpsdk.AddTool(s, &mcpsdk.Tool{Name: name, Description: desc},
		func(_ context.Context, _ *mcpsdk.CallToolRequest, in In) (*mcpsdk.CallToolResult, Out, error) {
			out, err := h(in)
			return nil, out, err
		})
}
