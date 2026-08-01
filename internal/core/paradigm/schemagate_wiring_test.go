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

// STAGE 2 IS WIRED, and this file is the lock that says so. It is the deliberate
// INVERSION of the stage-1 inertness locks that lived here
// (schemagate_inertness_test.go), and the three of them were retired one by one
// rather than deleted for green:
//
//  1. TestSchemaGate_IsCalledByNoProductionCode -> INVERTED into
//     TestSchemaGate_IsWiredIntoDetect. Same AST scanner, same positive probe,
//     opposite assertion: the gate must be referenced by the file that owns
//     Detect. Stage 1 said "if this is stage 2 deliberately wiring the gate in,
//     retire this test in the same change"; this is that change.
//  2. TestSchemaGate_DoesNotMoveDetect -> INVERTED into
//     TestSchemaGate_MovesDetect, on the byte-identical fixture. It asserted
//     that the motivating schema still folded to "mixed" with dim_status a
//     dimension. It now asserts the opposite, which is the entire slice.
//  3. TestSchemaGate_EveryGateSymbolIsEnumerated -> RETIRED, with no
//     replacement of that shape, because its subject no longer exists. It kept
//     a hand-written inventory of every gate symbol COMPLETE, so that a
//     seventh signal could not be added and left outside a scan asserting
//     "nothing anywhere references any of these". Once the gate is deliberately
//     wired, there is nothing to enumerate: the question changed from "is any
//     symbol referenced anywhere" (which needs a complete list) to "is the
//     verdict consulted where it must be" (which needs one call site).
//     Its real property — a new signal cannot slip into or out of the verdict
//     unnoticed — is now held by
//     TestSchemaGate_TheDecidingSplitIsExactlyAsMeasured below, which exercises
//     both sides of the split through schemas that actually fire each signal.

const gateFile = "schemagate.go"

// gateReference is one place a gate symbol is used.
type gateReference struct {
	file   string
	symbol string
}

// gateEntryPoints are the gate's PUBLIC entry points — the symbols a caller
// outside schemagate.go uses to obtain and read the verdict. This list is
// deliberately SHORT where the retired inertness inventory was exhaustive: an
// "is it wired" question is answered by the call site, not by the internals.
var gateEntryPoints = []string{"WarehouseSignals", "WarehouseEvidence"}

