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
	// deliberately does not model (CHECK, EXCLUDE, PARTITION, ALTER COLUMN,
	// RENAME, OWNER, ...) is known not to be a key/index/column and leaves
	// Complete=true (ADR 0018's declared subset, made machine-visible).
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

// Reasons a table's structure could not be proven complete. A CLOSED set,
// defined in this package (never provider-authored prose) — the type-level
// half of the measurement/diagnostics boundary (ADR 0034).
const (
	// ReasonUnreducedTableStatement: a statement affecting this table could
	// not be reduced by the parser.
	ReasonUnreducedTableStatement = "a statement affecting this table could not be reduced"
	// ReasonMalformedTableBody: the table's declaration body could not be
	// parsed at all (e.g. an unbalanced CREATE TABLE(...) body).
	ReasonMalformedTableBody = "the table's declaration body could not be parsed"
	// ReasonTableNeverDeclared: this table entry was materialized by a
	// statement that REFERENCES a table (e.g. ALTER TABLE, CREATE INDEX ...
	// ON) before any CREATE TABLE for that name was ever seen. It has zero
	// genuine structure — no columns were ever read — and an absence-based
	// rule that affirms over it (F4, 4R ledger obs #1282) would be affirming
	// over a table the parser never actually read at all, not merely one it
	// read incompletely.
	ReasonTableNeverDeclared = "no CREATE TABLE statement was ever seen for this table"
)

// MarkUnproven records that a statement affecting t could not be reduced. It
// sets Complete=false (fail-closed), appends the verbatim statement to
// Unreduced, and adds reason to Note exactly once (deduplicated by reason —
// architect resolved decision #3). reason MUST be one of this package's
// Reason* constants; text is the VERBATIM source statement, never a
// diagnostic. This is a CORE method (ADR 0014), not a reducer-local function,
// because more than one provider needs it (the sqlddl reducer and the Prisma
// parser) and both must dedupe identically.
func (t *Table) MarkUnproven(reason, text string, pos Pos) {
	t.Complete = false
	t.Unreduced = append(t.Unreduced, Unreduced{Text: text, Pos: pos})
	if !strings.Contains(t.Note, reason) {
		if t.Note != "" {
			t.Note += "; "
		}
		t.Note += reason
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
