package db

import "strings"

// Schema is the parsed structure of a database. It models the whole OLTP surface
// Phase 2 audits; a given parser fills the subset its format expresses (a Prisma
// parse leaves Views/Procedures/Triggers empty — slice 3's SQL-DDL parser fills
// them).
type Schema struct {
	Tables     []Table
	Views      []View      // OLTP surface; not filled by the Prisma parser
	Procedures []Procedure // idem
	Triggers   []Trigger   // idem

	// Unreduced carries statements the parser recognized as table-affecting
	// (e.g. "alter table ...") but could not attribute to any specific table
	// (a regex miss on the table name itself) — design §2, drop site 2. It
	// gates NOTHING per-table (there is no table to gate); it surfaces only in
	// the per-scan completeness inventory (sensors/db.Result.Note).
	Unreduced []Unreduced
}

// Pos is the origin of a schema element: the file it was parsed from and its
// 1-based line. Every element carries one so a finding and the baseline can anchor
// by file+line/content (the access-layer missing-line gap, relevamiento §E, not
// repeated here).
type Pos struct {
	File string
	Line int
}

// Table is a relation: its columns, primary key, foreign keys and indexes. PK and
// FK live at the table level (not as Column flags) because they can be composite
// and the membership is derivable by column name — see ADR 0014.
type Table struct {
	Name string
	// DBName is the real table name in the database when remapped via @@map (or the
	// SQL identifier when it differs from the model name). EMPTY means "no remap" —
	// a consumer falls back to Name; it is deliberately NOT defaulted to Name, so a
	// rule can tell an explicit remap from none. FKs/indexes reference by model
	// name, not DBName (ADR 0014).
	DBName      string
	Pos         Pos
	Columns     []Column
	PrimaryKey  []string // column names; empty = no PK (DB-050); len>1 = composite
	ForeignKeys []ForeignKey
	Indexes     []Index

	// Partitioning is what this table's DDL declares about TABLE
	// partitioning. Its zero value means "the source codefit read declares
	// none" — see the type's own doc for what a consumer may and may not
	// conclude from that.
	Partitioning Partitioning

	// Complete=false means the parser met at least one statement affecting
	// this table that it could NOT reduce, and could not rule out that the
	// statement declared a key, index or column. A rule that concludes from
	// ABSENCE MUST treat Complete==false as grounds to abstain or downgrade to
	// a surface item, never to emit a deterministic finding — ADR 0004 made
	// mechanical for the STRUCTURAL model exactly as ADR 0025 made it
	// mechanical for routine bodies (Body.Complete).
	//
	// BOUNDARY, so the contract does not over-promise: Complete covers DROPS,
	// not FABRICATIONS. A reducer that believes it succeeded while inventing
	// data reports Complete=true; that class needs its own control (design
	// §8e).
	//
	// A DECLARED skip is NOT incompleteness: a form the parser recognizes and
	// deliberately does not model (CHECK, EXCLUDE, ALTER COLUMN, RENAME,
	// OWNER, ...) is known not to be a key/index/column and leaves
	// Complete=true (ADR 0018's declared subset, made machine-visible).
	//
	// A table's PARTITIONING clause is neither: it is now READ, into
	// Partitioning below. Reading it likewise leaves Complete=true, and for
	// the same reason — a partition key is not a primary key, an index or a
	// column, so even a partitioning form the reducer cannot decompose
	// cannot have hidden one. Partitioning reports its own partial reads
	// internally (Partitioning.Declaration) instead of demoting the table.
	//
	// Zero value is false (fail-closed, design §1-D1b): a table nobody
	// explicitly proved complete is unproven by default, never trustworthy by
	// accident. Every construction site MUST set this explicitly.
	Complete bool

	// Note is WHY, drawn from this package's closed Reason* vocabulary —
	// never provider prose. A provider can only SELECT a reason and QUOTE the
	// user's own source (Unreduced[].Text); it has no channel through which
	// parser-internal diagnostics (a function name, a dispatch branch, a
	// regex) can reach scan output (ADR 0034's measurement/diagnostics
	// boundary). Deduplicated by reason: two drops for the same reason add one
	// Note entry, not two.
	Note string

	// Unreduced is the raw statement text the parser could not reduce, kept
	// VERBATIM with its origin so the agent can read the SOURCE itself — the
	// structural analogue of Body.Text. This is the USER's DDL, never
	// codefit's internals.
	Unreduced []Unreduced
}

