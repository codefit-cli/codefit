package namematch_test

import (
	"testing"

	"github.com/codefit-cli/codefit/internal/core/namematch"
	"github.com/codefit-cli/codefit/internal/core/ruleengine"
	"github.com/codefit-cli/codefit/rules"
)

// The Go core cannot import a provider, and TypeScript's credential vocabulary
// is not Go code at all — it is a RE2 metavariable regex inside embedded YAML.
// So the two vocabularies cannot be shared. They are BOUND instead, by the case
// table below: every name carries the verdict of BOTH engines, and a divergence
// is admissible only when it is listed with a written reason.
//
// SCOPE, stated so the table cannot over-claim: this binds the NAME GATE ONLY.
// It is not a claim that the two providers emit the same findings. TypeScript's
// SEC-001 additionally requires the syntactic shape `const $NAME = $VALUE` and a
// quoted $VALUE, while Go fires on assignment, value spec AND composite-literal
// key (*ast.KeyValueExpr) alike. A row saying goFires==tsFires means the two
// NAME vocabularies agree about that name, nothing more.
type crossCase struct {
	name    string
	goFires bool
	tsFires bool
	// divergence is REQUIRED when goFires != tsFires and forbidden otherwise.
	divergence string
}

var crossCases = []crossCase{
	// --- Agreement: the credential spellings both engines recognise. ---
	{name: "password", goFires: true, tsFires: true},
	{name: "passwd", goFires: true, tsFires: true},
	{name: "pwd", goFires: true, tsFires: true},
	{name: "secret", goFires: true, tsFires: true},
	{name: "apiKey", goFires: true, tsFires: true},
	{name: "api_key", goFires: true, tsFires: true},
	{name: "API_KEY", goFires: true, tsFires: true},
	{name: "token", goFires: true, tsFires: true},
	{name: "accessToken", goFires: true, tsFires: true},
	{name: "refreshToken", goFires: true, tsFires: true},
	{name: "privateKey", goFires: true, tsFires: true},
	{name: "accessKey", goFires: true, tsFires: true},
	// These two spellings exist to EXERCISE the accesstoken/refreshtoken
	// vocabulary entries, which no camelCase or delimited spelling can reach:
	// in accessToken the component "token" matches before the api-style pair is
	// ever tried, so the compound entry is only reachable when the whole name is
	// one lowercase component. Without these rows Control 3 reports the two
	// entries as unexercised — which is exactly what it did, and is the reason
	// they are written this way rather than as accessToken/refreshToken.
	{name: "accesstoken", goFires: true, tsFires: true},
	{name: "refreshtoken", goFires: true, tsFires: true},

	// --- The PLURAL class. ---
	//
	// These rows exist because their absence hid a real narrowing. TS matches
	// $NAME as an unanchored substring, so it never stopped firing on
	// "passwords"; Go's move to component matching turned each plural into ONE
	// component outside the set and went silent. With no plural row, Control 2
	// had nothing to compare and the divergence was unstateable — a table's
	// guarantee is only as wide as its rows. The spellings are the measured
	// losses, not invented examples.
	{name: "passwords", goFires: true, tsFires: true},
	{name: "secrets", goFires: true, tsFires: true},
	{name: "tokens", goFires: true, tsFires: true},
	{name: "passwds", goFires: true, tsFires: true},
	{name: "pwds", goFires: true, tsFires: true},
	{name: "apiKeys", goFires: true, tsFires: true},
	{name: "apikeys", goFires: true, tsFires: true},
	{name: "privateKeys", goFires: true, tsFires: true},
	{name: "accessKeys", goFires: true, tsFires: true},
	{name: "refreshTokens", goFires: true, tsFires: true},
	{name: "mySecrets", goFires: true, tsFires: true},
	{name: "userPasswords", goFires: true, tsFires: true},
	// Same reachability property as the singular compounds above: in
	// accessTokens the component "tokens" matches before any pair is tried, so
	// the compound plural entries are only reachable as one lowercase run.
	{name: "accesstokens", goFires: true, tsFires: true},
	{name: "refreshtokens", goFires: true, tsFires: true},
	{
		name: "signingKeys", goFires: true, tsFires: true,
		divergence: "",
	},
	{
		name: "encryptionKeys", goFires: true, tsFires: true,
		divergence: "",
	},
	// The fold's false-positive side, stated as rows rather than argued in a
	// comment. publickey is refused by the vocabulary, so its plural is refused
	// too; keys/keyboards are the substring class this change silenced, and a
	// plural fold must not smuggle them back.
	{name: "publicKeys", goFires: false, tsFires: false},
	{name: "keys", goFires: false, tsFires: false},
	{name: "keyboards", goFires: false, tsFires: false},
	{
		name: "tokenizers", goFires: false, tsFires: false,
		divergence: "",
	},

	// --- Agreement: names neither engine treats as a credential. ---
	{name: "userId", goFires: false, tsFires: false},
	{name: "count", goFires: false, tsFires: false},
	{name: "description", goFires: false, tsFires: false},

	// --- Divergence, each with its reason. ---
	//
	// EMPTY SINCE ISSUE #152, and that is the result rather than an oversight.
	// Every row below used to disagree because the two providers asked for the
	// same words through two different mechanisms — a component matcher in Go, an
	// unanchored regex in TypeScript. They now ask through ONE, so the class
	// cannot return by drift: only by editing SEC-001's declaration, which
	// tsNameVocabulary fails on. The rows stay, because they are the enumerated
	// evidence that these specific names get these specific verdicts.
	{
		name: "tokenizer", goFires: false, tsFires: false,
		divergence: "",
	},
	{
		name: "monkeyIndex", goFires: false, tsFires: false,
		divergence: "",
	},
	{
		name: "signingKey", goFires: true, tsFires: true,
		divergence: "",
	},
	{
		name: "encryptionKey", goFires: true, tsFires: true,
		divergence: "",
	},
	{
		name: "credential", goFires: true, tsFires: true,
		divergence: "",
	},
	{
		// The plural of the row above, carried by the fold. TS's unanchored
		// regex reaches it as a substring of "credentials"; Go reaches it as a
		// folded component. Same verdict, and after #152 the same mechanism.
		name: "credentials", goFires: true, tsFires: true,
		divergence: "",
	},
	{
		name: "authToken", goFires: true, tsFires: true,
		divergence: "",
	},
	{
		name: "clientSecret", goFires: true, tsFires: true,
		divergence: "",
	},
	{
		name: "publicKey", goFires: false, tsFires: false,
		divergence: "",
	},
	{
		// THE ONE ROW WHERE CONVERGENCE COST SOMETHING, recorded here rather than
		// absorbed. Go's declared limit (namematch.LimitLowercaseConcatenation)
		// is that an all-lowercase concatenation carries no boundary to tokenize
		// on. TypeScript's substring matcher happened to catch it; the shared
		// matcher does not, so `const secretkey = "…"` went from firing in TS to
		// silent in both.
		//
		// That is a NARROWING, and an undeclared one is not permitted here — so
		// it is declared in this row and in COVERAGE.md. It is ACCEPTED because
		// the mechanism that carried this single spelling is the same one that
		// affirmed `tokenizer` as a credential at confidence 1.0: keeping it
		// would have meant keeping nine false affirmations with it.
		name: "secretkey", goFires: false, tsFires: false,
		divergence: "",
	},
}