// scanGateReferences parses every non-test .go file under root and reports every
// reference to a gate entry point, skipping the files named in skip.
//
// The scan is AST-based, not textual: a comment or a doc string naming
// WarehouseSignals is prose, not a call, and a textual grep could not tell them
// apart. Outside internal/core/paradigm only the QUALIFIED form
// (paradigm.WarehouseSignals) counts, so an unrelated local identifier in
// another package can never raise a false alarm.
func scanGateReferences(t *testing.T, root string, skip map[string]bool) (refs []gateReference, filesScanned int) {
	t.Helper()

	wanted := map[string]bool{}
	for _, s := range gateEntryPoints {
		wanted[s] = true
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
				if pkg, ok := node.X.(*ast.Ident); ok && pkg.Name == "paradigm" && wanted[node.Sel.Name] {
					refs = append(refs, gateReference{file: rel, symbol: "paradigm." + node.Sel.Name})
					return false
				}
			case *ast.Ident:
				if self && wanted[node.Name] {
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

// detectFile is the file that must consult the gate: Detect and Resolve live in
// it, and they are the only two places a verdict may be produced or overridden.
const detectFile = "paradigm.go"

// TestSchemaGate_IsWiredIntoDetect is the structural half, inverted from the
// stage-1 inertness lock.
//
// It keeps that lock's discipline of PROVING ITS OWN SEARCH WORKS before
// concluding anything: it first scans with schemagate.go included and requires
// the detector to light up there. A scanner that finds nothing anywhere proves
// nothing in either direction — and an "is it wired" assertion is exactly the
// shape a broken walk would satisfy by accident if the probe were skipped.
func TestSchemaGate_IsWiredIntoDetect(t *testing.T) {
	root := repoRoot(t)

	// POSITIVE PROBE: the gate's own file must light up.
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
		t.Fatalf("detector found no gate entry points in %s across %d files — the scan is broken, "+
			"so any conclusion it supports is worthless", gateFile, probeFiles)
	}

	// THE LOCK: with the gate's own file skipped, paradigm.go must still be
	// there. Unwire the verdict and this fails.
	refs, _ := scanGateReferences(t, root, map[string]bool{
		filepath.Join("internal", "core", "paradigm", gateFile): true,
	})
	var seen []string
	wiredIntoDetect := false
	wantFile := filepath.Join("internal", "core", "paradigm", detectFile)
	for _, r := range refs {
		seen = append(seen, r.file+": "+r.symbol)
		if r.file == wantFile {
			wiredIntoDetect = true
		}
	}
	sort.Strings(seen)
	if !wiredIntoDetect {
		t.Fatalf("%s does not reference the schema gate, so Detect cannot be consulting the verdict.\n"+
			"References found elsewhere:\n  %s", wantFile, strings.Join(seen, "\n  "))
	}
}

// TestSchemaGate_MovesDetect is the behavioral half, and it is the stage-1
// lock's fixture UNCHANGED — one table named dim_status with fan-in >= 1,
// sitting in an otherwise purely transactional schema, spelling its audit stamp
// last_update and carrying a depth-1 join table. Two signals fire on it, and
// real Sakila measures exactly the same two.
//
// Stage 1 asserted that Detect did NOT move over this schema: dim_status
// promoted itself to a dimension and folded the whole thing to "mixed", which is
// what let a single table decide its own 3NF silencing. Stage 2 asserts the
// opposite, on the same bytes.
//
// The two signals it fires are BOTH excluded from the verdict, which makes this
// fixture do double duty: it proves the gate stays closed even when two of six
// signals fire, so a future change that reverted to counting would fail here.
func TestSchemaGate_MovesDetect(t *testing.T) {
	s := &db.Schema{Tables: []db.Table{
		refs(provenTable("orders", "id", "customer_id", "status_id", "placed_on"), "customers", "dim_status"),
		refs(provenTable("order_items", "id", "order_id", "product_id", "quantity", "price"), "orders", "products"),
		provenTable("customers", "id", "name", "last_update"),
		provenTable("products", "id", "name", "last_update"),
		provenTable("dim_status", "id", "label", "last_update"),
	}}

	// The gate sees two signals here, and still refuses: neither votes.
	e := paradigm.WarehouseSignals(s)
	assertFired(t, e, paradigm.SignalNoAuditTimestamps, paradigm.SignalStarTopology)
	if e.Qualifies() {
		t.Fatal("Qualifies() = true — two EXCLUDED signals must not add up to a warehouse")
	}

	cls := paradigm.Detect(s)
	if cls.Paradigm != paradigm.ParadigmOLTP {
		t.Errorf("Detect().Paradigm = %q, want %q — the schema now votes, and it votes no",
			cls.Paradigm, paradigm.ParadigmOLTP)
	}
	if got := cls.Roles["dim_status"]; got != paradigm.RoleUnclassified {
		t.Errorf("Roles[dim_status] = %q, want %q — one dim_-named table no longer decides its own "+
			"1NF silencing inside a transactional schema", got, paradigm.RoleUnclassified)
	}
	if got := cls.Gate.Withheld["dim_status"]; got != paradigm.RoleDimension {
		t.Errorf("Gate.Withheld[dim_status] = %q, want %q", got, paradigm.RoleDimension)
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

// TestSchemaGate_TheDecidingSplitIsExactlyAsMeasured is the replacement for the
// retired symbol-enumeration lock, and it holds the same property by a different
// route: a signal cannot join or leave the verdict quietly.
//
// The enumeration lock guarded a list of things that must be referenced NOWHERE.
// This one guards the list that DECIDES, from both sides — every deciding signal
// must appear in Deciding() wherever it fires, and every excluded signal must
// never appear there even when it does fire. Each case runs over a schema that
// genuinely fires the signal, asserted before the case is evaluated, so this is
// not a restatement of the source constant.
func TestSchemaGate_TheDecidingSplitIsExactlyAsMeasured(t *testing.T) {
	// Every signal the gate can report, in its fixed order.
	all := []paradigm.Signal{
		paradigm.SignalCalendarTable,
		paradigm.SignalSurrogateKeyNames,
		paradigm.SignalBulkLoadShape,
		paradigm.SignalNoAuditTimestamps,
		paradigm.SignalStarTopology,
		paradigm.SignalTypeProfileSplit,
	}
	deciding := map[paradigm.Signal]bool{
		paradigm.SignalCalendarTable:     true, // measured 8 warehouse / 0 transactional
		paradigm.SignalSurrogateKeyNames: true, // measured 3 / 0
		paradigm.SignalTypeProfileSplit:  true, // measured 3 / 0
	}
	// A schema that FIRES each signal. bulk_load_shape and no_audit_timestamps
	// share one because a schema with no foreign keys and no created_at fires
	// both; star_topology needs foreign keys and therefore cannot.
	firing := map[paradigm.Signal]*db.Schema{
		paradigm.SignalCalendarTable:     calendarOnlySchema(),
		paradigm.SignalSurrogateKeyNames: surrogateOnlySchema(),
		paradigm.SignalTypeProfileSplit:  warehouseSplit(),
		paradigm.SignalBulkLoadShape:     bulkLoadOnlySchema(),
		paradigm.SignalNoAuditTimestamps: bulkLoadOnlySchema(),
		paradigm.SignalStarTopology:      excludedOnlySchema(),
	}

	for _, sig := range all {
		s, ok := firing[sig]
		if !ok {
			t.Fatalf("signal %q has no fixture that fires it — a new signal reached the gate without "+
				"being classified as deciding or excluded here", sig)
		}
		e := paradigm.WarehouseSignals(s)
		if !e.Has(sig) {
			t.Fatalf("the fixture for %q does not fire it (Fired = %v); the case below would be vacuous", sig, e.Fired)
		}

		inDeciding := false
		for _, d := range e.Deciding() {
			if d == sig {
				inDeciding = true
			}
		}
		if deciding[sig] && !inDeciding {
			t.Errorf("%q fired but is absent from Deciding() = %v — a deciding signal stopped deciding", sig, e.Deciding())
		}
		if !deciding[sig] && inDeciding {
			t.Errorf("%q is an EXCLUDED signal but appears in Deciding() = %v — it measured ~50/50 over "+
				"26 corpora (or fired on nothing at all) and must never open the gate", sig, e.Deciding())
		}
	}
}
