package cli_test

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/codefit-cli/codefit/internal/mcp"
)

// TestMCPServeCommandStartsServer closes the gap the integration test left: it
// exercises the COMMAND PATH a user/agent actually runs — `codefit mcp serve` —
// not the server in isolation. It builds the real binary and connects to it the
// way an agent does (the SDK's CommandTransport spawns the process over stdio),
// then does the handshake, lists the tools, and calls one. If `mcp serve` were
// the old scaffolding stub, the process would print "not implemented" and exit,
// the handshake would fail, and this test would catch it.
func TestMCPServeCommandStartsServer(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the binary; skipped in -short")
	}
	bin := buildCodefit(t)

	root := t.TempDir()
	writeFixture(t, root, "app/users/[id]/route.ts", `
export async function GET(req: Request, { params }: { params: { id: string } }) {
  return Response.json(await prisma.user.findUnique({ where: { id: params.id } }));
}`)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// The real agent path: spawn `codefit mcp serve` and talk MCP over its stdio.
	client := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "codefit-e2e", Version: "0"}, nil)
	session, err := client.Connect(ctx, &mcpsdk.CommandTransport{Command: exec.Command(bin, "mcp", "serve")}, nil)
	if err != nil {
		t.Fatalf("`codefit mcp serve` did not start a working MCP server (handshake failed): %v", err)
	}
	defer func() { _ = session.Close() }()

	lt, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("tools/list against the spawned binary: %v", err)
	}
	found := false
	for _, tool := range lt.Tools {
		if tool.Name == "codefit-scan-all" {
			found = true
		}
	}
	if !found {
		t.Fatal("the spawned `codefit mcp serve` did not advertise codefit-scan-all")
	}

	res, err := session.CallTool(ctx, &mcpsdk.CallToolParams{
		Name:      "codefit-scan-all",
		Arguments: map[string]any{"root": root, "language": "typescript"},
	})
	if err != nil {
		t.Fatalf("tools/call against the spawned binary: %v", err)
	}
	if res.IsError {
		t.Fatalf("tool reported an error: %+v", res.Content)
	}
	var out mcp.ScanAllResponse
	data, _ := json.Marshal(res.StructuredContent)
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("decode report from the spawned server: %v", err)
	}
	if out.Summary.Endpoints == 0 {
		t.Errorf("expected a per-endpoint report from the real binary, got %+v", out.Summary)
	}
}

// buildCodefit builds the codefit binary to a temp path for the e2e test.
func buildCodefit(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "codefit")
	out, err := exec.Command("go", "build", "-o", bin, "github.com/codefit-cli/codefit/cmd/codefit").CombinedOutput()
	if err != nil {
		t.Fatalf("building codefit: %v\n%s", err, out)
	}
	return bin
}

func writeFixture(t *testing.T, root, rel, content string) {
	t.Helper()
	p := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
