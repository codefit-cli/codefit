package paradigm_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/codefit-cli/codefit/internal/core/db"
	"github.com/codefit-cli/codefit/internal/core/paradigm"
)

// STAGE 1 IS INERT, and this file is the lock that says so.
//
// The schema gate computes five schema-wide warehouse signals and is wired to
// NOTHING: Detect, roleFor, fold, Resolve and the sensor's suppress3NF must all
// behave exactly as they did before it existed. The signals are here to be
// MEASURED over real corpora; stage 2 decides what to do with them, from the
// measurement rather than from a hunch.
//
// "Inert" is a claim about the whole repository, so it is checked against the
// whole repository — not asserted in a comment. Two independent locks:
//
//  1. STRUCTURAL (TestSchemaGate_IsCalledByNoProductionCode): no non-test Go
//     file anywhere references any gate symbol. This is the lock stage 2 has to
//     deliberately retire, which is the point — wiring the gate in should be a
//     visible act, not a quiet one.
//  2. BEHAVIORAL (TestSchemaGate_DoesNotMoveDetect): over the exact schema
//     shape that motivated the whole inversion, the gate fires and Detect does
//     not move.

// gateSymbols are the schema gate's identifiers. The exported ones are what
// another package could reach; the unexported ones are what a sibling file in
// this package could reach.
var gateSymbols = struct {
	exported   []string
	unexported []string
}{
	exported: []string{
		"WarehouseSignals", "WarehouseEvidence", "Signal",
		"SignalCalendarTable", "SignalSurrogateKeyNames", "SignalBulkLoadShape",
		"SignalNoAuditTimestamps", "SignalStarTopology",
	},
	unexported: []string{
		"allSignals", "judgeable", "minJudgeableTables",
		"hasCalendarTable", "hasSurrogateKeyVocabulary", "hasBulkLoadShape",
		"hasNoAuditTimestamps", "hasStarTopology",
		"calendarNames", "keyLikeSegments", "starFanOutMin",
		"surrogateKeyColumnsMin", "surrogateKeyTablesMin",
		"bulkKeyColumnsInOneTableMin", "bulkKeyColumnsSchemaMin",
		"normalizeGateIdent", "trailingSegment",
	},
}

const gateFile = "schemagate.go"

// gateReference is one place a gate symbol is used.
type gateReference struct {
	file   string
	symbol string
}

