package typescript_test

import (
	"testing"

	"github.com/codefit-cli/codefit/internal/providers"
	"github.com/codefit-cli/codefit/internal/providers/typescript"
)

// N1 (design §1-D1b, spec DELTA 1) — the zero-value trap on the Prisma path.
// db.Table.Complete's zero value is false (fail-closed). parseModel is the
// TS/Prisma provider's sole construction site: it MUST set Complete=true
// explicitly, or every table of every Prisma project mutes DB-050/DB-001/
// DB-052 and the whole DW family — the project's OWN dogfood route (Next.js/
// Prisma). This test goes RED against the zero-value the moment db.Table
// gains the field (WU-1) and stays red until parseModel sets it explicitly.
func TestPrisma_WellFormedModel_IsComplete(t *testing.T) {
	p := &typescript.Provider{}
	src := "model User {\n  id Int @id\n  name String\n}\n"
	s, err := p.ParseSchema([]providers.SourceFile{{Path: "x.prisma", Content: []byte(src)}})
	if err != nil {
		t.Fatalf("ParseSchema: %v", err)
	}
	if len(s.Tables) != 1 {
		t.Fatalf("parsed %d tables, want 1", len(s.Tables))
	}
	tb := s.Tables[0]
	if !tb.Complete {
		t.Error("Complete = false, want true — a well-formed Prisma model with no unrecognized body lines must be proven complete (N1)")
	}
	if tb.Note != "" {
		t.Errorf("Note = %q, want empty on a complete table", tb.Note)
	}
	if tb.Unreduced != nil {
		t.Errorf("Unreduced = %v, want nil on a complete table", tb.Unreduced)
	}
}
