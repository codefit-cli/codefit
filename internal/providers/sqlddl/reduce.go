package sqlddl

import (
	"regexp"
	"strings"

	"github.com/codefit-cli/codefit/internal/core/db"
)

// builder accumulates the mutable schema state as statements are applied in
// order. It is dialect-free CODE (design §1): the dialect only supplies
// type/modifier VOCABULARY (dialect.TypeMap / dialect.Modifiers) — quoting was
// already canonicalized by split() before apply() ever sees a statement.
type builder struct {
	order     []string
	tables    map[string]*db.Table
	views     []db.View
	procs     []db.Procedure
	trigs     []db.Trigger
	seenIndex map[string]bool
	dialect   *Dialect

	// unreduced accumulates statements recognized as table-affecting (an
	// "alter table" head) whose table name could not be resolved — design §2,
	// drop site 2. It gates nothing per-table; it surfaces only in the
	// per-scan completeness inventory (sensors/db.Result.Note).
	unreduced []db.Unreduced
}

func newBuilder(dialect *Dialect) *builder {
	return &builder{tables: map[string]*db.Table{}, seenIndex: map[string]bool{}, dialect: dialect}
}

func (b *builder) schema() *db.Schema {
	s := &db.Schema{Views: b.views, Procedures: b.procs, Triggers: b.trigs, Unreduced: b.unreduced}
	for _, name := range b.order {
		if t := b.tables[name]; t != nil {
			s.Tables = append(s.Tables, *t)
		}
	}
	return s
}

var (
	reCreateTable = regexp.MustCompile(`(?is)^create\s+table\s+(if\s+not\s+exists\s+)?("?[\w".]+"?)\s*\(`)
	reAlterTable  = regexp.MustCompile(`(?is)^alter\s+table\s+(?:if\s+exists\s+)?(?:only\s+)?("?[\w".]+"?)\s+(.*)$`)
	reCreateIndex = regexp.MustCompile(`(?is)^create\s+(unique\s+)?index\s+(?:concurrently\s+)?(if\s+not\s+exists\s+)?("?[\w"]+"?)\s+on\s+("?[\w".]+"?)\s*(?:using\s+\w+\s*)?\(([^)]*)\)`)
	reView        = regexp.MustCompile(`(?is)^create\s+(?:or\s+replace\s+)?(?:materialized\s+)?view\s+(?:if\s+not\s+exists\s+)?("?[\w".]+"?)`)
	reRoutine     = regexp.MustCompile(`(?is)^create\s+(?:or\s+replace\s+)?(?:function|procedure)\s+("?[\w".]+"?)`)
	reTrigger     = regexp.MustCompile(`(?is)^create\s+(?:or\s+replace\s+)?(?:constraint\s+)?trigger\s+("?[\w".]+"?)\b.*?\son\s+("?[\w".]+"?)`)
	reDropTable   = regexp.MustCompile(`(?is)^drop\s+table\s+(?:if\s+exists\s+)?("?[\w".]+"?)`)
	reReferences  = regexp.MustCompile(`(?is)references\s+("?[\w".]+"?)\s*(?:\(([^)]*)\))?`)

	// reIndexShapedHead recognizes a CREATE INDEX-family statement head
	// BROADER than reCreateIndex — including forms reCreateIndex's own
	// grammar does not cover (an anonymous PostgreSQL index with no index
	// name, an "ON ONLY" partitioned-table index, T-SQL's
	// CLUSTERED/NONCLUSTERED/COLUMNSTORE index kinds, and the standalone
	// FULLTEXT/SPATIAL/XML/PRIMARY XML CREATE INDEX statement forms — this
	// package already treats FULLTEXT/SPATIAL as recognized index vocabulary
	// for the INLINE and ALTER...ADD shorthand forms
	// (isInlineKeyIndexForm/isAddKeyIndexForm), so leaving the standalone
	// CREATE form out here would be an internal inconsistency, not a new
	// dialect gap, REL-001) — so apply()'s default: branch can tell "this
	// dispatch genuinely has no branch for this INDEX form" apart from a
	// statement that is out of the declared subset entirely (INSERT, GRANT,
	// COMMENT, CREATE TYPE, ...), which must stay silent (ADR 0034 SS2.4;
	// TestSQLDDL_OutOfSubsetStatement_RecordsNothing locks that boundary).
	reIndexShapedHead = regexp.MustCompile(`(?is)^create\s+(?:unique\s+)?(?:clustered\s+|nonclustered\s+)?` +
		`(?:columnstore\s+|fulltext\s+|spatial\s+|primary\s+xml\s+|xml\s+)?index\b`)

	// reIndexShapedTarget extracts the target table from a CREATE
	// INDEX-shaped statement default() could not fully parse — the
	// identifier following its ON (or ON ONLY, PostgreSQL's
	// partitioned-table syntax) clause. Same table-identifier character
	// class as reCreateIndex's own capture group 4, for consistency. Narrow
	// on purpose: used ONLY to ATTRIBUTE an already-confirmed unrecognized
	// index drop to a table (design: "a wrong attribution is worse than
	// none"), never to reduce the statement itself.
	reIndexShapedTarget = regexp.MustCompile(`(?is)\bon\s+(?:only\s+)?("?[\w".]+"?)`)

	// reTriggerExecutes matches the PostgreSQL "EXECUTE FUNCTION|PROCEDURE
	// fn(...)" clause of a CREATE TRIGGER statement — the trigger→function
	// LINK (Phase 2.2, Unit A2, architecture/pg-trigger-body-link). PG has no
	// inline trigger body, but the executed function's NAME is always present
	// in the statement text, letting a consumer follow the link to where the
	// logic really lives (Schema.ExecutedProcedure). MySQL/T-SQL triggers
	// embed their logic directly and have no EXECUTE FUNCTION/PROCEDURE
	// clause in their grammar, so this never matches there —
	// Trigger.ExecutesFunction correctly stays empty on those dialects.
	reTriggerExecutes = regexp.MustCompile(`(?is)\bexecute\s+(?:function|procedure)\s+("?[\w".]+"?)\s*\(`)
)

