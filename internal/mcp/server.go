package mcp

import (
	"context"
	"fmt"

	"github.com/google/jsonschema-go/jsonschema"
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
// tool, wrapping it in the SDK's typed handler signature. The adapter only
// forwards. The input schema is derived from In and then normalized for the
// strict subset Google/Gemini enforces (see geminiInputSchema).
func addTool[In, Out any](s *mcpsdk.Server, name, desc string, h func(In) (Out, error)) {
	mcpsdk.AddTool(s, &mcpsdk.Tool{Name: name, Description: desc, InputSchema: geminiInputSchema[In](name)},
		func(_ context.Context, _ *mcpsdk.CallToolRequest, in In) (*mcpsdk.CallToolResult, Out, error) {
			out, err := h(in)
			return nil, out, err
		})
}

// geminiInputSchema derives the JSON schema for In (exactly as the SDK would) and
// normalizes it for Google/Gemini, which enforces a stricter schema subset than
// other models: a node's `type` must be a single string, never a union like
// ["null","array"]. Go slices/pointers infer to a nullable union by default,
// which Gemini rejects ("$type == Type.ARRAY" / "items: missing field" on the
// array). collapseNullable drops the "null" so every array is a plain `type:
// array` with its `items`. Pre-setting Tool.InputSchema makes the SDK use this
// instead of re-inferring.
func geminiInputSchema[In any](tool string) *jsonschema.Schema {
	schema, err := jsonschema.For[In](nil)
	if err != nil {
		// Our tool input types are plain structs that always infer cleanly; a
		// failure here is a programming error, surfaced loudly at startup.
		panic(fmt.Errorf("mcp: deriving input schema for tool %q: %w", tool, err))
	}
	collapseNullable(schema)
	return schema
}

// collapseNullable recursively rewrites nullable union types ("null" + one real
// type) into that single type, throughout the schema. Required fields are not
// meaningfully nullable, and codefit's tool inputs are required.
func collapseNullable(s *jsonschema.Schema) {
	if s == nil {
		return
	}
	if len(s.Types) > 0 {
		nonNull := make([]string, 0, len(s.Types))
		for _, t := range s.Types {
			if t != "null" {
				nonNull = append(nonNull, t)
			}
		}
		switch len(nonNull) {
		case 1:
			s.Type, s.Types = nonNull[0], nil
		case 0:
			// degenerate (only "null"): leave as-is.
		default:
			s.Types = nonNull
		}
	}
	for _, sub := range []*jsonschema.Schema{
		s.Items, s.AdditionalItems, s.Contains, s.UnevaluatedItems,
		s.AdditionalProperties, s.UnevaluatedProperties, s.Not,
	} {
		collapseNullable(sub)
	}
	for _, list := range [][]*jsonschema.Schema{s.PrefixItems, s.ItemsArray, s.AllOf, s.AnyOf, s.OneOf} {
		for _, sub := range list {
			collapseNullable(sub)
		}
	}
	for _, m := range []map[string]*jsonschema.Schema{s.Properties, s.PatternProperties, s.Defs, s.Definitions} {
		for _, sub := range m {
			collapseNullable(sub)
		}
	}
}
