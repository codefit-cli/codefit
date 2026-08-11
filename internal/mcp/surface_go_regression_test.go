package mcp_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/codefit-cli/codefit/internal/mcp"
)

// TestSurfaceAuthz_GoHandler_NoStructuralFactsRegression reproduces, end to end
// through the REAL server, the regression shipped hours before this fix:
// internal/providers/golang/surface.go never set SurfaceItem.StructuralFacts, so
// the nil map marshaled to JSON `null`; the SDK's output-schema validation then
// rejects the tool's own response ("structural_facts ... has type \"null\", want
// \"object\"") and codefit-surface-authz (and codefit-scan-security) hard-fail on
// any Go project with an HTTP handler. A struct-level assertion would have passed
// while the tool itself errored — this test drives the real MCP protocol
// (initialize -> tools/call) against the real go/ast-backed provider, so the
// proof is the tool, not the type.
func TestSurfaceAuthz_GoHandler_NoStructuralFactsRegression(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	serverT, clientT := mcpsdk.NewInMemoryTransports()
	srv := mcp.NewServer()
	go func() { _ = srv.Run(ctx, serverT) }()

	client := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "codefit-itest", Version: "0"}, nil)
	session, err := client.Connect(ctx, clientT, nil)
	if err != nil {
		t.Fatalf("client connect/handshake: %v", err)
	}
	defer func() { _ = session.Close() }()

	src := `package handlers

import "net/http"

func ListWidgets(w http.ResponseWriter, r *http.Request) {
	w.Write([]byte("ok"))
}
`
	res, err := session.CallTool(ctx, &mcpsdk.CallToolParams{
		Name: "codefit-surface-authz",
		Arguments: map[string]any{
			"files": []map[string]any{
				{"path": "handlers.go", "content": src},
			},
		},
	})
	if err != nil {
		t.Fatalf("codefit-surface-authz over a real Go HTTP handler must not error the protocol call: %v", err)
	}
	if res.IsError {
		t.Fatalf("tool reported an error: %+v", res.Content)
	}

	var out mcp.SurfaceResponse
	data, _ := json.Marshal(res.StructuredContent)
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("decode structured result: %v (raw: %s)", err, data)
	}
	if len(out.Surface) == 0 {
		t.Fatalf("expected at least one authz surface item for the Go handler, got none")
	}
	for _, it := range out.Surface {
		if it.StructuralFacts == nil {
			t.Errorf("surface item %+v carries a nil StructuralFacts", it)
		}
	}
}
