package mcp_test

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/codefit-cli/codefit/internal/mcp"
)

// summaryDimensionYAMLUnreadableSchema configures the DB dimension over a
// schema path that does not exist on disk. The dimension is CONFIGURED (so it
// is attempted) and cannot MEASURE (so it abstains) — the second of the two
// ways summary.db must be null.
const summaryDimensionYAMLUnreadableSchema = `version: "1"
project:
  name: t
  language: typescript
  framework: next
database:
  type: postgresql
  schema_paths:
    - prisma/schema.prisma
`

// summaryDimensionYAMLUnresolvedLang is a language with no registered security
// provider plus a real schema: security cannot be measured, db can.
const summaryDimensionYAMLUnresolvedLang = `version: "1"
project:
  name: t
  language: python
database:
  type: postgresql
  schema_paths:
    - db/schema.sql
`

const summaryDimensionSQLSchema = `CREATE TABLE no_key (
  name TEXT NOT NULL
);
`

const summaryDimensionPrismaSchema = `datasource db {
  provider = "postgresql"
  url      = env("DATABASE_URL")
}

model NoKey {
  name String
}
`

// summaryDimensionEndpoint is one route handler with an IDOR shape and no
// authorization: it guarantees the security dimension has surface to count, so
// a security count of zero in these tests would be a real signal and not an
// empty fixture.
const summaryDimensionEndpoint = `export async function GET(req: Request, { params }: { params: { id: string } }) {
  return Response.json(await prisma.order.findUnique({ where: { id: params.id } }));
}
`

// --- Q5: an unmeasured dimension is null, never zero ---------------------------

// TestSummary_UnmeasuredDimensionIsNullNeverZero covers BOTH ways a dimension
// goes unmeasured, because they are different code paths that must produce the
// same honest answer:
//
//   - the dimension never ran (no security provider for the language; no
//     database configured), and
//   - the dimension ran and could not measure (a configured schema that cannot
//     be read, i.e. DBSection.Measured == false).
//
// A zero would be the exact defect this project exists to prevent: a count of 0
// that means "nobody looked" is indistinguishable, to an agent skimming the
// response, from a project that was audited and found clean.
//
// Mutation that MUST turn this red: emit a zeroed sub-block instead of nil.
func TestSummary_UnmeasuredDimensionIsNullNeverZero(t *testing.T) {
	t.Run("no security provider for the language", func(t *testing.T) {
		root := writeProj(t, map[string]string{
			".codefit.yaml": summaryDimensionYAMLUnresolvedLang,
			"db/schema.sql": summaryDimensionSQLSchema,
		})
		resp, err := mcp.HandleScanAll(mcp.ScanAllRequest{Root: root, Language: "python"})
		if err != nil {
			t.Fatalf("HandleScanAll: %v", err)
		}
		if resp.Security.Measured {
			t.Fatal("the fixture is meant to have NO resolvable security provider — it resolved one")
		}
		if resp.Summary.Security != nil {
			t.Errorf("summary.security must be NULL when no provider resolved, got %+v — a zeroed block "+
				"reads as 'codefit looked at the code and found nothing'", resp.Summary.Security)
		}
		if resp.Summary.DB == nil {
			t.Fatal("summary.db must be populated: the db dimension ran independently of the language")
		}
		// The key must be PRESENT with a null value, not absent: an absent key is
		// a third state a reader has to guess at, and score.by_dimension already
		// set the precedent of serialising an explicit null.
		assertSummaryKeyIsExplicitNull(t, resp, "security")
	})

	t.Run("db configured but not measurable", func(t *testing.T) {
		root := writeProj(t, map[string]string{
			".codefit.yaml": summaryDimensionYAMLUnreadableSchema,
			// prisma/schema.prisma is deliberately NOT written.
			"app/orders/[id]/route.ts": summaryDimensionEndpoint,
		})
		resp, err := mcp.HandleScanAll(mcp.ScanAllRequest{Root: root, Language: "typescript"})
		if err != nil {
			t.Fatalf("HandleScanAll: %v", err)
		}
		if resp.DB == nil {
			t.Fatal("the db dimension is configured, so a section reporting the abstention must exist")
		}
		if resp.DB.Measured {
			t.Fatal("the fixture's configured schema does not exist on disk — the dimension must report " +
				"measured=false; this test is reading a project it does not recognise")
		}
		if resp.Summary.DB != nil {
			t.Errorf("summary.db must be NULL when the section exists with measured=false, got %+v — a "+
				"zeroed block asserts a clean schema over one codefit could not read", resp.Summary.DB)
		}
		if resp.Summary.Security == nil {
			t.Fatal("security resolved for typescript — summary.security must not be null")
		}
		assertSummaryKeyIsExplicitNull(t, resp, "db")
		// And the totals must not silently absorb the abstention as a zero: with
		// only security measured, totals equals security.
		if resp.Summary.Totals.SurfaceItems != resp.Summary.Security.SurfaceItems {
			t.Errorf("totals.surface_items = %d, want %d (only security was measured)",
				resp.Summary.Totals.SurfaceItems, resp.Summary.Security.SurfaceItems)
		}
	})
}

