package golang

import (
	"github.com/codefit-cli/codefit/internal/core/namematch"
	"github.com/codefit-cli/codefit/internal/core/surface"
	"github.com/codefit-cli/codefit/internal/providers"
)

// Capability declares what the Go provider implements, measured against
// security.go/practices.go/surface.go at main@ee0b263: 6 security rule IDs, 4
// practices rule IDs, and exactly ONE surface category (authz — HTTP
// handlers, surface.go). Both RuleSets are Enumerable:false: Go's detectors
// have no All()/ID() rule-registry loader (unlike TypeScript's YAML-backed
// security rules), so Declared is a hand-maintained mirror of the switch in
// security.go/practices.go, not a derived list — Control A
// (internal/providers/typescript's Enumerable:true declaration) does not
// apply here. CoverageManifest is false: the Go provider implements no
// CoverageManifest() method today (P1-4b's coverage-manifest half stays
// open; R1, ADR 0065, makes codefit-coverage derive an honest answer for
// go without one).
//
// Practices.Excluded carries PRAC-004 — permanently dropped per ADR 0056,
// not merely unimplemented: it fired on every `go` statement claiming "no
// visible WaitGroup or channel" with zero synchronization detection,
// because go/ast without go/types cannot see synchronization living in a
// callee, a struct field, or an errgroup, and the practices dimension is
// deterministic by spec (no surface channel to route an uncertain signal
// into). This is P1-4b's owed manifest entry: the drop was recorded only
// in the ADR and the CHANGELOG until now, so an agent asking what codefit
// covers for Go was left to infer the hole from PRAC-004's absence.
func (*Provider) Capability() providers.Capability {
	return providers.Capability{
		Security: providers.RuleSet{
			Declared:   []string{"SEC-001", "SEC-010", "SEC-013", "SEC-040", "SEC-050", "SEC-052"},
			Enumerable: false,
			Limits: []providers.RuleLimit{
				{
					ID: "SEC-001",
					// Cited BY REFERENCE, never copied. The const lives beside
					// the tokenizer that causes the gap, so this declaration
					// cannot drift from the behaviour it describes: changing one
					// without the other does not compile into a disagreement,
					// because there is only one string.
					Limit: namematch.LimitLowercaseConcatenation,
				},
				{
					ID: "SEC-001",
					// The VALUE half (ADR 0086), cited by reference for the same
					// reason: the const lives in security.go beside the veto it
					// describes. A literal the veto silences is otherwise
					// indistinguishable, to an agent, from "no credential here".
					// It is preceded by what reaches the veto at all
					// (limitOneLiteralOnly, below) and by the escapes only Go
					// decodes (limitGoOnlyEscapes): the three describe one
					// question — which values SEC-001 reads, what it reads them
					// as, and which of those it withholds — so they are served as
					// one limit.
					Limit: limitOneLiteralOnly + limitGoOnlyEscapes + limitValueVeto,
				},
			},
		},
		Practices: providers.RuleSet{
			Declared:   []string{"PRAC-001", "PRAC-002", "PRAC-003", "PRAC-005"},
			Enumerable: false,
			Excluded: []providers.ExcludedRule{
				{
					ID: "PRAC-004",
					Reason: "permanently dropped (ADR 0056): fired on every `go` statement " +
						"claiming \"no visible WaitGroup or channel\" with no synchronization " +
						"detection at all — synchronization can live in the callee, a struct " +
						"field, or an errgroup, none of which go/ast without go/types can see, " +
						"and the practices dimension is deterministic by spec, with no surface " +
						"channel to route an uncertain signal into",
				},
			},
		},
		Surface:          []surface.Category{surface.CategoryAuthz},
		CoverageManifest: false,
	}
}

// limitOneLiteralOnly declares the values and names Go's name-and-value rule
// for SEC-001 never reads (issue #148). hardcodedSecret asks stringLit for the
// value, and stringLit accepts one *ast.BasicLit of kind STRING and nothing
// else; securityChecks reads a name only as an *ast.Ident. Each form named here
// is measured through the real provider by TestSEC001ValueFormsGoNeverReaches
// and TestSEC001ShapesGoNeverReaches, which also require its phrase to appear in
// the served limit. The rule is not SEC-001's only producer: the sensor's line
// scan (apiKeyPatterns, internal/sensors/security) reports a known-format API
// key on any line, which TestSEC001LineScanReportsKnownFormatKeysInFormsTheRuleDoesNotRead
// pins through the real sensor; the text says so, so "not read by the rule" is
// never read as "not reported". It is a prefix of the value-veto limit, not a limit of
// its own, and it must not contain the phrases the value-veto tests parse ("at
// every site SEC-001 reaches (", or the word "forms" before a list).
const limitOneLiteralOnly = "SEC-001's name-and-value rule (go) reads a value only when it is ONE string " +
	"literal, interpreted or raw, bound to a name it can read. A value written any other way is neither vetoed nor " +
	"reported BY THAT RULE, whatever it holds: a concatenation of literals (\"s3cr\" + \"3t\"), a parenthesized " +
	"literal, a conversion such as string(\"s3cr3t\"), a reference to a constant or a variable (the provider parses " +
	"without type information and does not follow the name), and a call that returns a fallback, such as " +
	"cmp.Or(os.Getenv(\"KEY\"), \"s3cr3t\") (Go has no ||, ?? or ternary on strings). The same holds for a name " +
	"the rule does not read: an assignment to a field through a selector (c.signingKey = \"…\") and a string map " +
	"key ({\"signingKey\": \"…\"}). Separately, the security sensor's line scan reports SEC-001 on any line that " +
	"holds a value in one of five known API-key formats — an Anthropic key (sk-ant- and 8 or more characters), " +
	"an OpenAI key (sk- and 20 or more letters or digits), a GitHub personal access token (ghp_ and 20 or more), " +
	"a Slack token (xoxb-, xoxa-, xoxp-, xoxr- or xoxs- and 10 or more) or an AWS access key id (AKIA and 16 " +
	"upper-case letters or digits) — " +
	"whatever the code around it, so a key in one of those formats is still reported in every shape above. " +
	"TypeScript's SEC-001 binds a concatenation whose first and last operands are quoted literals on one line " +
	"and keeps firing on it, so the two providers differ there. "

// limitGoOnlyEscapes declares the two known differences between the providers on
// the same spelling that come from escapes only Go decodes (issue #148,
// declared 2026-11-28, not fixed). Both are rows of the value-axis parity table,
// internal/core/valueshape/crossprovider_test.go, admitted there by ADR 0087's
// sentence that these escapes exist only in Go; TestCrossProviderDivergencesAreServed
// there requires this text and the TypeScript manifest to name both spellings.
const limitGoOnlyEscapes = "Go decodes \\a (a bell byte) and \\U00… escapes, which JavaScript reads " +
	"as the letter itself, so the same spelling can get two verdicts, both known and not fixed: " +
	"\"lib/config\\a.ts\" fires in Go (the bell byte is outside the file-path charset) and is vetoed in " +
	"TypeScript (lib/configa.ts is a path); \"https:\\U0000002f\\U0000002fapi.example.com/v1/users\" is " +
	"vetoed in Go (a URL) and fires in TypeScript (no :// survives). An octal escape (\\056) in a path " +
	"is vetoed in Go and fires in TypeScript, whose decoder refuses a legacy octal escape. "