// isRoutineHead reports whether accumulated statement text is a CREATE
// FUNCTION/PROCEDURE/TRIGGER head. Shared with split() ON PURPOSE (intentional
// seam): split() consults it — only for a dialect with
// RoutineBodyEndsAtBatchSeparator — to decide whether an internal ';' is a body
// separator (suppress) or the statement terminator (flush). Head-recognition,
// NOT block counting: it never inspects BEGIN/END. Do NOT "clean up" this seam
// by moving these regexes out of reach — the T-SQL de-truncation depends on it
// (ADR 0027).
func isRoutineHead(text string) bool {
	return reRoutine.MatchString(text) || reTrigger.MatchString(text)
}

// apply classifies one statement and mutates the schema. Anything outside the
// declared subset (INSERT/UPDATE/DO/GRANT/COMMENT/CREATE TYPE/…) is skipped.
//
// Unit I rework: this used to guard T-SQL GO-batched routine/trigger bodies
// with an inRoutineBody flag that matched BEGIN/END as raw text. That guard
// was insound (matched BEGIN/END inside string literals, was not
// depth-counted so nested BEGIN...END closed it early, and was never reset
// between files — a stuck-open guard from one file's unterminated body could
// swallow a LATER file's real tables). It has been removed. The documented
// consequence: a T-SQL GO-batched procedure/trigger body containing a
// CREATE TABLE-shaped fragment may now surface as a spurious top-level
// table — a disclosed, rare limit (see docs/decisions and the sqlddl package
// doc), not silent corruption. MySQL routine bodies wrapped by "DELIMITER
// //" ... "DELIMITER ;" remain correctly handled: split()'s DELIMITER
// tracking keeps the whole body as ONE statement, captured by the
// reRoutine/reTrigger head regex directly, so no body fragment is ever
// reduced as its own statement in that case.
func (b *builder) apply(file string, st stmt) {
	pos := db.Pos{File: file, Line: st.line}
	head := strings.ToLower(strings.TrimSpace(st.text))
	switch {
	case reCreateTable.MatchString(st.text):
		b.applyCreateTable(file, st)
	case strings.HasPrefix(head, "alter table"):
		b.applyAlterTable(file, st)
	case reCreateIndex.MatchString(st.text):
		b.applyCreateIndex(file, st)
	case reView.MatchString(st.text):
		b.views = append(b.views, db.View{Name: normalizeName(reView.FindStringSubmatch(st.text)[1]), Pos: pos, Body: viewBody(st)})
	case reRoutine.MatchString(st.text):
		b.procs = append(b.procs, db.Procedure{Name: routineName(reRoutine.FindStringSubmatch(st.text)[1]), Pos: pos, Body: routineBody(st)})
	case reTrigger.MatchString(st.text):
		m := reTrigger.FindStringSubmatch(st.text)
		trig := db.Trigger{Name: normalizeName(m[1]), Pos: pos, Table: normalizeName(m[2]), Body: b.triggerBody(st)}
		if fm := reTriggerExecutes.FindStringSubmatch(st.text); fm != nil {
			trig.ExecutesFunction = normalizeName(fm[1])
		}
		b.trigs = append(b.trigs, trig)
	case reDropTable.MatchString(st.text):
		b.dropTable(normalizeName(reDropTable.FindStringSubmatch(st.text)[1]))
	case reIndexShapedHead.MatchString(st.text):
		// A genuinely UNRECOGNIZED CREATE INDEX form: it announces itself as
		// a CREATE INDEX statement but reCreateIndex's own grammar has no
		// branch for it (an anonymous index, ON ONLY, or a
		// CLUSTERED/NONCLUSTERED/COLUMNSTORE kind). Per ADR 0034 SS2.4, this
		// is NOT a declared skip: the dispatch genuinely does not know
		// whether it declares an index, so it must mark the table unproven
		// instead of vanishing silently.
		if tm := reIndexShapedTarget.FindStringSubmatch(st.text); tm != nil {
			t, created := b.getTable(normalizeName(tm[1]), pos)
			if created {
				// F4 pattern (4R ledger obs #1282), same disposition as
				// applyAlterTable/applyCreateIndex above (REL-002, 4R
				// reliability lens): this is the FIRST time this table name
				// was ever seen — no CREATE TABLE declared it. The accurate
				// claim reaching the agent through routeUnprovenTable is "no
				// CREATE TABLE was ever seen for this table", not "a
				// statement affecting this table could not be reduced".
				t.MarkUnproven(db.ReasonTableNeverDeclared, st.text, pos)
			} else {
				t.MarkUnproven(db.ReasonUnreducedTableStatement, st.text, pos)
			}
		} else {
			// No attributable table (a wrong attribution is worse than
			// none, design §2) — recorded at schema level; gates nothing
			// per-table.
			b.unreduced = append(b.unreduced, db.Unreduced{Text: st.text, Pos: pos})
		}
	default:
		// out of the declared subset (INSERT/UPDATE/DO/GRANT/COMMENT/CREATE
		// TYPE/…) — skipped on purpose. These statement KINDS are never
		// table-structure-affecting at all, so recording them would be
		// noise, not honesty (ADR 0034 SS2.4;
		// TestSQLDDL_OutOfSubsetStatement_RecordsNothing locks this).
	}
}