// Unreduced is one statement the parser recognized as affecting a table (or,
// on Schema.Unreduced, the schema as a whole) but could not reduce into the
// model, kept verbatim with its source position.
type Unreduced struct {
	Text string
	Pos  Pos
}

// Reason is why a table's structure could not be proven complete. A distinct
// named type (F7, 4R ledger obs #1282, verified — one lens declared the
// closed-vocabulary claim clean and was WRONG): MarkUnproven previously took
// a plain string, and the Reason* values were untyped constants, so the
// "type-level control" ADR 0034 and two doc comments claimed did not exist —
// the ONLY barrier was a doc comment. A provider computing a diagnostic
// string at runtime (a reducer function name, a dispatch branch) could pass
// it straight through with zero compiler friction. Making Reason its own
// type does not make it impossible to violate — an untyped string constant
// (a literal, visible in code review) still converts implicitly — but it
// DOES make it impossible to pass a computed `string` VARIABLE (the actual
// runtime-diagnostic-leak shape ADR 0034 §2.8 forbids) without an explicit,
// reviewable `db.Reason(...)` conversion at the call site.
type Reason string

// Reasons a table's structure could not be proven complete. A CLOSED set,
// defined in this package (never provider-authored prose) — the type-level
// half of the measurement/diagnostics boundary (ADR 0034).
const (
	// ReasonUnreducedTableStatement: a statement affecting this table could
	// not be reduced by the parser.
	ReasonUnreducedTableStatement Reason = "a statement affecting this table could not be reduced"
	// ReasonMalformedTableBody: the table's declaration body could not be
	// parsed at all (e.g. an unbalanced CREATE TABLE(...) body).
	ReasonMalformedTableBody Reason = "the table's declaration body could not be parsed"
	// ReasonTableNeverDeclared: this table entry was materialized by a
	// statement that REFERENCES a table (e.g. ALTER TABLE, CREATE INDEX ...
	// ON) before any CREATE TABLE for that name was ever seen. It has zero
	// genuine structure — no columns were ever read — and an absence-based
	// rule that affirms over it (F4, 4R ledger obs #1282) would be affirming
	// over a table the parser never actually read at all, not merely one it
	// read incompletely.
	ReasonTableNeverDeclared Reason = "no CREATE TABLE statement was ever seen for this table"
	// ReasonPartitionChildInheritsStructure: this table is declared as a
	// PARTITION OF a parent table (PostgreSQL's "CREATE TABLE c PARTITION OF
	// p FOR VALUES ..."). That statement declares the child's PARTITION
	// BOUNDS and nothing else: its columns, primary key, foreign keys and
	// constraints all come from the parent and appear NOWHERE in it. The
	// child is therefore genuinely read (its name and its parent are in
	// Partitioning.Of) but structurally unproven — distinct from
	// ReasonUnreducedTableStatement, where the parser met a statement it
	// could not decompose at all, and from ReasonTableNeverDeclared, where
	// no CREATE TABLE was ever seen. An absence-based rule that affirmed
	// over this table would report "no primary key" about a table whose
	// primary key is declared on its parent.
	ReasonPartitionChildInheritsStructure Reason = "this table is declared as a partition of another table: its columns and keys are inherited from the parent, not declared here"
)