// assertSummaryKeyIsExplicitNull checks the WIRE, not the Go struct: the
// sub-block key must be present with a JSON null. `omitempty` on a nil pointer
// would drop the key entirely, and "absent" is not the same statement as
// "measured nothing" — score.by_dimension already ships `"db": null` beside
// `"db": 95`, and summary must read the same way.
func assertSummaryKeyIsExplicitNull(t *testing.T, resp mcp.ScanAllResponse, key string) {
	t.Helper()
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
	v, present := envelope.Summary[key]
	if !present {
		t.Errorf("summary.%s is ABSENT from the serialized response. An absent key is a third state the "+
			"reader has to guess at; it must be present with an explicit null (no omitempty)", key)
		return
	}
	if strings.TrimSpace(string(v)) != "null" {
		t.Errorf("summary.%s should serialize as null, got %s", key, v)
	}
}

// --- Q1: the summary counts the RAW population ---------------------------------

// TestSummary_CountsTheRawPopulationNotTheBaselineFilteredList locks the
// population trap. DBSection.Findings/Surface are baseline-FILTERED in place
// after the sensor runs; the summary must count the raw sensor result, the same
// population Score is computed over. Otherwise a project whose items are all
// already tracked reports a summary of zero on the second scan — "nobody
// looked" again, this time produced by the baseline.
//
// The two runs share one baseline path (HandleScanAll writes it into the
// project root), so the second run sees everything as known.
//
// Mutation that MUST turn this red: swap dbRes for the post-filter dbSection.
func TestSummary_CountsTheRawPopulationNotTheBaselineFilteredList(t *testing.T) {
	root := writeProj(t, map[string]string{
		".codefit.yaml":            summaryDimensionYAMLUnreadableSchema,
		"prisma/schema.prisma":     summaryDimensionPrismaSchema,
		"app/orders/[id]/route.ts": summaryDimensionEndpoint,
	})

	first, err := mcp.HandleScanAll(mcp.ScanAllRequest{Root: root, Language: "typescript"})
	if err != nil {
		t.Fatalf("first scan: %v", err)
	}
	if first.Summary.DB == nil || first.Summary.DB.SurfaceItems == 0 {
		t.Fatalf("the fixture must produce at least one db surface item on a first scan, got %+v — this "+
			"test would otherwise compare zero to zero", first.Summary.DB)
	}
	if len(first.DB.Surface) == 0 {
		t.Fatal("the first scan's db.surface must be non-empty, or the filtering below proves nothing")
	}

	second, err := mcp.HandleScanAll(mcp.ScanAllRequest{Root: root, Language: "typescript"})
	if err != nil {
		t.Fatalf("second scan: %v", err)
	}
	// The rendered list IS filtered — that is the baseline doing its job.
	if len(second.DB.Surface) != 0 {
		t.Fatalf("the second scan's db.surface should be empty (every item is now tracked), got %d item(s) "+
			"— the baseline is not silencing what it already knows, so this test is not exercising the trap",
			len(second.DB.Surface))
	}
	// The COUNT is not.
	if second.Summary.DB == nil {
		t.Fatal("summary.db went null on a re-scan — the dimension still ran and still measured")
	}
	if second.Summary.DB.SurfaceItems != first.Summary.DB.SurfaceItems {
		t.Errorf("summary.db.surface_items collapsed from %d to %d on a fully-baselined re-scan — it is "+
			"being taken from the baseline-FILTERED db section instead of the raw sensor result",
			first.Summary.DB.SurfaceItems, second.Summary.DB.SurfaceItems)
	}
	// The response must be able to EXPLAIN that difference without the reader
	// having to open Go source.
	if second.Summary.Note == "" {
		t.Fatal("summary.note is empty — a non-zero count over an empty db.surface is unexplainable without it")
	}
	for _, want := range []string{"baseline", "RAW"} {
		if !strings.Contains(second.Summary.Note, want) {
			t.Errorf("summary.note must state that the counts precede the baseline filter (looking for %q), got %q",
				want, second.Summary.Note)
		}
	}
}