// viewBody builds a View's Body. A CREATE VIEW definition is a single SELECT
// statement and cannot legally contain a top-level ';' in any of the three
// supported dialects — there is nothing to truncate, so it is unconditionally
// Complete (spec RF-03.6 §4: "view bodies are unaffected by the T-SQL
// partial-capture limit").
func viewBody(st stmt) db.Body {
	return db.Body{Text: st.text, Complete: true}
}

// routineBody builds a Procedure/Trigger's Body and derives Complete from
// TOKENIZER STATE ONLY (architecture/tsql-body-truncation-limit, binding
// condition 1) — the terminator kind that flushed the statement and whether a
// dollar-quoted block was consumed while scanning it (both exposed by
// split.go's stmt, computed there for free, never re-derived here).
//
// It is DELIBERATELY FORBIDDEN to derive completeness by re-scanning the body
// text for BEGIN/END. That is exactly the guard ADR 0022 REMOVED from this
// package (design §67-77 / apply()'s own doc comment above): it matched
// BEGIN/END inside string literals, was not depth-counted, and was never
// reset between files — an unsound guard one level below this one would
// reintroduce precisely that failure mode here, on the very flag this
// package's own history proved cannot be gotten right that way.
//
// The rule, conservative by design (a false "partial" only downgrades a rule
// to surface — safe; a false "complete" would fabricate a wrong affirmation
// — unsafe):
//
//	Complete := (st.term != termSemicolon) || st.quotedBlockSeen
//
// A dollar-quoted block (PostgreSQL) swallows every internal ';' inside it,
// so a body that contains one is trustworthy even though the OUTER statement
// still ends on an ordinary ';' (the real one, after the $$ block closed). A
// MySQL DELIMITER-wrapped body never sees termSemicolon at all (its
// terminator is the active custom delimiter). T-SQL has neither mechanism: a
// multi-statement BEGIN...END body is cut at the FIRST internal ';', which is
// termSemicolon with no quoted block seen — Complete=false, Note explains why.
// A single-statement body with NO internal ';' at all flushes at GO/EOF
// (termGoBreak/termEOF) — Complete=true, nothing was cut.
func routineBody(st stmt) db.Body {
	complete := st.term != termSemicolon || st.quotedBlockSeen
	b := db.Body{Text: st.text, Complete: complete}
	if !complete {
		b.Note = "body may be truncated: this dialect has no statement-separator " +
			"escape in effect here (no dollar-quoting, no active DELIMITER), so " +
			"the tokenizer stopped at the body's first internal ';' — only the " +
			"text up to and including that ';' was captured; any statements " +
			"after it are not represented"
	}
	return b
}

// triggerBody builds a Trigger's Body, consulting the per-dialect DATUM
// dialect.TriggerHasInlineBody (Phase 2.2, Unit A2,
// architecture/pg-trigger-body-link) instead of branching on dialect.Name —
// the same DATA-not-code architecture ADR 0022 already established for every
// other per-dialect fact in this package.
//
// PostgreSQL triggers carry NO inline body at all: "CREATE TRIGGER x ... FOR
// EACH ROW EXECUTE FUNCTION fn();" is a WIRE from an event to a function, not
// a body — the logic lives in fn(), captured separately as a Procedure with
// its own (independently derived) Body. Applying routineBody's "Complete :=
// term != termSemicolon || quotedBlockSeen" formula to a PG trigger produces
// a FALSE incomplete: the statement was never truncated, it simply had
// nothing to truncate. This was discovered against the real Pagila fixture
// during Unit A (see apply-progress) and is what Unit A2 repairs: Condition 1
// of architecture/pg-trigger-body-link — "the Complete flag must TELL THE
// TRUTH" — is non-negotiable and binds here.
//
// TriggerHasInlineBody=false short-circuits straight to Complete=true with an
// explanatory Note pointing at Trigger.ExecutesFunction. TriggerHasInlineBody
// =true (MySQL, T-SQL) falls through to the UNCHANGED routineBody derivation
// — this is the regression lock: MySQL-no-DELIMITER and T-SQL multi-statement
// triggers keep their existing Complete=false behavior exactly as before this
// unit.
func (b *builder) triggerBody(st stmt) db.Body {
	if !b.dialect.TriggerHasInlineBody {
		return db.Body{
			Text:     st.text,
			Complete: true,
			Note: "this dialect's triggers carry no inline body — the statement " +
				"only wires an event to a function/procedure; see " +
				"Trigger.ExecutesFunction for the executed routine, whose own " +
				"Body carries the logic",
		}
	}
	return routineBody(st)
}

