package sqlddl

import "github.com/codefit-cli/codefit/internal/core/db"

// Dialect is a per-SQL-dialect DATA descriptor consumed by the shared,
// dialect-free tokenizer (split.go) and reducer (reduce.go). Adding a new SQL
// dialect means adding a new Dialect value — never a branch in the tokenizer
// or the reducer (see docs/decisions for the SQL-dialect-descriptor ADR).
//
// Lexical fields (LineComments, IdentQuotes, DoubleQuoteIsString,
// DollarQuoting) are consumed ONLY by split.go: the tokenizer canonicalizes
// every identifier-quoting style to ANSI double-quotes ("...") as it emits
// statement text, so reduce.go's regexes and normalizeName never need to
// know the source dialect's quoting style.
//
// Vocabulary fields (TypeMap, Modifiers) are consumed ONLY by
// reduce.go/types.go: TypeMap maps a lowercased base type keyword to the
// neutral db.Type; Modifiers is the parse-and-ignore set of column-tail
// keywords that END the type expression (e.g. UNSIGNED, IDENTITY,
// AUTO_INCREMENT) without corrupting the mapped type into TypeUnknown.
type Dialect struct {
	// Name identifies the dialect (e.g. "postgresql", "mysql", "sqlserver").
	Name string

	// LineComments lists the line-comment prefixes this dialect recognizes
	// (e.g. {{"--", false}} for PostgreSQL/T-SQL, {{"--", true}, {"#", false}}
	// for MySQL). Block comments (/* ... */) are universal across dialects and
	// not a field.
	LineComments []LineComment

	// IdentQuotes lists the quoted-identifier delimiter pairs this dialect
	// recognizes (e.g. {{'"','"',true}} for PostgreSQL, {{'`','`',true}} for
	// MySQL, {{'[',']',true},{'"','"',true}} for T-SQL). Every quoted
	// identifier is re-emitted as a canonical ANSI "..." identifier
	// regardless of the source delimiter — the seam that keeps the reducer
	// dialect-free.
	IdentQuotes []QuotePair

	// DoubleQuoteIsString is true when this dialect treats a bare " as a
	// STRING literal delimiter rather than an identifier quote (MySQL's
	// default ANSI_QUOTES-off behavior). PostgreSQL and T-SQL: false.
	DoubleQuoteIsString bool

	// DollarQuoting enables PostgreSQL-style $tag$...$tag$ body delimiters
	// (PL/pgSQL DO/function bodies). MySQL and T-SQL: false.
	DollarQuoting bool

	// TypeMap maps a lowercased base type keyword (already stripped of
	// length/precision and array markers by typeBase) to the neutral
	// db.Type. An unmapped keyword falls back to db.TypeUnknown — the
	// honest fallback, never a crash or a silent drop.
	TypeMap map[string]db.Type

	// Modifiers is the set of UPPERCASE column-tail keywords that end the
	// type expression at splitTypeAndMods time — parsed and deliberately
	// ignored (e.g. UNSIGNED, AUTO_INCREMENT, IDENTITY), never corrupting
	// the mapped type.
	Modifiers map[string]bool

	// TriggerHasInlineBody is a per-dialect DATUM (Phase 2.2, Unit A2,
	// architecture/pg-trigger-body-link): does a CREATE TRIGGER statement in
	// this dialect carry its own logic inline, or is it only a WIRE to a
	// separately-defined function/procedure? PostgreSQL triggers are wires —
	// "CREATE TRIGGER x ... EXECUTE FUNCTION fn();" — false: there is no body
	// to capture, so the statement is always Complete and the logic is found
	// via Trigger.ExecutesFunction instead. MySQL and T-SQL triggers embed
	// their logic directly in the statement ("FOR EACH ROW SET ..." / "AS
	// BEGIN ... END") — true: reduce.go's routineBody derivation (the same
	// tokenizer-state formula routines use) still applies.
	//
	// reduce.go's triggerBody consults this DATUM instead of branching on
	// dialect.Name, keeping the one-shared-reducer/per-dialect-data
	// architecture ADR 0022 established — a new dialect only ever adds a
	// Dialect value, never a branch in split.go or reduce.go.
	TriggerHasInlineBody bool

	// RoutineBodyEndsAtBatchSeparator is a per-dialect DATUM (ADR 0027,
	// architecture/tsql-body-truncation-limit CLOSED): when true, a CREATE
	// FUNCTION/PROCEDURE/TRIGGER body is delimited by the GO batch separator
	// (or EOF), not by an internal ';'. split() consults this DATUM (via
	// isRoutineHead in reduce.go) to suspend ';'-flushing once it recognizes a
	// routine head, so the whole body is captured whole up to GO/EOF instead
	// of being cut at the body's first internal ';'.
	//
	// true ONLY for SQLServer: T-SQL has neither PostgreSQL's dollar-quoting
	// nor MySQL's DELIMITER directive to protect internal ';'s inside a
	// routine body, so without this DATUM every multi-statement T-SQL body
	// would truncate. PostgreSQL and MySQL set this explicitly false — their
	// existing dollar-quoting/DELIMITER mechanisms already capture the whole
	// body, and GO is not a batch separator in either dialect.
	RoutineBodyEndsAtBatchSeparator bool
}

