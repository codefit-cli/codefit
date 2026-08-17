package mcp_test

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/codefit-cli/codefit/internal/core/findings"
	"github.com/codefit-cli/codefit/internal/core/scoring"
	"github.com/codefit-cli/codefit/internal/mcp"
)

// This file locks I4's third manifestation (docs/specs/audit-protocol.md,
// spec #1672 / design #1673): the first two manifestations already have
// controls — TestScanAllBudget_HonestyPersistsWhenTheBudgetForcesWithholding
// (budget withholding) and TestSummary_UnmeasuredDimensionIsNullNeverZero
// (null-vs-zero, for the two dimensions Summary already hand-lists) — but
// nothing compared the MEASURED-dimension set to which summary sub-blocks
// are non-nil. That is the gap docs/roadmap.md's P0-8 entry names as "Named,
// not built."
//
// Two halves lock the same invariant from two directions:
//
//   - V (TestSummary_CoversEveryDimensionTheResponseMeasured) drives the REAL
//     HandleScanAll over REAL fixtures and checks the WIRE: every dimension
//     resp.Score.ByDimension reports as measured has a non-null
//     resp.Summary sub-block under the same wire name.
//   - W (TestSummary_HasASlotForEveryDimensionTheWiringCanMeasure) never runs
//     a sensor: it parses internal/core/findings and internal/mcp with
//     go/ast for the `measured = append(measured, findings.X)` wiring sites,
//     and requires each one to have a matching JSON tag on ScanAllSummary.
//
// V alone has a blind spot (design D1, option A): it is only as general as
// the fixtures exercise it, so a future third dimension proves nothing about
// V until someone writes a fixture for it. W closes that blind spot
// statically — it goes red the moment a developer writes the append site,
// independent of any fixture, before any sensor for that dimension exists.
//
// TDD note (this is the whole point of this file's doc comment): there is NO
// natural red here. Both halves are expected GREEN on unmodified main — the
// invariant already holds today, it is simply unguarded. The required red
// comes from the two mutations in the design's mutation plan (M1, M2),
// applied and reverted by hand during `sdd-apply`, never from this file.

// --- V — the value half: drives the real handler, checks the wire --------------