// getTable returns the named table, creating it if this is the first
// statement to reference it, and reports whether it was JUST created (true)
// or already existed (false) — callers that can reference a table BEFORE any
// CREATE TABLE for it (applyAlterTable, applyCreateIndex) use this to detect
// a PHANTOM creation (F4, 4R ledger obs #1282) and record it; applyCreateTable
// itself ignores the bool, since a table it creates is by definition
// genuinely declared. pos anchors a NEWLY created table only — an
// already-registered table keeps whatever position its FIRST reference gave
// it (F1, 4R risk/reliability/resilience lens, corroborated + verified: this
// reducer used to never set Table.Pos at all, so every unproven table's
// routed surface item anchored on file:line "":0 — identical for every
// unproven table in a project, collapsing their baseline fingerprints into
// one. Matches the Prisma provider's own construction site
// (prismaschema.go:145), which has always set Pos).
func (b *builder) getTable(name string, pos db.Pos) (*db.Table, bool) {
	t := b.tables[name]
	created := t == nil
	if created {
		// Complete starts true (N1, design §1-D1b): db.Table.Complete's zero
		// value is false (fail-closed), so every construction site must set
		// it explicitly or the whole DB dimension mutes itself on this
		// provider. A table is demoted to Complete=false only when a later
		// statement affecting it cannot be reduced (MarkUnproven). This
		// default is UNCHANGED by F4 — F4's fix is applied selectively, at
		// the two call sites that can create a table this way, never by
		// flipping this default (that IS the N1 trap and would mute the
		// whole dimension).
		t = &db.Table{Name: name, Pos: pos, Complete: true}
		b.tables[name] = t
		b.order = append(b.order, name)
	}
	return t, created
}

func (b *builder) dropTable(name string) {
	if _, ok := b.tables[name]; !ok {
		return
	}
	delete(b.tables, name)
	kept := b.order[:0]
	for _, n := range b.order {
		if n != name {
			kept = append(kept, n)
		}
	}
	b.order = kept
}

func (b *builder) applyCreateTable(file string, st stmt) {
	loc := reCreateTable.FindStringSubmatchIndex(st.text)
	name := normalizeName(st.text[loc[4]:loc[5]])
	ifNotExists := loc[2] != -1
	if existing, exists := b.tables[name]; exists {
		// AJUSTE 1: an existing table + explicit IF NOT EXISTS → skip the
		// WHOLE statement silently. The first CREATE wins; no Frankenstein
		// merge. This IS a declared, recognized skip (ADR 0018): the SQL
		// itself says "only create if absent", so discarding the second
		// declaration is genuinely safe.
		//
		// F3 (4R ledger, obs #1282): WITHOUT "IF NOT EXISTS", a second
		// CREATE TABLE for the same normalized name is NOT a declared skip —
		// it is a genuine anomaly (most commonly normalizeName stripping a
		// schema qualifier so two DIFFERENT tables, e.g. public.users and
		// audit.users, collapse into one name) whose real columns/
		// constraints are silently discarded. That is unproven structure,
		// recorded on the SURVIVING table so an absence-based rule does not
		// affirm over data it never actually saw completely.
		if !ifNotExists {
			existing.MarkUnproven(db.ReasonUnreducedTableStatement, st.text, db.Pos{File: file, Line: st.line})
		}
		return
	}
	openIdx := loc[1] - 1 // the '(' the regex ended on
	inner, innerStart, ok := balancedParen(st.text, openIdx)
	if !ok {
		// malformed body: still register the table, no columns, and record
		// the drop (D2 site 3, design §2) — the parser could not read this
		// table's constraint set at all.
		t, _ := b.getTable(name, db.Pos{File: file, Line: st.line})
		t.MarkUnproven(db.ReasonMalformedTableBody, st.text, db.Pos{File: file, Line: st.line})
		return
	}
	t, _ := b.getTable(name, db.Pos{File: file, Line: st.line})
	for _, p := range splitTopLevelParts(inner) {
		line := st.line + strings.Count(st.text[:innerStart+p.off], "\n")
		b.applyTableItem(t, p.text, db.Pos{File: file, Line: line})
	}
}