// LineComment is one line-comment prefix a dialect recognizes.
//
// RequireBoundaryAfter encodes a real lexical divergence between dialects:
// PostgreSQL's "--" opens a line comment unconditionally, but MySQL's "--"
// opens one ONLY when immediately followed by a boundary — whitespace, a
// control char, or end-of-line/EOF (https://dev.mysql.com/doc/refman/8.0/en/comments.html).
// Without this distinction, "SELECT 1--1 FROM t;" under MySQL would be
// misread as "SELECT 1" with the rest silently swallowed as a comment. MySQL
// sets this true for "--" and false for "#" (which is unconditional in both
// dialects); PostgreSQL sets it false for "--".
type LineComment struct {
	// Prefix is the literal comment-opening text (e.g. "--", "#").
	Prefix string

	// RequireBoundaryAfter, when true, means Prefix opens a comment only if
	// the character immediately following it is a boundary (whitespace or a
	// control char) or there is no following character (end-of-line/EOF).
	// When false, Prefix is unconditional — matching as soon as it is found.
	RequireBoundaryAfter bool
}

// QuotePair is one quoted-identifier delimiter pair. Doubling is true when an
// occurrence of Close inside the identifier is escaped by writing it twice:
// two double-quotes mean one literal double-quote, two closing brackets mean
// one literal bracket, two backticks mean one literal backtick — true for
// every dialect this package supports.
type QuotePair struct {
	Open, Close byte
	Doubling    bool
}

// Postgres returns the PostgreSQL dialect descriptor. Its values reproduce
// TODAY's hardcoded parser behavior EXACTLY: this is the identity/no-op
// transform that keeps every existing caller and golden fixture
// byte-identical after the descriptor layer lands. It is also the default
// dialect for New() when no Option is given.
func Postgres() Dialect {
	return Dialect{
		Name:                 "postgresql",
		LineComments:         []LineComment{{Prefix: "--", RequireBoundaryAfter: false}},
		IdentQuotes:          []QuotePair{{Open: '"', Close: '"', Doubling: true}},
		DoubleQuoteIsString:  false,
		DollarQuoting:        true,
		TypeMap:              postgresTypeMap(),
		Modifiers:            postgresModifiers(),
		TriggerHasInlineBody: false,

		RoutineBodyEndsAtBatchSeparator: false,
	}
}

// MySQL returns the MySQL dialect descriptor: backtick-quoted identifiers,
// "--" and "#" line comments, and " as a STRING delimiter (MySQL's default
// ANSI_QUOTES-off behavior) rather than an identifier quote. No dollar
// quoting. Unlike PostgreSQL's unconditional "--", MySQL's "--" opens a
// comment ONLY when immediately followed by a boundary (whitespace, a
// control char, or end-of-line/EOF); "#" stays unconditional in both
// dialects. TypeMap and Modifiers hold the MySQL type/modifier vocabulary
// (Unit C, design §3): UNSIGNED/ZEROFILL/AUTO_INCREMENT/CHARACTER
// SET/COLLATE are parse-and-ignore Modifiers, never corrupting the mapped
// type.
func MySQL() Dialect {
	return Dialect{
		Name: "mysql",
		LineComments: []LineComment{
			{Prefix: "--", RequireBoundaryAfter: true},
			{Prefix: "#", RequireBoundaryAfter: false},
		},
		IdentQuotes:          []QuotePair{{Open: '`', Close: '`', Doubling: true}},
		DoubleQuoteIsString:  true,
		DollarQuoting:        false,
		TypeMap:              mysqlTypeMap(),
		Modifiers:            mysqlModifiers(),
		TriggerHasInlineBody: true,

		RoutineBodyEndsAtBatchSeparator: false,
	}
}

// SQLServer returns the T-SQL (SQL Server) dialect descriptor: this is the
// first dialect with TWO simultaneously-active IdentQuotes pairs — bracket
// identifiers ([Users], schema-qualified as [dbo].[Users]) AND ANSI
// double-quote identifiers (the QUOTED_IDENTIFIER ON default). Unlike PG's
// symmetric '"'/'"' pair and MySQL's symmetric '`'/'`' pair, the bracket pair
// is ASYMMETRIC (Open='[' != Close=']'); scanIdentQuoted already scans by
// Close alone so this required no tokenizer change (locked by
// TestSplit_SQLServerBracketIdentifierCanonicalized /
// ...BracketDoublingEscape). ']]' doubles to one literal ']', matching the
// universal Doubling convention. "--" is unconditional here (unlike MySQL's
// boundary-gated "--"). No dollar quoting. TypeMap and Modifiers hold the
// T-SQL type/modifier vocabulary (Unit F, design §3): IDENTITY/ROWGUIDCOL are
// parse-and-ignore Modifiers, never corrupting the mapped type.
func SQLServer() Dialect {
	return Dialect{
		Name:                 "sqlserver",
		LineComments:         []LineComment{{Prefix: "--", RequireBoundaryAfter: false}},
		IdentQuotes:          []QuotePair{{Open: '[', Close: ']', Doubling: true}, {Open: '"', Close: '"', Doubling: true}},
		DoubleQuoteIsString:  false,
		DollarQuoting:        false,
		TypeMap:              sqlserverTypeMap(),
		Modifiers:            sqlserverModifiers(),
		TriggerHasInlineBody: true,

		RoutineBodyEndsAtBatchSeparator: true,
	}
}