// TestSummary_CoversEveryDimensionTheResponseMeasured is table-driven over
// three cases so the walk is proven not to be secretly anchored to either
// dimension: both measured, security-only, and db-only-with-no-security-
// provider. Every case walks all 6 findings.Dimension keys; review,
// complexity, practices and tests always land in the absent-tolerated
// branch below — the visible trace of the deferred six-slot-symmetry
// non-goal (spec #1672's Non-Goals), and the reason this control must not
// demand PRESENCE for a dimension nobody measured.
func TestSummary_CoversEveryDimensionTheResponseMeasured(t *testing.T) {
	cases := []struct {
		name     string
		files    map[string]string
		language string
		// wantSecurityMeasured/wantDBMeasured are FIXTURE PRECONDITIONS — a
		// vacuity guard that this case exercises what its name promises — not
		// the control's expectation. The expectation below is derived from
		// resp.Score.ByDimension, never from this table.
		wantSecurityMeasured bool
		wantDBMeasured       bool
	}{
		{
			name: "both measured",
			files: map[string]string{
				".codefit.yaml":            summaryDimensionYAMLUnreadableSchema,
				"prisma/schema.prisma":     summaryDimensionPrismaSchema,
				"app/orders/[id]/route.ts": summaryDimensionEndpoint,
			},
			language:             "typescript",
			wantSecurityMeasured: true,
			wantDBMeasured:       true,
		},
		{
			name: "security only",
			files: map[string]string{
				".codefit.yaml":  tsYAMLNoDB,
				"app/x/route.ts": summaryDimensionEndpoint,
			},
			language:             "typescript",
			wantSecurityMeasured: true,
			wantDBMeasured:       false,
		},
		{
			// Proves the walk is not secretly security-anchored: security
			// never resolves for python, db resolves from the SQL-DDL schema.
			name: "db only, no security provider",
			files: map[string]string{
				".codefit.yaml": summaryDimensionYAMLUnresolvedLang,
				"db/schema.sql": summaryDimensionSQLSchema,
			},
			language:             "python",
			wantSecurityMeasured: false,
			wantDBMeasured:       true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := writeProj(t, tc.files)
			resp, err := mcp.HandleScanAll(mcp.ScanAllRequest{Root: root, Language: tc.language})
			if err != nil {
				t.Fatalf("HandleScanAll: %v", err)
			}

			secScore := resp.Score.ByDimension[findings.DimensionSecurity]
			dbScore := resp.Score.ByDimension[findings.DimensionDB]
			if tc.wantSecurityMeasured != (secScore != nil) {
				t.Fatalf("fixture precondition failed: wanted security measured=%v, by_dimension.security=%v — "+
					"this case does not exercise what its name promises", tc.wantSecurityMeasured, secScore)
			}
			if tc.wantDBMeasured != (dbScore != nil) {
				t.Fatalf("fixture precondition failed: wanted db measured=%v, by_dimension.db=%v — "+
					"this case does not exercise what its name promises", tc.wantDBMeasured, dbScore)
			}

			// The WIRE, not the Go struct — a json:"-,omitempty" pointer field
			// distinguishes ABSENT from an explicit `null`, which reflect over
			// Go fields cannot see (design D3).
			raw, err := json.Marshal(resp)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			var envelope struct {
				Summary map[string]json.RawMessage `json:"summary"`
			}
			if err := json.Unmarshal(raw, &envelope); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}

			// The INDEPENDENT input: resp.Score.ByDimension's non-nil keys, the
			// same set scoring.Compute already treats as ground truth for
			// "measured" (internal/core/scoring/scoring.go). Never derived from
			// resp.Summary's own fields — a control that walked the subject to
			// build the subject's own expectation would assert nothing (archive
			// #1664, "a control that counted its own subject"). totals/note need
			// no exclusion list: this loop only ever looks up a dimension key,
			// so a non-dimension field of Summary is never visited.
			for dim, score := range resp.Score.ByDimension {
				v, present := envelope.Summary[string(dim)]
				if score != nil {
					// measured: the sub-block must be present AND non-null.
					if !present {
						t.Errorf("dimension %q: by_dimension.%s=%d (measured) but summary.%s is ABSENT from "+
							"the wire — extend ScanAllSummary and summarize() to carry a %s sub-block",
							dim, dim, *score, dim, dim)
						continue
					}
					if strings.TrimSpace(string(v)) == "null" {
						t.Errorf("dimension %q: by_dimension.%s=%d (measured) but summary.%s is NULL — "+
							"extend summarize() to populate the %s sub-block on this path",
							dim, dim, *score, dim, dim)
					}
				} else {
					// not measured: absent or an explicit null are both honest
					// (TestSummary_UnmeasuredDimensionIsNullNeverZero already
					// locks which of the two summarize() must produce for the
					// dimensions it knows about). This control only forbids a
					// POPULATED block over a dimension nobody measured — the
					// I2 defect ("a zero that means nobody looked") one layer up.
					if present && strings.TrimSpace(string(v)) != "null" {
						t.Errorf("dimension %q: by_dimension.%s=nil (not measured) but summary.%s=%s — a "+
							"populated block over a dimension nobody measured reads as a false all-clear",
							dim, dim, dim, v)
					}
				}
			}
		})
	}
}

// --- W — the wiring half: static, no sensor, no fixture -------------------------

// dimensionConsts parses every non-test source file under
// internal/core/findings with go/ast and returns ident -> wire value for
// every Dimension-typed const. Fail-closed (CLAUDE.md, "la ausencia necesita
// sonda positiva" / vocabulary_test.go's "the probe itself is broken"
// precedent): empty, or missing a scoring.DefaultWeights() key, means the
// PROBE went blind, not that the vocabulary actually shrank.
func dimensionConsts(t *testing.T) map[string]string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller(0) failed — cannot locate this package's directory")
	}
	dir := filepath.Join(filepath.Dir(thisFile), "..", "core", "findings")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("reading %s: %v", dir, err)
	}

	out := map[string]string{}
	fset := token.NewFileSet()
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		f, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			t.Fatalf("go/ast parsing %s: %v", path, err)
		}
		for _, decl := range f.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok || gen.Tok != token.CONST {
				continue
			}
			for _, spec := range gen.Specs {
				vs, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				ident, ok := vs.Type.(*ast.Ident)
				if !ok || ident.Name != "Dimension" {
					continue // not a Dimension-typed const
				}
				for i, v := range vs.Values {
					lit, ok := v.(*ast.BasicLit)
					if !ok || lit.Kind != token.STRING {
						continue
					}
					val, err := strconv.Unquote(lit.Value)
					if err != nil {
						t.Fatalf("unquoting const literal %s: %v", lit.Value, err)
					}
					out[vs.Names[i].Name] = val
				}
			}
		}
	}

	if len(out) == 0 {
		t.Fatal("no Dimension-typed const found under internal/core/findings — the probe went blind; " +
			"re-anchor it before trusting a green")
	}
	values := make(map[string]bool, len(out))
	for _, v := range out {
		values[v] = true
	}
	for dim := range scoring.DefaultWeights() {
		if !values[string(dim)] {
			t.Fatalf("scoring.DefaultWeights() has key %q the parsed Dimension const block never produced — "+
				"the probe went blind; re-anchor it before trusting a green", dim)
		}
	}
	return out
}