// tsNameRegex loads TypeScript's SEC-001 $NAME metavariable regex from the REAL
// embedded YAML. It is never copied into this file: a copy would be a fifth
// vocabulary, and the drift it introduced would be invisible precisely because
// the test would keep passing.
//
// Every failure path is t.Fatal, never a nil regex. A nil or zero-value regex
// quietly matches nothing, which would render every tsFires:false row green
// while proving nothing at all — the exact vacuum this binding exists to close.
// WHAT THIS BINDING BECAME (issue #152). It used to load SEC-001's $NAME REGEX
// and compare its verdicts against Go's component matcher — two mechanisms, one
// vocabulary written twice, and the table was the only thing keeping them in
// step. It stopped keeping them in step in one case it faithfully recorded:
// `tokenizer` fired in TS and not in Go, an affirmation at confidence 1.0 on a
// word that merely contains "token".
//
// TypeScript now declares the SHARED vocabulary directly, so the two columns are
// the same function of the same set and that divergence class cannot come back
// by drift — only by someone editing this declaration, which is exactly what
// this function now fails on. The table did not become redundant: it became the
// enumerated evidence that specific real names get specific verdicts, and
// Control 3 still refuses to let a vocabulary token ship with no row.
//
// Every failure path is t.Fatal, never a nil set. A nil set quietly matches
// nothing, which would render every tsFires:false row green while proving
// nothing at all — the exact vacuum this binding exists to close.
func tsNameVocabulary(t *testing.T) map[string]bool {
	t.Helper()
	loaded, err := ruleengine.LoadFS(rules.FS, "typescript/security")
	if err != nil {
		t.Fatalf("loading typescript/security rules from the embedded FS: %v", err)
	}
	if len(loaded) == 0 {
		t.Fatal("vacuum: the embedded typescript/security rule set is empty")
	}
	for _, r := range loaded {
		if r.ID != "SEC-001" {
			continue
		}
		if expr, ok := r.MetavariableRegex["$NAME"]; ok {
			t.Fatalf("SEC-001 constrains $NAME with a REGEX again (%q). That is the substring "+
				"defect of issue #152 returning: an unanchored regex affirms `tokenizer` as a "+
				"credential at confidence 1.0, and RE2 cannot anchor to a component boundary. "+
				"$NAME must use metavariable-name.", expr)
		}
		vocab, ok := r.MetavariableName["$NAME"]
		if !ok {
			t.Fatalf("SEC-001 does not constrain $NAME by name at all; it declares metavariable-name=%v. "+
				"Without it the rule affirms a hardcoded credential for ANY identifier.", r.MetavariableName)
		}
		if vocab != "credential" {
			t.Fatalf("SEC-001 constrains $NAME against vocabulary %q, not \"credential\". The whole "+
				"point of the shared vocabulary is that both providers ask for the same words.", vocab)
		}
		return namematch.Credential()
	}
	t.Fatalf("no rule with id SEC-001 in typescript/security (%d rules loaded)", len(loaded))
	return nil
}

