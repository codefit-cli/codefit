package baseline_test

import (
	"reflect"
	"testing"

	"github.com/codefit-cli/codefit/internal/core/baseline"
	"github.com/codefit-cli/codefit/internal/core/scope"
)

func itemByFP(items []baseline.Item, fp string) (baseline.Item, bool) {
	for _, it := range items {
		if it.FP == fp {
			return it, true
		}
	}
	return baseline.Item{}, false
}

// twoFileBaseline: two security items the security sensor owns, in two different
// files. Both categories are always "scanned" — the ONLY variable in these tests
// is the file scope.
func twoFileBaseline() *baseline.Baseline {
	return &baseline.Baseline{Version: "1", Items: []baseline.Item{
		{FP: "inScopeFP", Category: "overfetch", File: "src/touched.ts", Snippet: "select *"},
		{FP: "outOfScopeFP", Category: "overfetch", File: "src/untouched.ts", Snippet: "select *",
			Ack: &baseline.Ack{Reason: "accepted debt", At: "2026-01-02", By: "human"}},
	}}
}

// Test contract item 2: an item that IS in the file scope, whose category ran,
// and that the pass did not observe, is gone — today's behaviour, kept.
func TestDiff_InFileScope_Unobserved_IsGone(t *testing.T) {
	res := baseline.Diff(twoFileBaseline(), nil, secScope(), scope.Of([]string{"src/touched.ts"}))

	if !inGone(res, "inScopeFP") {
		t.Error("an in-file-scope, unobserved item whose category ran MUST be gone")
	}
	if res.Counts.Gone != 1 {
		t.Errorf("Gone = %d, want 1 (only the in-scope item)", res.Counts.Gone)
	}
}

// Test contract item 3 — THE corruption test. A security finding in a file this
// pass never opened still belongs to the security category, which DID run. A
// category-only guard would call it gone and codefit-baseline-prune would delete
// it: the audit memory of every file a partial scan did not look at, erased
// silently. It must be absent from the delta and carried forward VERBATIM.
func TestDiff_OutOfFileScope_Unobserved_NotGone(t *testing.T) {
	prev := twoFileBaseline()
	original, _ := itemByFP(prev.Items, "outOfScopeFP")

	res := baseline.Diff(prev, nil, secScope(), scope.Of([]string{"src/touched.ts"}))

	if inGone(res, "outOfScopeFP") {
		t.Error("an item in a file the pass never opened MUST NOT be gone — pruning it deletes unverified audit memory")
	}
	if _, ok := res.State["outOfScopeFP"]; ok {
		t.Error("an out-of-file-scope item must be absent from the delta (State)")
	}
	if res.Shown["outOfScopeFP"] {
		t.Error("an out-of-file-scope item must not be shown — the pass concluded nothing about it")
	}
	carried, ok := itemByFP(res.Next.Items, "outOfScopeFP")
	if !ok {
		t.Fatal("an out-of-file-scope item must be carried forward to Next.Items")
	}
	if !reflect.DeepEqual(carried, original) {
		t.Errorf("carried forward item = %+v, want VERBATIM %+v", carried, original)
	}
}

// Test contract item 4: the zero-value Scope includes nothing, so a caller that
// forgets to pass a file scope prunes NOTHING. It under-reports; it never
// corrupts. This is why Full() is an explicit constructor and not the zero value.
func TestDiff_ZeroValueFileScope_PrunesNothing(t *testing.T) {
	prev := twoFileBaseline()
	var unset scope.Scope

	res := baseline.Diff(prev, nil, secScope(), unset)

	if res.Counts.Gone != 0 {
		t.Errorf("zero-value file scope: Gone = %d, want 0", res.Counts.Gone)
	}
	if len(res.Gone) != 0 {
		t.Errorf("zero-value file scope: Gone list = %v, want empty", res.Gone)
	}
	if len(res.Next.Items) != len(prev.Items) {
		t.Errorf("zero-value file scope: carried %d of %d items forward", len(res.Next.Items), len(prev.Items))
	}
}

// The two dimensions are ANDed, not ORed: being in the file scope does not
// rescue an item whose sensor did not run.
func TestDiff_InFileScope_ButCategoryDidNotRun_NotGone(t *testing.T) {
	prev := &baseline.Baseline{Version: "1", Items: []baseline.Item{
		{FP: "dbfp", Category: "db-fk-no-index", File: "prisma/schema.prisma"},
	}}
	res := baseline.Diff(prev, nil, secScope(), scope.Of([]string{"prisma/schema.prisma"}))
	if inGone(res, "dbfp") {
		t.Error("the file scope must not override the category scope — both conditions are required")
	}
}

// The file scope canonicalizes on lookup too: a baseline item recorded with a
// Windows separator (or a scope requested with one) is the same file.
func TestDiff_FileScope_CanonicalizesTheItemPath(t *testing.T) {
	prev := &baseline.Baseline{Version: "1", Items: []baseline.Item{
		{FP: "fp", Category: "overfetch", File: `src\touched.ts`},
	}}
	res := baseline.Diff(prev, nil, secScope(), scope.Of([]string{"./src/touched.ts"}))
	if !inGone(res, "fp") {
		t.Error("a baseline path spelled with a Windows separator must match the same scoped file")
	}
}