// scanGateReferences parses every non-test .go file under root and reports
// every reference to a gate symbol, skipping the files named in skip.
//
// The scan is AST-based, not textual: a comment or a doc string naming
// WarehouseSignals is prose, not a call, and a textual grep could not tell them
// apart. Outside internal/core/paradigm only the QUALIFIED form
// (paradigm.WarehouseSignals) counts, so an unrelated local identifier in
// another package can never raise a false alarm.
func scanGateReferences(t *testing.T, root string, skip map[string]bool) (refs []gateReference, filesScanned int) {
	t.Helper()

	exported := map[string]bool{}
	for _, s := range gateSymbols.exported {
		exported[s] = true
	}
	inPackage := map[string]bool{}
	for _, s := range append(append([]string{}, gateSymbols.exported...), gateSymbols.unexported...) {
		inPackage[s] = true
	}
	paradigmDir := filepath.Join("internal", "core", "paradigm")

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			name := d.Name()
			if path != root && (strings.HasPrefix(name, ".") || name == "testdata" || name == "dist" || name == "bin") {
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		if skip[rel] {
			return nil
		}
		filesScanned++

		f, parseErr := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if parseErr != nil {
			return parseErr
		}
		self := filepath.Dir(rel) == paradigmDir

		ast.Inspect(f, func(n ast.Node) bool {
			switch node := n.(type) {
			case *ast.SelectorExpr:
				if pkg, ok := node.X.(*ast.Ident); ok && pkg.Name == "paradigm" && exported[node.Sel.Name] {
					refs = append(refs, gateReference{file: rel, symbol: "paradigm." + node.Sel.Name})
					return false
				}
			case *ast.Ident:
				if self && inPackage[node.Name] {
					refs = append(refs, gateReference{file: rel, symbol: node.Name})
				}
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
	return refs, filesScanned
}

// repoRoot resolves the module root from this package's directory and proves it
// is the root, so a layout change fails loudly instead of scanning nothing.
func repoRoot(t *testing.T) string {
	t.Helper()
	root := filepath.Join("..", "..", "..")
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("expected the module root at %s: %v", root, err)
	}
	return root
}

// TestSchemaGate_IsCalledByNoProductionCode is the structural inertness lock.
//
// It also PROVES ITS OWN SEARCH WORKS before concluding an absence: it first
// scans WITH schemagate.go included and requires the detector to find the gate
// symbols there. An absence reported by a scanner that finds nothing anywhere
// is not evidence — it is a broken scanner.
func TestSchemaGate_IsCalledByNoProductionCode(t *testing.T) {
	root := repoRoot(t)

	// POSITIVE PROBE: with nothing skipped, the gate's own file must light up.
	probe, probeFiles := scanGateReferences(t, root, nil)
	if probeFiles < 50 {
		t.Fatalf("scanned only %d non-test .go files; the walk is not reaching the repository", probeFiles)
	}
	inGateFile := 0
	for _, r := range probe {
		if filepath.Base(r.file) == gateFile {
			inGateFile++
		}
	}
	if inGateFile == 0 {
		t.Fatalf("detector found no gate symbols in %s across %d files — the scan is broken, "+
			"so any absence it reports is worthless", gateFile, probeFiles)
	}

	// THE LOCK: everywhere else, silence.
	refs, _ := scanGateReferences(t, root, map[string]bool{
		filepath.Join("internal", "core", "paradigm", gateFile): true,
	})
	if len(refs) == 0 {
		return
	}
	seen := make([]string, 0, len(refs))
	for _, r := range refs {
		seen = append(seen, r.file+": "+r.symbol)
	}
	sort.Strings(seen)
	t.Fatalf("the schema gate is STAGE 1 and must be wired to nothing, but %d production reference(s) exist:\n  %s\n"+
		"If this is stage 2 deliberately wiring the gate in, retire this test in the same change — "+
		"do not weaken it.", len(refs), strings.Join(seen, "\n  "))
}

// TestSchemaGate_DoesNotMoveDetect is the behavioral half, built on the EXACT
// shape that motivated the inversion: one table named dim_status with fan-in
// >= 1, sitting in an otherwise purely transactional schema. Bottom-up
// detection promotes it to a dimension and folds the whole schema to "mixed",
// which is what lets a single table decide its own 3NF silencing.
//
// The gate can see this schema. Detect still cannot. That gap IS stage 1, and
// this test fails the moment stage 1 stops being inert.
//
// The schema is deliberately built to fire TWO signals, and that detail is the
// difference between a lock and an ornament. An earlier version gave every
// table a created_at, which left only star_topology firing — so a stage-2
// wiring of the obvious form "two or more signals means warehouse" slipped
// through it green. Proven by mutation, not by inspection. Two signals is also
// what the real Sakila corpus measures, so the shape is not contrived.
func TestSchemaGate_DoesNotMoveDetect(t *testing.T) {
	s := &db.Schema{Tables: []db.Table{
		refs(provenTable("orders", "id", "customer_id", "status_id", "placed_on"), "customers", "dim_status"),
		refs(provenTable("order_items", "id", "order_id", "product_id", "quantity", "price"), "orders", "products"),
		provenTable("customers", "id", "name", "last_update"),
		provenTable("products", "id", "name", "last_update"),
		provenTable("dim_status", "id", "label", "last_update"),
	}}

	// The gate DOES see evidence here — enough of it that any plausible
	// stage-2 threshold would act on it.
	e := paradigm.WarehouseSignals(s)
	assertFired(t, e, paradigm.SignalNoAuditTimestamps, paradigm.SignalStarTopology)

	// Detection is unmoved: the single dim_ table still carries the whole
	// schema to "mixed", exactly as it did before the gate existed.
	cls := paradigm.Detect(s)
	if cls.Paradigm != paradigm.ParadigmMixed {
		t.Errorf("Detect().Paradigm = %q, want %q — stage 1 must not move detection",
			cls.Paradigm, paradigm.ParadigmMixed)
	}
	if got := cls.Roles["dim_status"]; got != paradigm.RoleDimension {
		t.Errorf("Roles[dim_status] = %q, want %q — stage 1 must not move role assignment",
			got, paradigm.RoleDimension)
	}
	for _, name := range []string{"orders", "order_items", "customers", "products"} {
		if got := cls.Roles[name]; got != paradigm.RoleUnclassified {
			t.Errorf("Roles[%s] = %q, want %q", name, got, paradigm.RoleUnclassified)
		}
	}
	if len(cls.Unprovable) != 0 {
		t.Errorf("Unprovable = %v, want empty", cls.Unprovable)
	}
}
