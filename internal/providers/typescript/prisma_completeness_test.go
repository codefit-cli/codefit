package typescript_test

import (
	"strings"
	"testing"

	"github.com/codefit-cli/codefit/internal/core/db"
	"github.com/codefit-cli/codefit/internal/providers"
	"github.com/codefit-cli/codefit/internal/providers/typescript"
)

// Spec "Domain: Prisma Schema Completeness" (DELTA 2), design SS7b —
// prismaschema.go:153 (`if m == nil { continue }`, inside parseModel with
// &t in scope) must mark the table unproven instead of silently continuing.
// This is separate from N1's default (prismaschema.go:145, tested in
// completeness_construction_test.go), which this narrows for lines the
// parser actually drops.
func TestPrisma_UnrecognizedModelBodyLine_MarksUnproven(t *testing.T) {
	p := &typescript.Provider{}
	// A multi-line block-attribute continuation: line 2 of the @@index
	// attribute trims to `map: "idx_foo")` — not @@-prefixed, and fieldLine
	// requires whitespace after the first identifier while the next char is
	// ':', so it fails to match and falls to :153 (design SS7b's candidate
	// fixture, code-read prediction — verified here by running it).
	src := "model Foo {\n  id Int @id\n  @@index([id],\n    map: \"idx_foo\")\n}\n"
	s, err := p.ParseSchema([]providers.SourceFile{{Path: "x.prisma", Content: []byte(src)}})
	if err != nil {
		t.Fatalf("ParseSchema: %v", err)
	}
	if len(s.Tables) != 1 {
		t.Fatalf("parsed %d tables, want 1", len(s.Tables))
	}
	tb := s.Tables[0]
	t.Logf("actual output: Complete=%v Note=%q Unreduced=%v", tb.Complete, tb.Note, tb.Unreduced)
	if tb.Complete {
		t.Fatal("Complete = true, want false — the unrecognized continuation line must mark the table unproven")
	}
	if tb.Note == "" || !strings.Contains(tb.Note, string(db.ReasonUnreducedTableStatement)) {
		t.Errorf("Note = %q, want it to contain ReasonUnreducedTableStatement", tb.Note)
	}
	if len(tb.Unreduced) != 1 {
		t.Fatalf("Unreduced = %d entries, want 1", len(tb.Unreduced))
	}
}

// Deferred-debt lock (design SS7b): prismaschema.go:92 (a stray TOP-LEVEL
// line, block-header granularity, no *db.Table in scope) stays OUT of
// scope. A schema with an unrecognized top-level block (a view block,
// declared out by ADR 0014) still yields Complete=true on the real tables —
// asserts TODAY's behavior so the debt is executable and any future fill
// turns it red.
func TestPrisma_UnrecognizedTopLevelBlock_DeferredDebt_TablesStayComplete(t *testing.T) {
	p := &typescript.Provider{}
	src := "view Foo {\n  id Int\n}\n\nmodel Bar {\n  id Int @id\n}\n"
	s, err := p.ParseSchema([]providers.SourceFile{{Path: "x.prisma", Content: []byte(src)}})
	if err != nil {
		t.Fatalf("ParseSchema: %v", err)
	}
	if len(s.Tables) != 1 {
		t.Fatalf("parsed %d tables, want 1 (Bar only — view blocks are skipped)", len(s.Tables))
	}
	if !s.Tables[0].Complete {
		t.Error("Complete = false, want true — an unrecognized TOP-LEVEL block (prismaschema.go:92) is deferred debt, distinct from an unrecognized MODEL BODY line (:153)")
	}
}