// measuredDimensionIdents ast.Inspects this package's OWN non-test source
// files (runtime.Caller(0) locates internal/mcp) for
// `append(measured, findings.X)` call sites and returns the distinct
// findings.X selector names found, in first-seen order.
//
// Floor (design D1's declared limit, fail-closed): fewer than 2 distinct
// sites means this append-shaped probe stopped seeing the wiring — either it
// was refactored to a slice literal / a loop over registered sensors, or
// something broke the parse — never that codefit started measuring fewer
// than two dimensions. t.Fatal, not t.Error: a blind probe must never report
// a silent green.
func measuredDimensionIdents(t *testing.T) []string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller(0) failed — cannot locate this package's directory")
	}
	dir := filepath.Dir(thisFile)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("reading %s: %v", dir, err)
	}

	seen := map[string]bool{}
	var idents []string
	fset := token.NewFileSet()
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		f, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			t.Fatalf("go/ast parsing %s: %v", path, err)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			fun, ok := call.Fun.(*ast.Ident)
			if !ok || fun.Name != "append" || len(call.Args) < 2 {
				return true
			}
			first, ok := call.Args[0].(*ast.Ident)
			if !ok || first.Name != "measured" {
				return true
			}
			for _, arg := range call.Args[1:] {
				sel, ok := arg.(*ast.SelectorExpr)
				if !ok {
					continue
				}
				pkg, ok := sel.X.(*ast.Ident)
				if !ok || pkg.Name != "findings" {
					continue
				}
				if !seen[sel.Sel.Name] {
					seen[sel.Sel.Name] = true
					idents = append(idents, sel.Sel.Name)
				}
			}
			return true
		})
	}

	if len(idents) < 2 {
		t.Fatalf("found only %d distinct append(measured, findings.X) site(s) under %s (%v) — the probe went "+
			"blind; re-anchor it before trusting a green", len(idents), dir, idents)
	}
	return idents
}

// TestSummary_HasASlotForEveryDimensionTheWiringCanMeasure is W: no sensor,
// no fixture, no runtime. It requires every dimension the real production
// code appends to `measured` to have a matching JSON tag on ScanAllSummary —
// the Phase-3 trap (a developer adding a third dimension to `measured`
// without extending ScanAllSummary/summarize() to carry it) fails HERE, at
// the moment the append site is written, independent of whether any test
// fixture would ever exercise the new dimension's sensor.
func TestSummary_HasASlotForEveryDimensionTheWiringCanMeasure(t *testing.T) {
	consts := dimensionConsts(t)
	idents := measuredDimensionIdents(t)
	tags := jsonTagsOf(t, reflect.TypeOf(mcp.ScanAllSummary{})) // reused from summary_dimension_test.go

	tagSet := make(map[string]bool, len(tags))
	for _, tag := range tags {
		tagSet[tag] = true
	}

	for _, ident := range idents {
		value, ok := consts[ident]
		if !ok {
			t.Fatalf("append(measured, findings.%s) references an ident dimensionConsts never parsed — "+
				"re-anchor dimensionConsts against internal/core/findings", ident)
		}
		if !tagSet[value] {
			t.Errorf("findings.%s (wire value %q) is appended to measured in internal/mcp, but ScanAllSummary "+
				"has no %q json tag — extend ScanAllSummary and summarize() to carry a matching sub-block "+
				"(current tags: %v)", ident, value, value, tags)
		}
	}
}