// applyTableItem parses one comma-separated item of a CREATE TABLE body: a table
// constraint or a column definition.
func (b *builder) applyTableItem(t *db.Table, item string, pos db.Pos) {
	item = strings.TrimSpace(item)
	up := strings.ToUpper(item)
	kw := leadingKeyword(item)
	switch {
	case strings.HasPrefix(up, "CONSTRAINT "):
		// strip "CONSTRAINT <name>" then treat the rest as a table constraint
		rest := strings.TrimSpace(item[len("CONSTRAINT "):])
		if sp := strings.IndexAny(rest, " \t\n("); sp >= 0 {
			rest = strings.TrimSpace(rest[sp:])
		}
		b.applyTableConstraint(t, rest, pos)
	case strings.HasPrefix(up, "PRIMARY KEY"), strings.HasPrefix(up, "UNIQUE"),
		strings.HasPrefix(up, "FOREIGN KEY"), strings.HasPrefix(up, "CHECK"),
		strings.HasPrefix(up, "EXCLUDE"), strings.HasPrefix(up, "PARTITION"):
		b.applyTableConstraint(t, item, pos)
	case (kw == "KEY" || kw == "INDEX" || kw == "FULLTEXT" || kw == "SPATIAL") && b.isInlineKeyIndexForm(item):
		// MySQL inline secondary-index shorthand: KEY name (cols), INDEX name
		// (cols), FULLTEXT KEY/INDEX (cols), SPATIAL KEY/INDEX (cols) — a
		// table CONSTRAINT/index line, never a column (Unit I, task I4b: this
		// previously mis-parsed as a phantom column literally named
		// "KEY"/"INDEX" and silently dropped the index). Unit I rework re-judge
		// fix: routing on a bare '(' anywhere in the item (hasParenColumnList)
		// was ALSO wrong — a column legitimately named key/index/fulltext/spatial
		// whose TYPE itself carries parens (`key varchar(255)`, `index int(11)`,
		// `key numeric(10,2)`, `key enum('a','b')`) has a '(' from the TYPE, not
		// from an index column list, and was misclassified as the index FORM —
		// dropping the column and fabricating a garbage index. The correct
		// discriminator (isInlineKeyIndexForm) looks at the TOKEN right after the
		// leading keyword: if its type-base is a known type in the dialect's
		// TypeMap, this is a column of that type, not an index. A real column
		// named `key`/`index` (backtick- or bracket-quoted, MySQL/T-SQL reserved
		// words) is unambiguous: quoting is canonicalized to a leading '"' by
		// split() before reduce.go ever sees it, so it never matches this bare,
		// unquoted leadingKeyword check either way.
		b.applyTableConstraint(t, item, pos)
	default:
		b.applyColumn(t, item, pos)
	}
}

// isInlineKeyIndexForm reports whether item — already known to start with the
// bare, unquoted leading keyword KEY/INDEX/FULLTEXT/SPATIAL — is MySQL's
// inline secondary-index FORM (KEY name (cols), INDEX (cols), ...) rather than
// a plain column definition whose name happens to be that reserved word (e.g.
// "key varchar(255)", legal unquoted in PostgreSQL/MySQL). The discriminator
// is dialect-data-driven, NOT paren-presence: a column's TYPE may itself
// carry parens (length/precision/enum literals), so "does item contain a '('"
// is insufficient (re-judge fix). Instead, inspect the token immediately
// after the leading keyword: if its type-base (paren-stripped via typeBase)
// is a known type in the dialect's own TypeMap, this is a column of that
// type; otherwise (an index name, or '(' directly — the unnamed KEY (cols)
// form) it is the inline index FORM. Consults TypeMap DATA only — no
// dialect-name branch.
func (b *builder) isInlineKeyIndexForm(item string) bool {
	kw := leadingKeyword(item)
	rest := strings.TrimSpace(item[len(kw):])
	tok, _ := firstToken(rest)
	base := typeBase(tok)
	if base == "" {
		return true // e.g. "KEY (cols)" — no type-like token at all
	}
	_, isType := b.dialect.TypeMap[strings.ToLower(base)]
	return !isType
}

// isAddKeyIndexForm reports whether an ALTER TABLE action is the "ADD
// KEY|INDEX|FULLTEXT KEY|SPATIAL KEY ... (cols)" secondary-index shorthand
// rather than a column named KEY/INDEX/FULLTEXT/SPATIAL being added (MySQL
// allows omitting COLUMN, e.g. "ADD key varchar(255)"). Same TypeMap-driven
// discriminator as isInlineKeyIndexForm.
func (b *builder) isAddKeyIndexForm(act string) bool {
	up := strings.ToUpper(act)
	if !strings.HasPrefix(up, "ADD ") {
		return false
	}
	rest := act[len("ADD "):]
	kw := leadingKeyword(rest)
	if kw != "KEY" && kw != "INDEX" && kw != "FULLTEXT" && kw != "SPATIAL" {
		return false
	}
	return b.isInlineKeyIndexForm(strings.TrimSpace(rest))
}

// leadingKeyword extracts the first table-item keyword: the run of
// non-whitespace, non-'(' bytes at the start of s, uppercased. It is used to
// distinguish MySQL's inline KEY/INDEX secondary-index shorthand — always
// unquoted — from a real (quoted) column whose name happens to start with the
// same letters (e.g. a column literally named "keyword" is NOT "KEY").
func leadingKeyword(s string) string {
	s = strings.TrimSpace(s)
	i := 0
	for i < len(s) {
		c := s[i]
		if c == ' ' || c == '\t' || c == '\n' || c == '(' {
			break
		}
		i++
	}
	return strings.ToUpper(s[:i])
}

