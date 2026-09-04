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
	// The OPERATOR axis. SEC-010 declares concatenation; every other binary
	// operator produces the same node type with the same two named children, so
	// before the matcher compared node skeletons all of these fired SEC-010 —
	// an AFFIRMATION at confidence 1.0, which the baseline refuses to
	// auto-silence — on arithmetic that cannot build a string.
	{label: "query, concatenated with + and no spaces", src: `db.query("SELECT * FROM t WHERE id ="+id);`, want: "SEC-010"},
	{
		// Prettier's default trailingComma:"all" writes exactly this.
		label: "query, concatenated with + and a trailing comma",
		src: `db.query(
  "SELECT * FROM t WHERE id = " + id,
);`,
		want: "SEC-010",
	},
	{
		label: "query, subtraction", src: `db.query(total - discount);`, want: "",
		why: "CORRECT SILENCE, and it was a false AFFIRMATION until the matcher learned to " +
			"read operators: subtraction cannot concatenate a string, so there is no inline " +
			"query assembly to report.",
	},
	{
		label: "query, multiplication", src: `db.query(a * b);`, want: "",
		why: "CORRECT SILENCE, same root as subtraction.",
	},
	{
		label: "query, modulo", src: `db.query(a % b);`, want: "",
		why: "CORRECT SILENCE, same root as subtraction. This was the sharpest form of the " +
			"defect: a SQL-injection finding affirmed on a modulo.",
	},
	{
		label: "query, division", src: `db.query(a / b);`, want: "",
		why: "CORRECT SILENCE, same root as subtraction.",
	},
	{
		label: "query, comparison", src: `db.query(a > b);`, want: "",
		why: "CORRECT SILENCE, same root: a comparison is a boolean, not an assembled query.",
	},
	{
		label: "query, logical and", src: `db.query(a && b);`, want: "",
		why: "CORRECT SILENCE, same root: && is a binary_expression too.",
	},
	{label: "query, awaited interpolated template", src: "await db.query(`SELECT * FROM t WHERE id = ${id}`);", want: "SEC-011"},
	{
		label: "query, bare variable", src: `db.query(q);`, want: "",
		why: "BY DESIGN, not a gap: the query built through an intermediate variable is SURFACE, " +
			"not a rule — the conclusion is not visible in the call subtree (ADR 0004). sql.yaml says so.",
	},
	{
		label: "prisma.$queryRawUnsafe, interpolated",
		src:   "prisma.$queryRawUnsafe(`SELECT * FROM t WHERE id = ${id}`);", want: "SEC-011",
	},
	{
		label: "prisma.$executeRawUnsafe, interpolated",
		src:   "prisma.$executeRawUnsafe(`DELETE FROM t WHERE id = ${id}`);", want: "SEC-011",
	},
	{
		label: "prisma.$queryRawUnsafe, concatenated",
		src:   `prisma.$queryRawUnsafe("SELECT * FROM t WHERE id = " + id);`, want: "SEC-010",
	},
	{
		label: "prisma.$queryRaw TAGGED — SAFE, must stay silent",
		src:   "prisma.$queryRaw`SELECT * FROM t WHERE id = ${id}`;", want: "",
		why: "NOT a gap — the single most important negative fixture in this file. Prisma's TAGGED " +
			"$queryRaw PARAMETERISES its interpolations; it is the CORRECT way to write raw SQL and " +
			"firing on it would be a false positive at confidence 1.0. The census measured this shape " +
			"3 times in a real Prisma project while the unsafe forms appeared ZERO times, so a careless " +
			"widening would have produced 3 false affirmations and caught nothing at all. " +
			"WHAT SEPARATES THE TWO FORMS, measured rather than assumed: both parse to a call_expression " +
			"with two named children, so the node TYPE is identical. Only the second child differs — " +
			"arguments for the called form, template_string for the tagged one. That is the whole basis " +
			"of the distinction, and it is why call-shaped patterns cannot reach the tagged form. " +
			"PROVEN BY MUTATION: adding a tagged pattern carrying this row's exact template text to " +
			"SEC-010 turns this row and its $executeRaw twin RED. Reverting restores green. " +
			"AIM IT AT SEC-010, NOT SEC-011, and this cost four wasted attempts to learn: SEC-011 " +
			"carries a metavariable-regex on $Q, so a mutation injected there is swallowed by the " +
			"regex before it can reach the fixture, and the green that comes back reads exactly like a " +
			"weak test. It is not. A filter on the mutated rule silently disarms the mutation — when a " +
			"row refuses to break, suspect the aim before the lock.",
	},
	{
		label: "prisma.$executeRaw TAGGED — SAFE, must stay silent",
		src:   "prisma.$executeRaw`DELETE FROM t WHERE id = ${id}`;", want: "",
		why: "Same as $queryRaw tagged: parameterised, correct, must never fire.",
	},
	{
		label: "knex.raw, interpolated", src: "knex.raw(`SELECT * FROM t WHERE id = ${id}`);", want: "SEC-011",
	},
	{
		label: "better-sqlite3 prepare, interpolated",
		src:   "db.prepare(`SELECT * FROM t WHERE id = ${id}`).get();", want: "",
		why: "GAP, same root: prepare is not query/execute.",
	},

	// ---- Hardcoded secrets --------------------------------------------------
	{label: "secret, const declaration", src: `const apiKey = "sk-live-abc";`, want: "SEC-001"},
	{label: "secret, object property (arity 1)", src: `const cfg = { apiKey: "sk-live-abc" };`, want: "SEC-001"},
	{
		label: "secret, object property in a MULTI-property object",
		src:   `const cfg = { baseUrl: "u", apiKey: "sk-live-abc", retries: 3 };`, want: "SEC-001",
		why: "THE shape the census says a credential actually lives in. Across two real " +
			"TypeScript projects every object holding a credential-named string property had " +
			"arity 5, 5, 5 and 3 — four of four multi-property, ZERO at arity 1. Reaching it " +
			"needed the scoped object ellipsis in the matcher, because the exact-arity rule " +
			"could not be written.",
	},
	{
		label: "object property, credential name but NON-string value",
		src:   `const cfg = { baseUrl: "u", apiKey: readKey(), retries: 3 };`, want: "",
		why: "the $VALUE metavariable-regex still demands a quoted literal; the ellipsis " +
			"widened WHICH objects are reached, never what counts as a secret",
	},
	{
		label: "multi-property object with no credential name",
		src:   `const cfg = { baseUrl: "u", retries: 3, label: "x" };`, want: "",
		why: "subset matching must not degrade into matching any object — the $NAME regex " +
			"is what still decides, and this row is what proves it",
	},
	{
		label: "secret, class field", src: `class C { apiKey = "sk-live-abc"; }`, want: "",
		why: "THE GAP THAT REMAINS after the object ellipsis closed the object one, and the " +
			"largest still open: the same census counted 316 class-field string assignments, " +
			"about 10x the const shape the rule already reached. It is NOT the arity problem " +
			"the ellipsis solved, and that was measured rather than assumed — the pattern " +
			"`class $C { $NAME = $VALUE }` finds ZERO even in a class with a SINGLE field, so " +
			"widening the ellipsis to class bodies would not reach it either. The pattern " +
			"unwraps to class_declaration (unwrap peels only program, expression_statement and " +
			"parenthesized_expression), and a rule cannot address the public_field_definition " +
			"node on its own. Closing it needs a different mechanism, not another alternative. " +
			"Tracked separately so this row is a declared limit and not a silent one.",
	},
	{
		// STILL fires, but for a different reason than it used to. It was reached
		// by the operator blindness — the declaration keyword is not a named
		// child, so every lexical_declaration looked alike and `const $NAME =
		// $VALUE` caught `let` by the same accident that made `$A + $B` catch a
		// modulo. secrets.yaml now DECLARES `let`, so the verdict is unchanged
		// and the reason is honest.
		label: "secret, let declaration", src: `let apiKey = "sk-live-abc";`, want: "SEC-001",
	},
	{label: "secret, var declaration", src: `var apiKey = "sk-live-abc";`, want: "SEC-001"},

	// ---- SEC-001, the NAME axis (issue #152) -------------------------------
	// The rows above vary the SHAPE the name is written in. These vary the NAME
	// itself, and they are the axis that was never censused: $NAME was matched
	// by unanchored substring, so any identifier merely CONTAINING a credential
	// word was affirmed at confidence 1.0. Measured over 41 names, the switch to
	// component matching kills 9 such names and costs zero true positives.
	{
		label: "NOT a secret: tokenizer", src: `const tokenizer = "whitespace";`, want: "",
		why: "issue #152 verbatim. It contains \"token\" and is not a credential. This was " +
			"an AFFIRMATION at confidence 1.0 — the class the baseline refuses to auto-silence " +
			"(ADR 0011), so it reappeared on every scan until a human accepted it by hand. The " +
			"loudest and stickiest thing codefit can emit, on a tokenizer.",
	},
	{
		label: "NOT a secret: secretariat", src: `const secretariat = "un";`, want: "",
		why: "NOT a gap — a required silence. It contains \"secret\" and tokenizes to the single " +
			"component \"secretariat\", which is not in the vocabulary. Firing here would be a " +
			"false affirmation at confidence 1.0.",
	},
	{
		label: "NOT a secret: passwordless", src: `const passwordless = "on";`, want: "",
		why: "NOT a gap — a required silence, and the sharpest of the three: it contains " +
			"\"password\" and names the feature that means there ISN'T one.",
	},
	{
		label: "NOT a secret: subtokenizer", src: `const subtokenizer = "x";`, want: "",
		why: "NOT a gap — a required silence. It carries \"token\" in the MIDDLE of a component, " +
			"which is precisely what a regex cannot exclude in RE2: excluding it needs a lookbehind, " +
			"and that is why this is an operator and not a better regex.",
	},
	{
		label: "NOT a secret: tokenizer as an object property",
		src:   `const cfg = { baseUrl: "u", tokenizer: "ws", n: 1 };`, want: "",
		why: "the object ellipsis shipped one release earlier, so the substring defect had just " +
			"been handed every config object in the project. Widening the SHAPE axis without " +
			"fixing the NAME axis multiplies a false positive rather than adding one.",
	},
	{
		label: "still a secret: API_KEY", src: `const API_KEY = "sk-live-abc";`, want: "SEC-001",
		why: "the trap ADR 0075 measured in Go: lower(\"API_KEY\") is \"api_key\", which does NOT " +
			"contain \"apikey\", so deleting the substring arm without a component matcher costs " +
			"real findings. This row is what proves the replacement is a repair and not a trade.",
	},
	{
		label: "still a secret: credential", src: `const credential = "abc123";`, want: "SEC-001",
		why: "the ONE true positive the substring regex carried that the component vocabulary " +
			"did not. Closing #152 required adding it to namematch — which also fixed the " +
			"matching silent gap in Go, where this declaration fired nothing at all.",
	},
	{label: "secret, typed const", src: `const apiKey: string = "sk-live-abc";`, want: "SEC-001"},

	// ---- Insecure randomness ------------------------------------------------
	{label: "Math.random, const", src: `const token = Math.random().toString(36);`, want: "SEC-058"},
	{
		// Same story as the `let` secret above: reached by accident before, named
		// by crypto.yaml now. Verdict unchanged, reason honest.
		label: "Math.random, let", src: `let token = Math.random().toString(36);`, want: "SEC-058",
	},
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

	// ---- XSS, and THE ARITY LIMIT --------------------------------------------
	//
	// These rows exist because the arity limit was invisible: it affects a rule
	// that ships today, and COVERAGE.md described that rule without it.
	{
		label: "dangerouslySetInnerHTML, interpolated, sole property",
		src:   "f({__html: `<b>${x}</b>`});", want: "SEC-080",
	},
	{
		label: "dangerouslySetInnerHTML, interpolated, TWO properties",
		src:   "f({__html: `<b>${x}</b>`, className: \"x\"});", want: "SEC-080",
		why: "THE ARITY LIMIT, and it is a consequence of a DECLARED design decision rather than a " +
			"bug: matchNode requires the named-child COUNT to be equal, so an object pattern with one " +
			"pair matches only an object with exactly one pair. Reaching a property inside a larger " +
			"object needs an ellipsis, and this engine deliberately has none (matcher.go, " +
			"rules/README.md, PRD section 17). The canonical JSX shape is a one-property object, so " +
			"the common case works — this row is here so the limit stops being invisible.",
	},
	{
		label: "dangerouslySetInnerHTML, interpolated, THREE properties",
		src:   "f({id: 1, __html: `<b>${x}</b>`, className: \"x\"});", want: "SEC-080",
		why: "Same arity limit. Two rows, not one, because a single row could be read as an " +
			"off-by-one rather than as 'any object larger than the pattern'.",
	},

	// ---- eval ---------------------------------------------------------------
	{label: "eval(x)", src: `eval(userInput);`, want: "SEC-014"},
	{
		label: "setTimeout with a string body", src: `setTimeout("doThing()", 100);`, want: "",
		why: "GAP: the string form of setTimeout/setInterval is an eval channel and SEC-014 " +
			"declares eval and new Function only.",
	},

	// ---- XSS (dangerouslySetInnerHTML) --------------------------------------
	// Present so the operator axis is pinned on a SECOND rule: SEC-079 shares
	// SEC-010's `$A + $B` shape and shared its blindness, which is the evidence
	// that the defect lived in the matcher and not in one rule's pattern.
	{label: "__html, concatenated with +", src: `const p = {__html: a + b};`, want: "SEC-079"},
	{
		label: "__html, subtraction", src: `const p = {__html: a - b};`, want: "",
		why: "CORRECT SILENCE, same operator-axis root as the SQL rows: SEC-079 declares " +
			"concatenation, and subtraction cannot build markup.",
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