// TestCrossProviderNameGate runs the four controls that make drift impossible
// rather than merely discouraged.
func TestCrossProviderNameGate(t *testing.T) {
	tsVocab := tsNameVocabulary(t)
	tsFires := func(name string) bool {
		_, hit := namematch.MatchSet(name, tsVocab)
		return hit
	}

	// Positive control on the loader itself: if this matched nothing, the whole
	// TS column would be a constant false and every agreement row would pass by
	// vacuum. And the negative half, so it is not a constant true either.
	if !tsFires("password") {
		t.Fatalf("vacuum: TS $NAME does not fire on \"password\" — the loaded vocabulary is not the credential set")
	}
	if tsFires("userId") {
		t.Fatalf("vacuum: TS $NAME fires on \"userId\" — the loaded vocabulary matches everything")
	}

	cred := namematch.Credential()
	// exercised records which VOCABULARY TOKEN each row actually exercised, as
	// measured by the matcher — not which spelling the row was written with.
	// Keying on the spelling would let a row named "apiKey" be mistaken for
	// coverage of a token called "apikey" only by string luck, and would miss
	// the case entirely for a token no row reaches.
	exercised := map[string]bool{}

	for _, c := range crossCases {
		// Control 1 — verdict fidelity. The declared verdict must equal the
		// MEASURED one, in both columns. Without this the table drifts away
		// from reality and becomes documentation.
		tok, goGot := namematch.MatchSet(c.name, cred)
		if goGot {
			exercised[tok] = true
		}
		if goGot != c.goFires {
			t.Errorf("%q: Go name gate fires=%v, table declares %v", c.name, goGot, c.goFires)
		}
		tsGot := tsFires(c.name)
		if tsGot != c.tsFires {
			t.Errorf("%q: TS $NAME fires=%v, table declares %v", c.name, tsGot, c.tsFires)
		}

		// Control 2 — agreement or reason. A disagreement with no written
		// reason is drift that nobody decided on.
		if c.goFires != c.tsFires && c.divergence == "" {
			t.Errorf("%q: go=%v ts=%v disagree with no divergence reason recorded",
				c.name, c.goFires, c.tsFires)
		}

		// Control 4 — no ghost reasons. A reason surviving after the engines
		// converged is a stale claim, and a stale claim reads as a live one.
		if c.goFires == c.tsFires && c.divergence != "" {
			t.Errorf("%q: go and ts agree (%v) but a divergence reason is still recorded: %s",
				c.name, c.goFires, c.divergence)
		}
	}

	// Control 3 — no silent widening. Every token in Go's credential set must
	// be EXERCISED by some row, so a vocabulary addition cannot ship without
	// its cross-provider verdict being stated. This is the inverse of the
	// deliberatelyNotInSkill allowlist already used in this repo: that one
	// forbids a tool with no mention, this one forbids a token with no verdict.
	for tok := range cred {
		if !exercised[tok] {
			t.Errorf("credential token %q is exercised by no row in the cross-provider "+
				"case table — a widening cannot ship without declaring its TS verdict", tok)
		}
	}
}
