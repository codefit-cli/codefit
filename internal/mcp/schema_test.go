package mcp_test

import (
	"context"
	"encoding/json"
	"testing"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/codefit-cli/codefit/internal/mcp"
)

// TestToolSchemasAreGeminiCompatible checks every tool's input schema against the
// strict subset Google/Gemini enforces: a node's `type` must be a single string
// (never a union like ["null","array"], which Gemini rejects), and every array
// node must declare `items`. Go slices infer to a nullable union by default; the
// server must normalize that. Covers all 7 tools, not just the two that failed —
// any array without a clean type+items is the same latent bug.
func TestToolSchemasAreGeminiCompatible(t *testing.T) {
	ctx := context.Background()
	st, ct := mcpsdk.NewInMemoryTransports()
	srv := mcp.NewServer()
	go func() { _ = srv.Run(ctx, st) }()
	client := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "schema-check"}, nil)
	session, err := client.Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = session.Close() }()

	lt, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(lt.Tools) < 7 {
		t.Fatalf("expected the 7 codefit tools, got %d", len(lt.Tools))
	}
	for _, tool := range lt.Tools {
		data, err := json.Marshal(tool.InputSchema)
		if err != nil {
			t.Fatalf("%s: marshal schema: %v", tool.Name, err)
		}
		var node map[string]any
		if err := json.Unmarshal(data, &node); err != nil {
			t.Fatalf("%s: %v", tool.Name, err)
		}
		checkSchemaNode(t, tool.Name, "$", node)
	}
}

// checkSchemaNode recursively enforces: type is a single string (no union), and
// an array type carries items.
func checkSchemaNode(t *testing.T, tool, path string, node map[string]any) {
	t.Helper()
	if typ, ok := node["type"]; ok {
		if _, isList := typ.([]any); isList {
			t.Errorf("%s %s: type is a union %v — Gemini requires a single type", tool, path, typ)
		}
		if typ == "array" {
			if _, hasItems := node["items"]; !hasItems {
				t.Errorf("%s %s: array type without `items` — Gemini rejects it", tool, path)
			}
		}
	}
	if items, ok := node["items"].(map[string]any); ok {
		checkSchemaNode(t, tool, path+".items", items)
	}
	if props, ok := node["properties"].(map[string]any); ok {
		for name, p := range props {
			if pm, ok := p.(map[string]any); ok {
				checkSchemaNode(t, tool, path+".properties."+name, pm)
			}
		}
	}
	for _, key := range []string{"$defs", "definitions"} {
		if defs, ok := node[key].(map[string]any); ok {
			for name, d := range defs {
				if dm, ok := d.(map[string]any); ok {
					checkSchemaNode(t, tool, path+"."+key+"."+name, dm)
				}
			}
		}
	}
}
