package db_test

import (
	"crypto/sha256"
	"encoding/hex"
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

// wantCompleteDocHash is the sha256 of Table.Complete's doc comment (go/ast's
// CommentGroup.Text(), the same cleaned form the crossrules package's own
// go/ast lock already uses — internal/core/crossrules/
// structureproven_debt_test.go) as it read on 2026-08-04, BEFORE
// sql-ddl-phantom-index. The spec for that change verified this comment does
// NOT name film.fulltext and requires it to stay byte-identical — no edit
// needed, because it was already accurate: Complete's contract has always
// been "DROPS, not FABRICATIONS" (its own BOUNDARY paragraph says so), and
// this change closes a DROP, which is squarely inside that existing
// contract, not a boundary this doc comment ever misdescribed.
const wantCompleteDocHash = "e348cd32719ab9ae424f535b5dc1aacee50f30b30f6a478775e3624d2d4e3e21"

// TestTable_CompleteDocComment_ByteIdentical is the sql-ddl-phantom-index lock
// (spec "db.go doc comment stays byte-identical"): db.Table's Complete field
// doc comment must not change as part of this — or any future — change unless
// that change deliberately reviews and re-verifies its content. If this test
// goes red, do NOT just update the hash: read the new comment, confirm it is
// still accurate (in particular, still does not misdescribe the DROPS/
// FABRICATIONS boundary), and only then record the new hash with a note
// explaining what changed and why.
func TestTable_CompleteDocComment_ByteIdentical(t *testing.T) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "db.go", nil, parser.ParseComments)
	if err != nil {
		t.Fatalf("parsing db.go: %v", err)
	}

	doc := completeFieldDoc(t, f)
	sum := sha256.Sum256([]byte(doc))
	got := hex.EncodeToString(sum[:])
	if got != wantCompleteDocHash {
		t.Errorf("Table.Complete doc comment hash = %s, want %s (comment text below — verify it is still accurate before updating the hash):\n%s",
			got, wantCompleteDocHash, doc)
	}
}

// completeFieldDoc walks f for the Table struct's Complete field and returns
// its cleaned doc comment text (ast.CommentGroup.Text()). Fails the test
// outright if the field or its doc comment cannot be found at all — that is
// itself a structural change this lock must catch, not silently pass.
func completeFieldDoc(t *testing.T, f *ast.File) string {
	t.Helper()
	for _, decl := range f.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.TYPE {
			continue
		}
		for _, spec := range gd.Specs {
			ts, ok := spec.(*ast.TypeSpec)
			if !ok || ts.Name.Name != "Table" {
				continue
			}
			st, ok := ts.Type.(*ast.StructType)
			if !ok {
				t.Fatalf("Table is not a struct type anymore")
			}
			for _, field := range st.Fields.List {
				for _, name := range field.Names {
					if name.Name == "Complete" {
						if field.Doc == nil {
							t.Fatalf("Table.Complete has no doc comment anymore")
						}
						return field.Doc.Text()
					}
				}
			}
		}
	}
	t.Fatalf("could not find db.Table.Complete field in db.go")
	return ""
}