// --- Q3/Q4: incommensurable units are not summed --------------------------------

// TestSummaryTotals_CarriesOnlyCommensurableUnits reads the JSON tags off the
// types with reflect rather than asserting against a hand-written list of what
// the response "should" contain — the same approach as
// core/surface/vocabulary_test.go, and for the same reason: a hand-kept second
// list is one more place to drift.
//
// endpoints and schema_sources are each dimension's own SCALE UNIT. Summing them
// would produce a number with no meaning (a table has no route). certain_concerns
// is absent from totals for a sharper reason: the security field of that name
// counts Deterministic PLUS SurfaceConfirmed concerns, so it is not a
// certainty-1.0 count, and a same-named DB sibling would be the exact
// same-name-different-definition defect this whole change fixes.
//
// Mutation that MUST turn this red: add Endpoints (or SchemaSources, or
// CertainConcerns) to SummaryTotals.
func TestSummaryTotals_CarriesOnlyCommensurableUnits(t *testing.T) {
	got := jsonTagsOf(t, reflect.TypeOf(mcp.SummaryTotals{}))
	want := []string{"deterministic_findings", "surface_items"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("summary.totals carries %v, want exactly %v", got, want)
	}
	for _, forbidden := range []string{"endpoints", "schema_sources", "certain_concerns"} {
		if containsTag(got, forbidden) {
			t.Errorf("summary.totals must not carry %q: it is not commensurable across dimensions", forbidden)
		}
	}

	// The same asymmetry, stated on the DB sub-block: it carries its own scale
	// unit and deliberately NO certain_concerns.
	dbTags := jsonTagsOf(t, reflect.TypeOf(mcp.DBSummary{}))
	if !containsTag(dbTags, "schema_sources") {
		t.Errorf("summary.db must carry schema_sources, its own scale unit; got %v", dbTags)
	}
	if containsTag(dbTags, "certain_concerns") {
		t.Error("summary.db must NOT carry certain_concerns: the security field of that name counts " +
			"Deterministic + SurfaceConfirmed, so a DB sibling would be a same-name-different-definition " +
			"count — the defect this shape exists to remove (see ADR 0069)")
	}
	if containsTag(dbTags, "endpoints") {
		t.Errorf("summary.db must not carry endpoints; got %v", dbTags)
	}

	// And endpoints stays where it belongs.
	secTags := jsonTagsOf(t, reflect.TypeOf(mcp.SecuritySummary{}))
	if !containsTag(secTags, "endpoints") {
		t.Errorf("summary.security must carry endpoints; got %v", secTags)
	}
}

