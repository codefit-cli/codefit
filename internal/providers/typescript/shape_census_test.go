package typescript_test

import (
	"strings"
	"testing"

	"github.com/codefit-cli/codefit/internal/providers"
	"github.com/codefit-cli/codefit/internal/providers/typescript"
)

// THE SHAPE CENSUS — the missing half of the mutation discipline.
//
// Mutation answers "what would have to break for this test to fail?". It proves
// a control catches its target. NOTHING proved the target set was COMPLETE, and
// that is the gap this table closes.
//
// A detector's own test verifies it against its own definition: give the rule
// that declares `const $NAME = $VALUE` a `const`, and it passes by tautology. It
// cannot discover what is missing, because it only ever looks inside the cut.
//
// So this table enumerates SHAPES — real ways the thing being detected is
// written — and pins the verdict of each, INCLUDING the silences. A silence is
// admissible only when its reason is written down. It is the same instrument
// namematch/crossprovider_test.go already applies to the NAME axis; this is that
// instrument applied to the SHAPE axis, which is where it had never been used.
//
// The table reports EVERY rule that fires rather than the one the case expects.
// A case that names its own expected id can fabricate a false SILENT when the
// guess is wrong — which happened while this table was being written: the SQL
// template-literal rows were labelled SEC-010 and read as silent, when SEC-011
// catches them correctly.
type shapeCase struct {
	label string
	src   string
	// want is the comma-joined ids expected to fire, or "" for silence.
	want string
	// why is REQUIRED when want is "" — a silence without a written reason is
	// exactly the undeclared blindness this table exists to prevent.
	why string
}

var shapeCases = []shapeCase{
	// ---- SQL injection ------------------------------------------------------
	{label: "query, concatenated with +", src: `db.query("SELECT * FROM t WHERE id = " + id);`, want: "SEC-010"},
	{label: "query, interpolated template", src: "db.query(`SELECT * FROM t WHERE id = ${id}`);", want: "SEC-011"},
	{label: "query, awaited interpolated template", src: "await db.query(`SELECT * FROM t WHERE id = ${id}`);", want: "SEC-011"},
	{
		label: "query, bare variable", src: `db.query(q);`, want: "",
		why: "BY DESIGN, not a gap: the query built through an intermediate variable is SURFACE, " +
			"not a rule — the conclusion is not visible in the call subtree (ADR 0004). sql.yaml says so.",
	},
	{
		label: "prisma.$queryRawUnsafe, interpolated",
		src:   "prisma.$queryRawUnsafe(`SELECT * FROM t WHERE id = ${id}`);", want: "",
		why: "GAP. SEC-010/011 match the METHOD NAMES query/execute only, so Prisma's explicitly " +
			"unsafe raw API is silent — on the ORM this project treats as its flagship stack. " +
			"Not a shape limit: a method-vocabulary limit. Filed, not fixed here.",
	},
	{
		label: "knex.raw, interpolated", src: "knex.raw(`SELECT * FROM t WHERE id = ${id}`);", want: "",
		why: "GAP, same method-vocabulary root as $queryRawUnsafe: raw is not query/execute.",
	},
	{
		label: "better-sqlite3 prepare, interpolated",
		src:   "db.prepare(`SELECT * FROM t WHERE id = ${id}`).get();", want: "",
		why: "GAP, same root: prepare is not query/execute.",
	},

	// ---- Hardcoded secrets --------------------------------------------------
	{label: "secret, const declaration", src: `const apiKey = "sk-live-abc";`, want: "SEC-001"},
	{
		label: "secret, object property", src: `const cfg = { apiKey: "sk-live-abc" };`, want: "",
		why: "GAP, and the widest one measured: SEC-001 declares the single shape " +
			"`const $NAME = $VALUE`. A shape census of a real TypeScript project counted 1191 " +
			"object-literal string properties against 31 const-with-string-literal declarations — " +
			"the shape it reaches is ~38x rarer than the one it does not. The engine is not the " +
			"limit: SEC-080 matches an object property today.",
	},
	{
		label: "secret, class field", src: `class C { apiKey = "sk-live-abc"; }`, want: "",
		why: "GAP, same root as the object property. Same census counted 316 class-field string " +
			"assignments, ~10x the shape SEC-001 reaches.",
	},
	{
		label: "secret, let declaration", src: `let apiKey = "sk-live-abc";`, want: "SEC-001",
		why: "",
	},
	{
		label: "secret, var declaration", src: `var apiKey = "sk-live-abc";`, want: "",
		why: "GAP, same root: the pattern names `const`. Rare in modern TypeScript (7 occurrences " +
			"of any let/var string assignment in the censused project), so it is the least costly " +
			"of the three — recorded so the list is complete, not because it is urgent.",
	},
	{
		label: "secret, typed const", src: `const apiKey: string = "sk-live-abc";`, want: "",
		why: "GAP: a type annotation changes the declarator's shape and the pattern stops matching. " +
			"Zero occurrences in the censused project, so its prevalence is UNMEASURED here — " +
			"recorded as a shape fact, with no claim about how common it is.",
	},

	// ---- Insecure randomness ------------------------------------------------
	{label: "Math.random, const", src: `const token = Math.random().toString(36);`, want: "SEC-058"},
	{label: "Math.random, let", src: `let token = Math.random().toString(36);`, want: "SEC-058"},
	{
		label: "Math.random, object property",
		src:   `const c = { token: Math.random().toString(36) };`, want: "",
		why: "GAP: SEC-058's alternatives are both `const $T = …`, so it inherits SEC-001's " +
			"shape narrowness for the same reason.",
	},
	{
		label: "Math.random, returned", src: `function f() { return Math.random().toString(36); }`, want: "",
		why: "GAP, same root: a return is not a const declaration.",
	},

	// ---- eval ---------------------------------------------------------------
	{label: "eval(x)", src: `eval(userInput);`, want: "SEC-014"},
	{
		label: "setTimeout with a string body", src: `setTimeout("doThing()", 100);`, want: "",
		why: "GAP: the string form of setTimeout/setInterval is an eval channel and SEC-014 " +
			"declares eval and new Function only.",
	},
}

func TestShapeCensus(t *testing.T) {
	p := typescript.New()
	for _, tc := range shapeCases {
		t.Run(tc.label, func(t *testing.T) {
			if tc.want == "" && tc.why == "" {
				t.Fatal("a SILENCE must carry a written reason — an undeclared blindness is the " +
					"defect this table exists to prevent")
			}
			fs, err := p.AnalyzeSecurity(providers.SourceFile{Path: "app/x/route.ts", Content: []byte(tc.src)})
			if err != nil {
				t.Fatalf("analyze: %v", err)
			}
			var ids []string
			for _, f := range fs {
				ids = append(ids, f.ID)
			}
			got := strings.Join(ids, ",")
			if got != tc.want {
				t.Errorf("fired %q, want %q\n  src : %s\n  note: %s", got, tc.want, tc.src, tc.why)
			}
		})
	}
}

// The table must actually EXERCISE both outcomes. A census of only-fires or
// only-silences would pass while proving nothing about the axis it claims to
// measure — the same vacuity a fixture that dodges the case under test produces.
func TestShapeCensusExercisesBothOutcomes(t *testing.T) {
	var fires, silences int
	for _, tc := range shapeCases {
		if tc.want == "" {
			silences++
		} else {
			fires++
		}
	}
	if fires == 0 || silences == 0 {
		t.Fatalf("the census must contain both: fires=%d silences=%d", fires, silences)
	}
	t.Logf("shape census: %d shapes reached, %d silent (each with a written reason)", fires, silences)
}
