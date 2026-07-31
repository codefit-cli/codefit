package typescript_test

import (
	"testing"

	"github.com/codefit-cli/codefit/internal/providers"
	"github.com/codefit-cli/codefit/internal/providers/typescript"
)

// index-method-capture: parseBlockAttr (prismaschema.go) previously read
// @@index([...]) via bracketList(text) and discarded the rest of the
// attribute — a "ceguera por omisión" (obs #1307): the provider reads the
// source perfectly but never extracts the type: argument. This drives the
// REAL parser end to end (ParseSchema), never a hand-built db.Index literal.
//
// Deliberately NOT asserting against an assumed Prisma vocabulary: Prisma's
// exact set of accepted index types has not been verified against its own
// spec by this project, so these tests only assert "whatever the schema
// text says, lowercased, comes through verbatim" — never a codefit-owned
// enum.
func TestPrisma_AtAtIndex_TypeArgument_CapturedAsMethod(t *testing.T) {
	tests := []struct {
		name       string
		attrLine   string
		wantMethod string
	}{
		{
			name:       "type: Hash lowercased",
			attrLine:   `  @@index([authorId], type: Hash)`,
			wantMethod: "hash",
		},
		{
			name:       "type: Gin — arbitrary value, not validated against any enum",
			attrLine:   `  @@index([tags], type: Gin)`,
			wantMethod: "gin",
		},
		{
			name:       "type: BRIN mixed case still lowercased",
			attrLine:   `  @@index([createdAt], type: BRIN)`,
			wantMethod: "brin",
		},
		{
			name:       "no type: argument leaves Method empty",
			attrLine:   `  @@index([authorId, title])`,
			wantMethod: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			src := "model Post {\n  id       Int    @id\n  authorId Int\n  title    String\n  tags     String[]\n  createdAt DateTime\n" +
				tt.attrLine + "\n}\n"
			p := &typescript.Provider{}
			s, err := p.ParseSchema([]providers.SourceFile{{Path: "x.prisma", Content: []byte(src)}})
			if err != nil {
				t.Fatalf("ParseSchema: %v", err)
			}
			if len(s.Tables) != 1 {
				t.Fatalf("parsed %d tables, want 1", len(s.Tables))
			}
			tb := s.Tables[0]
			if !tb.Complete {
				t.Fatalf("Complete = false, want true. Note=%q Unreduced=%v", tb.Note, tb.Unreduced)
			}
			if len(tb.Indexes) != 1 {
				t.Fatalf("Indexes = %v, want exactly 1", tb.Indexes)
			}
			if got := tb.Indexes[0].Method; got != tt.wantMethod {
				t.Errorf("Method = %q, want %q", got, tt.wantMethod)
			}
		})
	}
}

// TestPrisma_AtUnique_NeverCapturesType locks that @@unique (a different
// block attribute, does not accept a type: argument in Prisma's own
// grammar) is unaffected: Method stays empty even if a stray "type:"-looking
// substring appeared nearby in a different attribute on the same model.
func TestPrisma_AtAtUnique_MethodStaysEmpty(t *testing.T) {
	src := "model Membership {\n  userId  Int\n  groupId Int\n  @@unique([userId, groupId])\n}\n"
	p := &typescript.Provider{}
	s, err := p.ParseSchema([]providers.SourceFile{{Path: "x.prisma", Content: []byte(src)}})
	if err != nil {
		t.Fatalf("ParseSchema: %v", err)
	}
	tb := s.Tables[0]
	if len(tb.Indexes) != 1 {
		t.Fatalf("Indexes = %v, want exactly 1", tb.Indexes)
	}
	if got := tb.Indexes[0].Method; got != "" {
		t.Errorf("Method = %q, want empty — @@unique carries no type: argument in Prisma's grammar", got)
	}
}
