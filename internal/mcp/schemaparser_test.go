package mcp

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSchemaParserForPaths_ByInput(t *testing.T) {
	root := t.TempDir()

	if p, note := schemaParserForPaths(root, []string{"prisma/schema.prisma"}); p == nil || note != "" {
		t.Errorf(".prisma should resolve a parser, got nil=%v note=%q", p == nil, note)
	}
	if p, note := schemaParserForPaths(root, []string{"db/migration/V1__x.sql"}); p == nil || note != "" {
		t.Errorf(".sql should resolve a parser, got nil=%v note=%q", p == nil, note)
	}
	// A directory (no extension) is treated as a SQL migration dir.
	if err := os.MkdirAll(filepath.Join(root, "db", "migration"), 0o755); err != nil {
		t.Fatal(err)
	}
	if p, note := schemaParserForPaths(root, []string{"db/migration"}); p == nil || note != "" {
		t.Errorf("a directory should resolve the SQL parser, got nil=%v note=%q", p == nil, note)
	}
	// Mixed .prisma + .sql is a declared out-of-scope limit → no parser + note.
	if p, note := schemaParserForPaths(root, []string{"a.prisma", "b.sql"}); p != nil || note == "" {
		t.Errorf("mixed schema types must not resolve (nil + note), got nil=%v note=%q", p == nil, note)
	}
	// The relocated "no parser" case: an unrecognized schema type.
	if p, note := schemaParserForPaths(root, []string{"schema.txt"}); p != nil || note == "" {
		t.Errorf("unrecognized schema type must not resolve (nil + note), got nil=%v note=%q", p == nil, note)
	}
}
