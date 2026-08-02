package mcp

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/codefit-cli/codefit/internal/providers"
	"github.com/codefit-cli/codefit/internal/providers/typescript"
)

// collectQueryFilters is the THIRD reader that handed os.ReadFile's raw bytes
// to a tokenizer (internal/mcp/scanall.go), after the DB and security sensors.
// Its failure mode is milder — the code×schema cross emits surface items, never
// an affirmation, so an unread file costs QUESTIONS rather than producing a
// false finding — but it is the same blindness and it is silent in the same
// way, so it takes the same decode.
//
// The assertion compares a BOM-marked file against its UTF-8 twin rather than
// against a literal count, so it stays a statement about ENCODING even if the
// extractor's rules change.
func TestCollectQueryFilters_BOMMarkedEncodings_ExtractWhatUTF8Extracts(t *testing.T) {
	const src = `import { prisma } from "./db";
export async function listOrders(customerId: string) {
  return prisma.orders.findMany({ where: { customer_id: customerId, status: "open" } });
}
`
	utf16le := func(s string) []byte {
		out := []byte{0xFF, 0xFE}
		for _, r := range s {
			out = append(out, byte(r), 0x00)
		}
		return out
	}

	extract := func(name string, data []byte) int {
		root := t.TempDir()
		if err := os.WriteFile(filepath.Join(root, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
		p := typescript.New()
		qe, ok := any(p).(providers.QueryExtractor)
		if !ok {
			t.Fatal("the typescript provider no longer implements QueryExtractor — this test would silently measure nothing")
		}
		return len(collectQueryFilters(root, p.FileExtensions(), qe))
	}

	want := extract("orders.ts", []byte(src))
	if want == 0 {
		t.Fatalf("the UTF-8 reference run extracted no filter — the fixture exercises nothing, so the comparisons below would pass vacuously")
	}
	for name, data := range map[string][]byte{
		"utf-8 with BOM":    append([]byte{0xEF, 0xBB, 0xBF}, []byte(src)...),
		"utf-16le with BOM": utf16le(src),
	} {
		t.Run(name, func(t *testing.T) {
			if got := extract("orders.ts", data); got != want {
				t.Fatalf("filters = %d, want the UTF-8 twin's %d", got, want)
			}
		})
	}
}
