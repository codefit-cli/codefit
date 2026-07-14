package mcp_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/codefit-cli/codefit/internal/mcp"
)

// TestServerProtocolEndToEnd is the real risk of this slice: not the handlers
// (already tested) but that the server speaks the protocol. A real MCP client
// connects to the real codefit server over a paired transport, performs the
// handshake, lists the tools, calls one, and gets the correct report back. This
// exercises the full protocol (initialize → tools/list → tools/call), not just
// the handler.
func TestServerProtocolEndToEnd(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	root := t.TempDir()
	mustWrite(t, root, "app/users/[id]/route.ts", `
export async function GET(req: Request, { params }: { params: { id: string } }) {
  return Response.json(await prisma.user.findUnique({ where: { id: params.id } }));
}`)

	// A connected client/server transport pair — the same Server.Run path used for
	// stdio, exercised in-process.
	serverT, clientT := mcpsdk.NewInMemoryTransports()
	srv := mcp.NewServer()
	go func() { _ = srv.Run(ctx, serverT) }()

	client := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "codefit-itest", Version: "0"}, nil)
	session, err := client.Connect(ctx, clientT, nil) // initialize handshake
	if err != nil {
		t.Fatalf("client connect/handshake: %v", err)
	}
	defer func() { _ = session.Close() }()

	// tools/list — the codefit tools must be advertised.
	lt, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("tools/list: %v", err)
	}
	names := map[string]bool{}
	for _, tool := range lt.Tools {
		names[tool.Name] = true
	}
	for _, want := range []string{"codefit-scan-all", "codefit-surface-idor", "codefit-surface-nplus1", "codefit-coverage", "codefit-confirm-surface", "codefit-check-cves", "codefit-scan-db"} {
		if !names[want] {
			t.Errorf("tool %q not advertised; got %v", want, names)
		}
	}

	// tools/call codefit-scan-all over the fixture project.
	res, err := session.CallTool(ctx, &mcpsdk.CallToolParams{
		Name:      "codefit-scan-all",
		Arguments: map[string]any{"root": root, "language": "typescript"},
	})
	if err != nil {
		t.Fatalf("tools/call: %v", err)
	}
	if res.IsError {
		t.Fatalf("tool reported an error: %+v", res.Content)
	}

	// The structured result must be the codefit report, anchored per endpoint.
	var out mcp.ScanAllResponse
	data, _ := json.Marshal(res.StructuredContent)
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("decode structured result: %v (raw: %s)", err, data)
	}
	if out.Summary.Endpoints == 0 || out.Summary.SurfaceItems == 0 {
		t.Errorf("expected a per-endpoint report with surface, got %+v", out.Summary)
	}
	if len(out.Actionable) == 0 || len(out.Actionable[0].Concerns) == 0 {
		t.Errorf("expected at least one actionable endpoint with concerns, got %+v", out.Actionable)
	}
}
