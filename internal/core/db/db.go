package db

// Schema is the parsed structure of a database. It models the whole OLTP surface
// Phase 2 audits; a given parser fills the subset its format expresses (a Prisma
// parse leaves Views/Procedures/Triggers empty — slice 3's SQL-DDL parser fills
// them).
type Schema struct {
	Tables     []Table
	Views      []View      // OLTP surface; not filled by the Prisma parser
	Procedures []Procedure // idem
	Triggers   []Trigger   // idem
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
}

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