func (b *builder) applyTableConstraint(t *db.Table, c string, pos db.Pos) {
	up := strings.ToUpper(strings.TrimSpace(c))
	kw := leadingKeyword(c)
	switch {
	case strings.HasPrefix(up, "PRIMARY KEY"):
		t.PrimaryKey = parenCols(c)
	case strings.HasPrefix(up, "UNIQUE"):
		t.Indexes = append(t.Indexes, db.Index{Pos: pos, Columns: parenCols(c), Unique: true})
	case strings.HasPrefix(up, "FOREIGN KEY"):
		if fk, ok := parseForeignKey(c, pos); ok {
			t.ForeignKeys = append(t.ForeignKeys, fk)
		}
	case kw == "KEY" || kw == "INDEX" || kw == "FULLTEXT" || kw == "SPATIAL":
		// MySQL inline KEY/INDEX/FULLTEXT KEY/SPATIAL KEY shorthand (task
		// I4b) — recorded as a plain (non-unique) index by its base columns.
		t.Indexes = append(t.Indexes, db.Index{Pos: pos, Columns: parenCols(c), Unique: false})
	case kw == "CHECK" || kw == "EXCLUDE" || kw == "PARTITION":
		// Declared, RECOGNIZED skips (ADR 0018) — known not to be a
		// key/index/column, so this is NOT incompleteness. Recording these
		// would mute DB-050 across virtually every real PostgreSQL schema
		// (N2, design §2/§3c) — CHECK constraints are commonplace.
	default:
		// A genuinely UNRECOGNIZED table constraint form — the dispatch does
		// not know what this declares, so it cannot rule out a key/index (D2
		// site 4, design §2).
		t.MarkUnproven(db.ReasonUnreducedTableStatement, c, pos)
	}
}

func (b *builder) applyColumn(t *db.Table, def string, pos db.Pos) {
	def = strings.TrimSpace(def)
	name, rest := firstToken(def)
	if name == "" {
		return
	}
	rawType, mods := splitTypeAndMods(rest, b.dialect.Modifiers)
	col := db.Column{
		Name:    normalizeName(name),
		DBName:  "",
		Pos:     pos,
		RawType: strings.TrimSpace(rawType),
		Type:    b.dialect.mapType(typeBase(rawType)),
		List:    strings.Contains(rawType, "[]"),
	}
	upMods := strings.ToUpper(mods)
	col.Nullable = !strings.Contains(upMods, "NOT NULL") && !strings.Contains(upMods, "PRIMARY KEY")
	t.Columns = append(t.Columns, col)

	if strings.Contains(upMods, "PRIMARY KEY") {
		t.PrimaryKey = append(t.PrimaryKey, col.Name)
	}
	if containsWord(upMods, "UNIQUE") {
		t.Indexes = append(t.Indexes, db.Index{Pos: pos, Columns: []string{col.Name}, Unique: true})
	}
	if m := reReferences.FindStringSubmatch(mods); m != nil {
		t.ForeignKeys = append(t.ForeignKeys, db.ForeignKey{
			Pos: pos, Columns: []string{col.Name}, RefTable: normalizeName(m[1]), RefColumns: splitIdents(m[2]),
		})
	}
}

func (b *builder) applyAlterTable(file string, st stmt) {
	m := reAlterTable.FindStringSubmatch(st.text)
	if m == nil {
		// The statement announced itself as "alter table" (apply's own
		// dispatch matched it) but reAlterTable itself failed to parse the
		// table name — a genuine unknown, unlike a declared out-of-subset
		// statement. It cannot be attributed to any table, so it is recorded
		// on the SCHEMA (D2 site 2, design §2): it gates nothing per-table,
		// it only feeds the scan-level completeness inventory.
		b.unreduced = append(b.unreduced, db.Unreduced{Text: st.text, Pos: db.Pos{File: file, Line: st.line}})
		return
	}
	t, created := b.getTable(normalizeName(m[1]), db.Pos{File: file, Line: st.line})
	if created {
		// F4 (4R ledger obs #1282, "the false affirmation survives, path
		// 2"): this ALTER TABLE is the FIRST time this name was ever seen —
		// no CREATE TABLE declared it. Recording it here, not by defaulting
		// Complete to false (the N1 trap), keeps every genuinely-declared
		// table unaffected while stopping DB-050 from affirming over a
		// table it never actually read.
		t.MarkUnproven(db.ReasonTableNeverDeclared, st.text, db.Pos{File: file, Line: st.line})
	}
	// offset of the action group within the statement, for per-action line numbers
	actOff := strings.Index(st.text, m[2])
	for _, p := range splitTopLevelParts(m[2]) {
		line := st.line + strings.Count(st.text[:actOff+p.off], "\n")
		b.applyAlterAction(t, strings.TrimSpace(p.text), db.Pos{File: file, Line: line})
	}
}

// alterActionRecognizedSkips are the ALTER TABLE action heads this reducer
// RECOGNIZES and deliberately does not model (ADR 0018's declared subset,
// made machine-visible, design §2). Recording these as unproven would mute
// DB-050 across virtually every real PostgreSQL dump (N2) — none of them can
// declare a key, index or column. Values are the leading token(s), matched
// case-insensitively against act's own prefix.
var alterActionRecognizedSkips = []string{
	"ALTER COLUMN", "ALTER ", "RENAME", "OWNER", "ENABLE", "DISABLE",
	"CLUSTER", "SET ", "RESET ", "VALIDATE", "NO ",
}

