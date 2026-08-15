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
	if out.Summary.Security.Endpoints == 0 || out.Summary.Security.SurfaceItems == 0 {
		t.Errorf("expected a per-endpoint report with surface, got %+v", out.Summary)
	}
	// Named, not inlined: the endpoint arrives with what it takes to rank it, and
	// its concern detail is a codefit-scan-endpoint call away (ADR 0054).
	if len(out.Actionable.Endpoints) == 0 || out.Actionable.Endpoints[0].Concerns == 0 {
		t.Errorf("expected at least one actionable endpoint carrying a concern count, got %+v", out.Actionable)
	}
	if out.Budget.Bytes != mcp.ResponseBudgetBytes || out.Budget.Note == "" {
		t.Errorf("the response an agent receives over the wire must declare its budget, got %+v", out.Budget)
	}

	// tools/call codefit-coverage — this test LISTED the tool for a long time and
	// never called it, so nothing here had ever measured what an agent actually
	// receives from it. It does now.
	cov, err := session.CallTool(ctx, &mcpsdk.CallToolParams{
		Name:      "codefit-coverage",
		Arguments: map[string]any{"language": "typescript"},
	})
	if err != nil {
		t.Fatalf("tools/call codefit-coverage: %v", err)
	}
	if cov.IsError {
		t.Fatalf("codefit-coverage reported an error: %+v", cov.Content)
	}

	structured, err := json.Marshal(cov.StructuredContent)
	if err != nil {
		t.Fatalf("marshal structured coverage result: %v", err)
	}
	t.Logf("WIRE MEASUREMENT — codefit-coverage {language: typescript}: structured payload %d bytes", len(structured))

	var covOut mcp.CoverageResponse
	if err := json.Unmarshal(structured, &covOut); err != nil {
		t.Fatalf("decode structured coverage result: %v", err)
	}
	if len(covOut.Index) == 0 {
		t.Fatal("vacuum: the coverage index arrived empty over the wire")
	}
	t.Logf("WIRE MEASUREMENT — %d entries, payload %d bytes (index %d), over budget %v, withheld %d",
		covOut.Entries, covOut.Bytes, covOut.IndexBytes, covOut.OverBudget, covOut.Withheld)
	// The declared size describes what an agent actually received, which for an
	// index-only call is the index. A detail call declares the detail too — see
	// TestCoverage_ADetailResponseDeclaresTheSizeOfWhatItReturns.
	if covOut.Bytes != covOut.IndexBytes {
		t.Errorf("no detail was asked for, so the declared payload IS the index: bytes=%d index_bytes=%d",
			covOut.Bytes, covOut.IndexBytes)
	}
	if covOut.Withheld != 0 || covOut.WithheldNote == "" {
		t.Errorf("the response an agent receives must state that it withheld nothing, got withheld=%d note=%q",
			covOut.Withheld, covOut.WithheldNote)
	}
	if len(structured) >= 143_557 {
		t.Errorf("the coverage response is %d bytes over the wire — no smaller than the 143,557-byte payload this change exists to replace",
			len(structured))
	}

	// EMPIRICAL PROOF, recorded rather than fixed: codefit's addTool returns nil
	// for the *CallToolResult, and the go-sdk then serializes the SAME output
	// JSON into a TextContent block. Every codefit tool response therefore
	// crosses the wire TWICE. This asserts the two copies are identical, which is
	// what makes it duplication rather than two different answers. Which copy the
	// client meters is UNMEASURED — see the roadmap item; do not assume.
	if len(cov.Content) != 1 {
		t.Fatalf("expected exactly one content block alongside the structured result, got %d", len(cov.Content))
	}
	text, ok := cov.Content[0].(*mcpsdk.TextContent)
	if !ok {
		t.Fatalf("expected the content block to be TextContent, got %T", cov.Content[0])
	}
	if text.Text != string(structured) {
		t.Errorf("the text block and the structured payload differ, so this is not the duplication the finding describes:\n text %d bytes\n structured %d bytes",
			len(text.Text), len(structured))
	}
	t.Logf("WIRE DUPLICATION — the same %d-byte payload is carried twice: once as structured content and once as a text block, %d bytes on the wire in total",
		len(structured), len(structured)+len(text.Text))
}