// MarkUnproven records that a statement affecting t could not be reduced. It
// sets Complete=false (fail-closed), appends the verbatim statement to
// Unreduced, and adds reason to Note exactly once (deduplicated by reason —
// architect resolved decision #3). reason is TYPE-CONSTRAINED to this
// package's Reason* constants (F7) — not merely documented as such; text is
// the VERBATIM source statement, never a diagnostic. This is a CORE method
// (ADR 0014), not a reducer-local function, because more than one provider
// needs it (the sqlddl reducer and the Prisma parser) and both must dedupe
// identically.
func (t *Table) MarkUnproven(reason Reason, text string, pos Pos) {
	t.Complete = false
	t.Unreduced = append(t.Unreduced, Unreduced{Text: text, Pos: pos})
	reasonStr := string(reason)
	if !strings.Contains(t.Note, reasonStr) {
		if t.Note != "" {
			t.Note += "; "
		}
		t.Note += reasonStr
	}
}

// StructureProven reports whether every statement affecting t was reduced
// into the model. An absence-based rule (one that concludes "I did not see X,
// therefore X is missing") MUST consult this before concluding — reading
// Complete==false as grounds to abstain (or, for DB-050, to route to a
// surface item instead of affirming), never to emit a deterministic finding.
func (t Table) StructureProven() bool { return t.Complete }

// Column is one field of a table. Type is the neutral classification; RawType is
// the origin type verbatim ("String", "String @db.Text", "Role") so a rule can
// still see what the source actually wrote (e.g. TEXT used as a FK, DB-051). The
// "is this sensitive / encrypted" judgment is the rule's, never a flag here.
type Column struct {
	Name string
	// DBName is the real column name in the database when remapped via @map. EMPTY
	// means "no remap" (fall back to Name) — not defaulted to Name, same rationale
	// as Table.DBName.
	DBName   string
	Pos      Pos
	Type     Type
	RawType  string
	Nullable bool
	List     bool // multivalued / array, e.g. Prisma String[] (DB-002)
}

// Type is the neutral column type. TypeText is distinct from TypeString so a rule
// can tell a TEXT column used as a FK (DB-051) from a normal string. TypeUnknown
// is the honest fallback for a type the parser does not classify.
type Type string

const (
	TypeString   Type = "string"
	TypeText     Type = "text"
	TypeInt      Type = "int"
	TypeFloat    Type = "float"
	TypeBool     Type = "bool"
	TypeDateTime Type = "datetime"
	TypeJSON     Type = "json"
	TypeBytes    Type = "bytes"
	TypeEnum     Type = "enum"
	TypeUnknown  Type = "unknown"
)

// ForeignKey is a relationship from local Columns to RefColumns of RefTable.
// Composite keys are supported. Only explicit relations are modeled; implicit
// many-to-many (no local FK columns) is a declared limit this slice (ADR 0014).
type ForeignKey struct {
	Pos        Pos
	Columns    []string
	RefTable   string
	RefColumns []string
}

// Index is an index over one or more columns. Unique distinguishes a unique
// constraint/index from a plain one. PK is NOT represented as an Index — it is
// Table.PrimaryKey; a rule that treats the PK as an implicit index does so itself
// (deferred consideration, ADR 0014).
type Index struct {
	Pos     Pos
	Columns []string // composite supported (DB-013)
	Unique  bool

	// Method is the index's declared access method/type (PostgreSQL's `USING
	// <method>`, MySQL's `USING BTREE|HASH`, T-SQL's CLUSTERED/NONCLUSTERED/
	// COLUMNSTORE kind, Prisma's `@@index(..., type: ...)`), lowercased at
	// every capture site so one convention holds across dialects. EMPTY means
	// "no access method declared in source" — the same empty-means-none
	// convention as Table.DBName and Trigger.ExecutesFunction — and is
	// deliberately NEVER defaulted to a guessed vocabulary word like "btree":
	// a consumer that needs a default applies its own, this field only
	// reports what the source actually said.
	//
	// A dialect that does not (yet) capture Method for a given statement
	// shape leaves it empty exactly like a dialect that never declared one —
	// the two are indistinguishable through this field alone (architecture/
	// two-classes-of-parser-blindness, "ceguera por omisión"); a provider's
	// own coverage manifest (internal/core/dbcoverage) is the place that
	// states which shapes it does and does not read.
	//
	// Prisma's `type:` vocabulary is NOT codefit-maintained: whatever value
	// the schema declares is captured verbatim (lowercased), never validated
	// against an assumed enum — codefit has not verified Prisma's own
	// documented set of accepted index types against every provider, so it
	// makes no claim here beyond "this is what the source said".
	Method string
}