func isAlterActionRecognizedSkip(up string) bool {
	for _, prefix := range alterActionRecognizedSkips {
		if strings.HasPrefix(up, prefix) {
			return true
		}
	}
	return false
}

func (b *builder) applyAlterAction(t *db.Table, act string, pos db.Pos) {
	up := strings.ToUpper(act)
	switch {
	case strings.HasPrefix(up, "ADD CONSTRAINT"):
		rest := strings.TrimSpace(act[len("ADD CONSTRAINT"):])
		if sp := strings.IndexAny(rest, " \t\n("); sp >= 0 {
			rest = strings.TrimSpace(rest[sp:])
		}
		b.applyTableConstraint(t, rest, pos)
	case strings.HasPrefix(up, "ADD PRIMARY KEY"), strings.HasPrefix(up, "ADD UNIQUE"), strings.HasPrefix(up, "ADD FOREIGN KEY"):
		b.applyTableConstraint(t, strings.TrimSpace(act[len("ADD"):]), pos)
	case b.isAddKeyIndexForm(act):
		// ADD KEY idx (cols) / ADD INDEX idx (cols) / ADD FULLTEXT KEY (cols) /
		// ADD SPATIAL KEY (cols) — MySQL's secondary-index shorthand via
		// ALTER TABLE. Unit I rework (C2 MINOR): the generic "ADD " column
		// branch below previously turned this into a phantom column literally
		// named "KEY". Same parenthesized-column-list discriminator as
		// applyTableItem's inline case.
		b.applyTableConstraint(t, strings.TrimSpace(act[len("ADD"):]), pos)
	case strings.HasPrefix(up, "ADD COLUMN"), strings.HasPrefix(up, "ADD "):
		rest := strings.TrimSpace(act[len("ADD"):])
		// R1 disposition (design §8c, spec "R1 — Fabrication Hypothesis",
		// CONFIRMED): a non-single-space ADD/CONSTRAINT (e.g. "ADD  CONSTRAINT")
		// still lands here because "ADD " only needs ONE space to match. Before
		// treating the remainder as a column, check whether it is actually a
		// constraint form the dispatch above MISSED (its own leading keyword
		// says so) — narrowing the fabrication at its source converts it into a
		// recorded drop, which the completeness contract then covers, instead
		// of inventing a phantom column/key literally named "CONSTRAINT".
		if kw := leadingKeyword(rest); kw == "CONSTRAINT" || kw == "PRIMARY" || kw == "UNIQUE" || kw == "FOREIGN" || kw == "CHECK" {
			t.MarkUnproven(db.ReasonUnreducedTableStatement, act, pos)
			return
		}
		rest = trimPrefixFold(rest, "COLUMN")
		rest = trimPrefixFold(rest, "IF NOT EXISTS")
		name, _ := firstToken(rest)
		if hasColumn(t, normalizeName(name)) {
			return // idempotent add
		}
		b.applyColumn(t, rest, pos)
	case strings.HasPrefix(up, "DROP COLUMN"), strings.HasPrefix(up, "DROP "):
		rest := strings.TrimSpace(act[len("DROP"):])
		rest = trimPrefixFold(rest, "COLUMN")
		rest = trimPrefixFold(rest, "IF EXISTS")
		name, _ := firstToken(rest)
		dropColumn(t, normalizeName(name))
	case isAlterActionRecognizedSkip(up):
		// ALTER COLUMN / RENAME / OWNER / ENABLE / DISABLE / CLUSTER / SET /
		// RESET / VALIDATE / NO … — declared, RECOGNIZED skips (N2): known
		// not to declare a key/index/column, so this is NOT incompleteness.
	default:
		// A genuinely UNRECOGNIZED alter action — the dispatch does not know
		// what this declares, so it cannot rule out a key/index (D2 site 5,
		// design §2). This is the dominant chokepoint: AdventureWorksDW's
		// WITH CHECK ADD / newline-ADD / comma-chained CONSTRAINT shapes all
		// land here.
		t.MarkUnproven(db.ReasonUnreducedTableStatement, act, pos)
	}
}

func (b *builder) applyCreateIndex(file string, st stmt) {
	m := reCreateIndex.FindStringSubmatch(st.text)
	if m == nil {
		return
	}
	unique := m[1] != ""
	name := normalizeName(m[3])
	if m[2] != "" && b.seenIndex[name] { // IF NOT EXISTS + already created
		return
	}
	b.seenIndex[name] = true
	t, created := b.getTable(normalizeName(m[4]), db.Pos{File: file, Line: st.line})
	if created {
		// F4 (4R ledger obs #1282): CREATE INDEX ... ON x is the FIRST time
		// x was ever seen — no CREATE TABLE declared it. Same disposition as
		// applyAlterTable's phantom-creation case above.
		t.MarkUnproven(db.ReasonTableNeverDeclared, st.text, db.Pos{File: file, Line: st.line})
	}
	t.Indexes = append(t.Indexes, db.Index{Pos: db.Pos{File: file, Line: st.line}, Columns: splitIdents(m[5]), Unique: unique})
}