// jsonTagsOf returns the json tag names of every exported field of a struct
// type, in declaration order.
func jsonTagsOf(t *testing.T, typ reflect.Type) []string {
	t.Helper()
	if typ.Kind() != reflect.Struct {
		t.Fatalf("jsonTagsOf wants a struct, got %s", typ.Kind())
	}
	var out []string
	for i := 0; i < typ.NumField(); i++ {
		tag := typ.Field(i).Tag.Get("json")
		name, _, _ := strings.Cut(tag, ",")
		if name == "" || name == "-" {
			t.Fatalf("%s.%s has no json tag — the wire name would be derived from the Go name",
				typ.Name(), typ.Field(i).Name)
		}
		out = append(out, name)
	}
	if len(out) == 0 {
		t.Fatalf("%s has no exported fields — the probe itself is broken", typ.Name())
	}
	return out
}

func containsTag(tags []string, want string) bool {
	for _, tag := range tags {
		if tag == want {
			return true
		}
	}
	return false
}

// --- D4: totals is derived, never written ---------------------------------------

// TestSummaryTotals_IsDerivedFromTheSubBlocks recomputes the roll-up from the
// sub-blocks the response itself published and requires an exact match, over a
// project where BOTH dimensions contribute. A hand-written totals literal is
// right exactly once and then drifts; this is what catches the drift.
//
// Mutation that MUST turn this red: hand-write a totals literal.
func TestSummaryTotals_IsDerivedFromTheSubBlocks(t *testing.T) {
	root := writeProj(t, map[string]string{
		".codefit.yaml":            summaryDimensionYAMLUnreadableSchema,
		"prisma/schema.prisma":     summaryDimensionPrismaSchema,
		"app/orders/[id]/route.ts": summaryDimensionEndpoint,
	})
	resp, err := mcp.HandleScanAll(mcp.ScanAllRequest{Root: root, Language: "typescript"})
	if err != nil {
		t.Fatalf("HandleScanAll: %v", err)
	}
	if resp.Summary.Security == nil || resp.Summary.DB == nil {
		t.Fatalf("this test needs BOTH dimensions measured, got security=%+v db=%+v — a single-dimension "+
			"project cannot tell a derived total from a copied one",
			resp.Summary.Security, resp.Summary.DB)
	}
	if resp.Summary.DB.SurfaceItems == 0 && resp.Summary.DB.DeterministicFindings == 0 {
		t.Fatal("the db dimension contributed nothing — totals would equal security either way, and this " +
			"test would pass over a copied literal")
	}

	wantDet := resp.Summary.Security.DeterministicFindings + resp.Summary.DB.DeterministicFindings
	wantSurf := resp.Summary.Security.SurfaceItems + resp.Summary.DB.SurfaceItems
	if resp.Summary.Totals.DeterministicFindings != wantDet {
		t.Errorf("totals.deterministic_findings = %d, want %d (security %d + db %d)",
			resp.Summary.Totals.DeterministicFindings, wantDet,
			resp.Summary.Security.DeterministicFindings, resp.Summary.DB.DeterministicFindings)
	}
	if resp.Summary.Totals.SurfaceItems != wantSurf {
		t.Errorf("totals.surface_items = %d, want %d (security %d + db %d)",
			resp.Summary.Totals.SurfaceItems, wantSurf,
			resp.Summary.Security.SurfaceItems, resp.Summary.DB.SurfaceItems)
	}
	// Not equal to either dimension alone: a totals that silently copied one
	// sub-block would satisfy a weaker assertion than the sums above only by
	// coincidence, and this makes the coincidence a failure.
	if resp.Summary.Totals.SurfaceItems == resp.Summary.Security.SurfaceItems ||
		resp.Summary.Totals.SurfaceItems == resp.Summary.DB.SurfaceItems {
		t.Errorf("totals.surface_items (%d) equals a single sub-block (security %d, db %d) over a project "+
			"where both contribute — it is copied, not derived", resp.Summary.Totals.SurfaceItems,
			resp.Summary.Security.SurfaceItems, resp.Summary.DB.SurfaceItems)
	}
}