// Partitioning is what a table's DDL declares about TABLE PARTITIONING —
// PostgreSQL's `PARTITION BY <strategy> (<key>)` and `PARTITION OF <parent>`,
// MySQL's `PARTITION BY RANGE|LIST|HASH|KEY (<key>)`, T-SQL's
// `ON <partition scheme> (<column>)`. It is NOT window-function `PARTITION
// BY`, which is query syntax and never reaches this model.
//
// Every field follows the empty-means-not-declared convention db.Index.Method
// and db.Table.DBName already hold to: a value here was READ FROM THE SOURCE
// (lowercased where the source word is a keyword), never guessed, never
// defaulted.
//
// WHAT A CONSUMER MAY CONCLUDE
//
//   - Declaration != "" — the DDL codefit read declares table partitioning
//     for this table. This is the single authoritative "is it partitioned?"
//     predicate: Strategy, Scheme, Key and Of may ALL be empty for a form the
//     reducer recognized as partitioning but could not decompose, and reading
//     any one of them alone would then answer "no" to a table that said yes.
//   - Declaration == "" — the DDL codefit read declares NO partitioning for
//     this table. That is NOT the same as "this table is not partitioned in
//     the database": a table can be partitioned by a form this parser does
//     not read, by a statement in a file the scan never saw, or by a
//     PostgreSQL `ALTER TABLE ... ATTACH PARTITION` naming it (which lands on
//     the parent's honest-abstention floor, not here). This field reports the
//     SOURCE, not the database.
//   - Of != "" — the source declares this table AS A PARTITION of that
//     parent. A partition child is modelled as ITS OWN TABLE (it is one: it
//     has its own storage, its own indexes, and pg_dump emits most of them as
//     ordinary standalone CREATE TABLEs) AND carries this back-reference. It
//     is deliberately not folded into the parent, which would discard the
//     child's own indexes, and not left parentless, which would make a rule
//     count every monthly child of one partitioned fact table as a separate
//     unpartitioned fact table.
//   - Of == "" does NOT mean "not a child": a child attached by `ALTER TABLE
//     ... ATTACH PARTITION`, or dumped as a standalone CREATE TABLE with no
//     partition grammar of its own (what pg_dump actually emits — Pagila's
//     payment_p2022_01 is exactly this), is indistinguishable here from an
//     ordinary table.
type Partitioning struct {
	// Declaration is the partitioning clause VERBATIM from the user's own
	// DDL (modulo the identifier-quote canonicalization every SQL-DDL
	// statement goes through before reduction) — the structural analogue of
	// Unreduced.Text: parser internals never reach it. NON-EMPTY is the
	// authoritative "this table declares partitioning" answer.
	Declaration string

	// Strategy is the partitioning strategy word as the SOURCE spells it,
	// lowercased: "range", "list", "hash", "key", … EMPTY means the strategy
	// is not in the DDL read — either because the dialect does not state one
	// at the table (T-SQL puts it in the partition FUNCTION) or because the
	// clause was not decomposed. codefit does not maintain a closed
	// vocabulary here: whatever word follows PARTITION BY is captured.
	Strategy string

	// Scheme is the T-SQL partition scheme the table is created ON, name
	// only (normalized like any other identifier in the model). EMPTY on
	// dialects that have no partition schemes. codefit does NOT resolve the
	// scheme's range boundaries — only, when the DDL read also contains the
	// scheme's CREATE PARTITION FUNCTION, its Strategy.
	Scheme string

	// Key is the partition key as a list of plain COLUMN identifiers, in
	// source order. EMPTY when the source key is not a plain column list —
	// most commonly an expression (`PARTITION BY RANGE (YEAR(sold_on))`,
	// `PARTITION BY RANGE (extract(year from d))`). A partition key is never
	// GUESSED into columns: an expression decomposed by a column-list
	// splitter yields an invented column name ("YEAR(sold_on") that exists in
	// no table, which is exactly the fabrication class the completeness
	// contract cannot catch (it covers drops, not fabrications). When Key is
	// empty and Declaration is not, read Declaration.
	Key []string

	// Of names the PARENT table of a partition CHILD (PostgreSQL's `CREATE
	// TABLE c PARTITION OF p ...`), normalized like every other table
	// reference. EMPTY means the source did not declare this table as a
	// child — which is not proof that it is not one (see the type doc).
	Of string
}