// --- small parse helpers ---

func parseForeignKey(c string, pos db.Pos) (db.ForeignKey, bool) {
	cols := parenCols(c)
	m := reReferences.FindStringSubmatch(c)
	if m == nil || len(cols) == 0 {
		return db.ForeignKey{}, false
	}
	return db.ForeignKey{Pos: pos, Columns: cols, RefTable: normalizeName(m[1]), RefColumns: splitIdents(m[2])}, true
}

// parenCols returns the identifiers inside the first (...) of s.
func parenCols(s string) []string {
	i := strings.IndexByte(s, '(')
	if i < 0 {
		return nil
	}
	inner, _, ok := balancedParen(s, i)
	if !ok {
		return nil
	}
	return splitIdents(inner)
}

func splitIdents(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		p = strings.TrimSpace(p)
		// drop trailing ASC/DESC and quotes
		if sp := strings.IndexByte(p, ' '); sp >= 0 {
			p = p[:sp]
		}
		if p = normalizeName(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// firstToken splits s into its first whitespace-delimited token and the rest.
func firstToken(s string) (string, string) {
	s = strings.TrimSpace(s)
	i := strings.IndexAny(s, " \t\n")
	if i < 0 {
		return s, ""
	}
	return s[:i], strings.TrimSpace(s[i:])
}

// splitTypeAndMods splits a column's tail into its type expression and the
// trailing modifiers, stopping the type at the first modifiers keyword at
// paren-depth 0. modifiers is the dialect's parse-and-ignore vocabulary
// (dialect.Modifiers) — this function itself stays dialect-free CODE.
func splitTypeAndMods(rest string, modifiers map[string]bool) (typeExpr, mods string) {
	depth := 0
	i := 0
	for i < len(rest) {
		if rest[i] == '(' {
			depth++
		} else if rest[i] == ')' {
			depth--
		} else if depth == 0 && (rest[i] == ' ' || rest[i] == '\t' || rest[i] == '\n') {
			word, _ := firstToken(rest[i:])
			kw := word
			if pi := strings.IndexByte(kw, '('); pi >= 0 {
				kw = kw[:pi]
			}
			if modifiers[strings.ToUpper(kw)] {
				return strings.TrimSpace(rest[:i]), strings.TrimSpace(rest[i:])
			}
		}
		i++
	}
	return strings.TrimSpace(rest), ""
}

// part is a comma-separated fragment with its byte offset in the source.
type part struct {
	text string
	off  int
}

// splitTopLevelParts splits by commas at paren-depth 0, respecting single-quoted
// strings, returning each fragment with its offset.
func splitTopLevelParts(s string) []part {
	var out []part
	depth, start := 0, 0
	inStr := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case inStr:
			if c == '\'' {
				inStr = false
			}
		case c == '\'':
			inStr = true
		case c == '(':
			depth++
		case c == ')':
			depth--
		case c == ',' && depth == 0:
			out = append(out, part{s[start:i], start})
			start = i + 1
		}
	}
	out = append(out, part{s[start:], start})
	return out
}

// balancedParen returns the content between the '(' at openIdx and its matching
// ')', the index just after '(', and ok. Respects single-quoted strings.
func balancedParen(s string, openIdx int) (inner string, innerStart int, ok bool) {
	if openIdx >= len(s) || s[openIdx] != '(' {
		return "", 0, false
	}
	depth := 0
	inStr := false
	for i := openIdx; i < len(s); i++ {
		c := s[i]
		switch {
		case inStr:
			if c == '\'' {
				inStr = false
			}
		case c == '\'':
			inStr = true
		case c == '(':
			depth++
		case c == ')':
			depth--
			if depth == 0 {
				return s[openIdx+1 : i], openIdx + 1, true
			}
		}
	}
	return "", 0, false
}

// normalizeName strips surrounding quotes and a schema qualifier (public.t -> t).
func normalizeName(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.LastIndexByte(s, '.'); i >= 0 {
		s = s[i+1:]
	}
	s = strings.Trim(s, `"`)
	return strings.TrimSpace(s)
}

// routineName strips a function/procedure argument list: f(int) -> f.
func routineName(s string) string {
	if i := strings.IndexByte(s, '('); i >= 0 {
		s = s[:i]
	}
	return normalizeName(s)
}

func containsWord(s, word string) bool {
	for _, f := range strings.Fields(s) {
		if f == word {
			return true
		}
	}
	return false
}

func trimPrefixFold(s, prefix string) string {
	if len(s) >= len(prefix) && strings.EqualFold(s[:len(prefix)], prefix) {
		return strings.TrimSpace(s[len(prefix):])
	}
	return s
}

func hasColumn(t *db.Table, name string) bool {
	for _, c := range t.Columns {
		if c.Name == name {
			return true
		}
	}
	return false
}

func dropColumn(t *db.Table, name string) {
	kept := t.Columns[:0]
	for _, c := range t.Columns {
		if c.Name != name {
			kept = append(kept, c)
		}
	}
	t.Columns = kept
}