// View, Procedure and Trigger complete the OLTP surface. They are DEFINED here so
// the model is format-agnostic, but the Prisma parser leaves them empty; the
// SQL-DDL parser populates them, INCLUDING Body (Phase 2.2, RF-03.6).
type View struct {
	Name string
	Pos  Pos
	Body Body
}

type Procedure struct {
	Name string
	Pos  Pos
	Body Body
}

type Trigger struct {
	Name  string
	Pos   Pos
	Table string
	Body  Body

	// ExecutesFunction is the name of the function/procedure this trigger
	// invokes, when the dialect expresses that as a distinct name in the
	// trigger statement (PostgreSQL: "... EXECUTE FUNCTION|PROCEDURE fn()").
	// EMPTY means the dialect embeds the trigger's logic directly in Body
	// instead (MySQL, T-SQL), or the executed routine's name could not be
	// parsed. This is the trigger→function LINK (Phase 2.2, Unit A2,
	// architecture/pg-trigger-body-link): a PostgreSQL trigger carries no
	// inline body of its own — the logic lives in the named function, which a
	// consumer resolves via Schema.ExecutedProcedure(t), never by re-deriving
	// completeness on the trigger's own (bodyless) statement.
	ExecutesFunction string
}

// ExecutedProcedure resolves t.ExecutesFunction to the Procedure with that
// name in s, or (nil, false) when t names no function (this dialect embeds
// the trigger's logic directly in Body) or no Procedure with that name is
// present in the schema — e.g. a PostgreSQL built-in like
// tsvector_update_trigger, which has no CREATE FUNCTION statement of its own
// and therefore never appears as a Procedure.
//
// Resolution lives HERE, in the neutral model, never in a rule and never in
// a provider: both Trigger and Procedure are neutral model elements, and the
// mapping between them is pure name-based schema data — the binding
// placement of architecture/pg-trigger-body-link (Unit A2).
func (s *Schema) ExecutedProcedure(t Trigger) (*Procedure, bool) {
	if t.ExecutesFunction == "" {
		return nil, false
	}
	for i := range s.Procedures {
		if s.Procedures[i].Name == t.ExecutesFunction {
			return &s.Procedures[i], true
		}
	}
	return nil, false
}

// Body is a routine/view definition as the parser RECOVERED it. Complete=false
// means the captured text may be TRUNCATED — the tokenizer could not prove the
// whole body is here — an honest partial, never a silent one; Note says why.
// This is deliberately a struct, not a plain string: a string cannot express
// "partial", so every consumer would have to trust it blindly. A dbrules rule
// reading Body.Text MUST treat Complete==false as grounds to abstain or
// downgrade to a surface item, never to emit a deterministic finding — ADR
// 0004's "a mutilated rule is worse than an absent one" made mechanical
// instead of aspirational (architecture/tsql-body-truncation-limit).
type Body struct {
	Text     string
	Complete bool
	Note     string
}
