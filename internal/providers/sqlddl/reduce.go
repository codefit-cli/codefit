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

	// partSchemeFunc and partFuncStrategy hold T-SQL's two-hop partitioning
	// vocabulary (partition-capture), keyed by LOWERCASED identifier because
	// T-SQL identifiers are case-insensitive: scheme -> partition function,
	// and partition function -> the strategy word its own statement spells
	// ("AS RANGE RIGHT" -> "range"). They are reducer-internal ONLY — no
	// model surface is added for them, because a CREATE PARTITION
	// FUNCTION/SCHEME statement affects no table's columns, keys or indexes.
	// Their sole purpose is to let a table's "ON <scheme>(<col>)" clause
	// report a strategy that is genuinely IN THE SOURCE instead of leaving
	// T-SQL's Strategy permanently empty. A scheme this map cannot resolve
	// leaves Strategy empty — never a default.
	partSchemeFunc   map[string]string
	partFuncStrategy map[string]string
}

func newBuilder(dialect *Dialect) *builder {
	return &builder{
		tables:           map[string]*db.Table{},
		seenIndex:        map[string]bool{},
		dialect:          dialect,
		partSchemeFunc:   map[string]string{},
		partFuncStrategy: map[string]string{},
	}
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
	// reCreateIndex recognizes the "ordinary" CREATE INDEX shape — one with an
	// explicit column list — across all three dialects, and captures the
	// index's declared access method wherever the dialect places it:
	//
	//   group 1: UNIQUE
	//   group 2: T-SQL's CLUSTERED|NONCLUSTERED kind (no column-list impact —
	//            same statement shape as an ordinary index, just an extra
	//            keyword before INDEX; index-method-capture)
	//   group 3: IF NOT EXISTS
	//   group 4: the index NAME — now OPTIONAL, to admit PostgreSQL's
	//            anonymous form ("CREATE INDEX ON t ..."), where PostgreSQL
	//            itself generates the name (index-method-capture). Making
	//            this optional does NOT introduce ambiguity with an ordinary
	//            named index whose name happens to start with "on" (e.g.
	//            "on_hand_idx") or a TABLE named "on": Go's regexp (RE2) finds
	//            the correct overall match via Pike's algorithm, which
	//            reproduces Perl-like leftmost-first submatch semantics
	//            without true backtracking — both directions are locked by
	//            TestSQLDDL_PG_AnonymousIndex_NamedIndexStartingWithOn_
	//            StillAttributes and ..._OnTableNamedOn.
	//   group 5: the table name
	//   group 6: PostgreSQL's USING <method> position, BEFORE the column list
	//   group 7: the column list
	//   group 8: MySQL's USING BTREE|HASH position, AFTER the column list —
	//            a DIFFERENT grammar position from PostgreSQL's (
	//            index-method-capture)
	reCreateIndex = regexp.MustCompile(`(?is)^create\s+(unique\s+)?(clustered\s+|nonclustered\s+)?index\s+` +
		`(?:concurrently\s+)?(if\s+not\s+exists\s+)?(?:("?[\w"]+"?)\s+)?on\s+("?[\w".]+"?)\s*` +
		`(?:using\s+(\w+)\s*)?\(([^)]*)\)(?:\s*using\s+(\w+))?`)

	// reCreateColumnstoreIndex recognizes T-SQL's CREATE [CLUSTERED] COLUMNSTORE
	// INDEX — a genuinely DIFFERENT statement shape from reCreateIndex, not a
	// widened case of it: a clustered columnstore index (CLUSTERED is also
	// this statement's own default when omitted) carries NO column list at
	// all — it always covers every column of the table implicitly. This is
	// deliberately its own branch (index-method-capture task instruction),
	// not folded into reCreateIndex's `\(([^)]*)\)` grammar, which requires a
	// column list to match at all. A NONCLUSTERED COLUMNSTORE INDEX, which
	// DOES take an explicit column list, is a distinct shape this regex does
	// NOT cover — it stays a genuinely unrecognized CREATE INDEX-shaped head
	// (reIndexShapedHead), out of scope for this slice.
	//
	//   group 1: CLUSTERED (optional; also this statement's own default)
	//   group 2: IF NOT EXISTS
	//   group 3: the index name (T-SQL always requires one here, unlike PG's
	//            anonymous form)
	//   group 4: the table name
	reCreateColumnstoreIndex = regexp.MustCompile(`(?is)^create\s+(clustered\s+)?columnstore\s+index\s+` +
		`(if\s+not\s+exists\s+)?("?[\w"]+"?)\s+on\s+("?[\w".]+"?)\b`)
	reView       = regexp.MustCompile(`(?is)^create\s+(?:or\s+replace\s+)?(?:materialized\s+)?view\s+(?:if\s+not\s+exists\s+)?("?[\w".]+"?)`)
	reRoutine    = regexp.MustCompile(`(?is)^create\s+(?:or\s+replace\s+)?(?:function|procedure)\s+("?[\w".]+"?)`)
	reTrigger    = regexp.MustCompile(`(?is)^create\s+(?:or\s+replace\s+)?(?:constraint\s+)?trigger\s+("?[\w".]+"?)\b.*?\son\s+("?[\w".]+"?)`)
	reDropTable  = regexp.MustCompile(`(?is)^drop\s+table\s+(?:if\s+exists\s+)?("?[\w".]+"?)`)
	reReferences = regexp.MustCompile(`(?is)references\s+("?[\w".]+"?)\s*(?:\(([^)]*)\))?`)

	// reIndexShapedHead recognizes a CREATE INDEX-family statement head
	// BROADER than reCreateIndex/reCreateColumnstoreIndex COMBINED — the
	// LAST-RESORT net in apply()'s switch, checked AFTER both of those (so it
	// only ever sees a statement neither of them could dispatch). Its own
	// regex text is UNCHANGED and still broadly matches CLUSTERED/
	// NONCLUSTERED/COLUMNSTORE/anonymous-shaped heads too — the dispatch
	// ORDER, not this regex, is what determines which forms actually reach
	// this case. As of index-method-capture, what STILL lands here (an
	// anonymous PostgreSQL index and T-SQL's ordinary CLUSTERED/NONCLUSTERED
	// index no longer do — reCreateIndex now reads both; T-SQL's CLUSTERED
	// COLUMNSTORE INDEX no longer does either — reCreateColumnstoreIndex now
	// reads it): PostgreSQL's "ON ONLY" partitioned-table index; the
	// standalone FULLTEXT/SPATIAL/XML/PRIMARY XML CREATE INDEX statement
	// forms — this package already treats FULLTEXT/SPATIAL as recognized
	// index vocabulary for the INLINE and ALTER...ADD shorthand forms
	// (isInlineKeyIndexForm/isAddKeyIndexForm), so leaving the standalone
	// CREATE form out here would be an internal inconsistency, not a new
	// dialect gap, REL-001); and T-SQL's CREATE NONCLUSTERED COLUMNSTORE
	// INDEX, which — unlike its CLUSTERED counterpart — carries an explicit
	// column list, a materially different shape reCreateColumnstoreIndex
	// deliberately does not cover (out of scope this slice). apply()'s
	// default: branch can then tell "this dispatch genuinely has no branch
	// for this INDEX form" apart from a statement that is out of the
	// declared subset entirely (INSERT, GRANT, COMMENT, CREATE TYPE, ...),
	// which must stay silent (ADR 0034 SS2.4;
	// TestSQLDDL_OutOfSubsetStatement_RecordsNothing locks that boundary).
	reIndexShapedHead = regexp.MustCompile(`(?is)^create\s+(?:unique\s+)?(?:clustered\s+|nonclustered\s+)?` +
		`(?:columnstore\s+|fulltext\s+|spatial\s+|primary\s+xml\s+|xml\s+)?index\b`)

	// reIndexShapedTarget extracts the target table from a CREATE
	// INDEX-shaped statement the dispatch could not fully reduce (via
	// markUnrecognizedIndexShape) — the identifier following its ON (or ON
	// ONLY, PostgreSQL's partitioned-table syntax) clause. Same
	// table-identifier character class as reCreateIndex's own capture group
	// 5 (the TABLE name, `[\w".]` — schema-qualifier-tolerant), NOT group 4
	// (the INDEX name, `[\w"]`, no dot) — a "restore consistency" edit that
	// picked group 4 instead would silently drop schema qualifiers from the
	// attribution target and misattribute drops. Narrow on purpose: used
	// ONLY to ATTRIBUTE an already-confirmed unrecognized index drop to a
	// table (design: "a wrong attribution is worse than none"), never to
	// reduce the statement itself.
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

	// --- table partitioning (partition-capture) -------------------------
	//
	// reCreateTablePartitionOf recognizes PostgreSQL's partition CHILD form,
	// "CREATE TABLE <child> PARTITION OF <parent> FOR VALUES ... | DEFAULT".
	// It is a genuinely DIFFERENT statement shape from reCreateTable, not a
	// widened case of it: reCreateTable requires a '(' immediately after the
	// table name, which this form does not have, so the two can never
	// compete for the same statement (verified against the real parser
	// before this slice: this form matched NOTHING and the whole child table
	// vanished). "PARTITION OF" is matched as a two-word unit so it can
	// never be confused with the parent's "PARTITION BY".
	reCreateTablePartitionOf = regexp.MustCompile(`(?is)^create\s+table\s+(?:if\s+not\s+exists\s+)?("?[\w".]+"?)\s+partition\s+of\s+("?[\w".]+"?)`)

	// rePartitionBy matches the parent's "PARTITION BY" keyword pair
	// (PostgreSQL and MySQL spell it identically). It is deliberately only
	// the KEYWORD: the strategy word and the key list are read by walking
	// forward from the match, because MySQL's strategies are multi-word
	// ("LINEAR HASH", "RANGE COLUMNS") and its keys can be arbitrary
	// EXPRESSIONS whose parentheses a regex character class cannot balance.
	// Callers must locate it with firstTopLevelMatch, never with a bare
	// FindStringIndex: "PARTITION BY" is ALSO window-function syntax (OVER
	// (PARTITION BY ...)) and can appear inside a quoted table COMMENT.
	rePartitionBy = regexp.MustCompile(`(?is)\bpartition\s+by\b`)

	// rePartitionOf locates the "PARTITION OF" keyword pair inside an
	// already-dispatched partition-child statement, so the Declaration can
	// start at the clause rather than at "CREATE TABLE".
	rePartitionOf = regexp.MustCompile(`(?is)\bpartition\s+of\b`)

	// rePartitionSchemeOn matches T-SQL's "ON <partition scheme> (<column>)"
	// table-tail clause. The PARENTHESIZED COLUMN is the whole
	// discriminator: T-SQL's grammar admits "ON <filegroup>" (the vendored
	// AdventureWorksDW corpus ends every CREATE TABLE with ") ON
	// [PRIMARY];") and "ON <scheme>(<column>)", and only the latter is
	// partitioning. Requiring the '(' is what keeps a filegroup from being
	// reported as a partition scheme. \b before "on" stops the match from
	// starting inside TEXTIMAGE_ON / FILESTREAM_ON, whose '_' is a word
	// character.
	rePartitionSchemeOn = regexp.MustCompile(`(?is)\bon\s+("?[\w]+"?)\s*\(`)

	// rePartitionFunction / rePartitionScheme read T-SQL's two standalone
	// partitioning statements. Neither affects any table's columns, keys or
	// indexes, so neither reaches the neutral model directly (they populate
	// builder-internal maps): they exist only so a table's ON-clause can
	// report the strategy word its partition function actually spells.
	rePartitionFunction = regexp.MustCompile(`(?is)^create\s+partition\s+function\s+("?[\w".]+"?)\s*\([^)]*\)\s*as\s+(\w+)\b`)
	rePartitionScheme   = regexp.MustCompile(`(?is)^create\s+partition\s+scheme\s+("?[\w".]+"?)\s+as\s+partition\s+("?[\w".]+"?)`)
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
	case reCreateTablePartitionOf.MatchString(st.text):
		// PostgreSQL's partition CHILD. Before partition-capture this
		// statement matched NO branch at all and fell through to default:,
		// so the entire table disappeared from the model without a trace —
		// the one failure mode ADR 0034 exists to prevent.
		b.applyCreateTablePartitionOf(file, st)
	case b.dialect.PartitionSchemeOnClause && rePartitionScheme.MatchString(st.text):
		m := rePartitionScheme.FindStringSubmatch(st.text)
		b.partSchemeFunc[strings.ToLower(normalizeName(m[1]))] = strings.ToLower(normalizeName(m[2]))
	case b.dialect.PartitionSchemeOnClause && rePartitionFunction.MatchString(st.text):
		m := rePartitionFunction.FindStringSubmatch(st.text)
		b.partFuncStrategy[strings.ToLower(normalizeName(m[1]))] = strings.ToLower(m[2])
	case strings.HasPrefix(head, "alter table"):
		b.applyAlterTable(file, st)
	case reCreateIndex.MatchString(st.text):
		b.applyCreateIndex(file, st)
	case reCreateColumnstoreIndex.MatchString(st.text):
		b.applyCreateColumnstoreIndex(file, st)
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
		// a CREATE INDEX statement but neither reCreateIndex nor
		// reCreateColumnstoreIndex's grammar has a branch for it (PostgreSQL's
		// ON ONLY, a standalone FULLTEXT/SPATIAL/XML/PRIMARY XML form, or
		// T-SQL's CREATE NONCLUSTERED COLUMNSTORE INDEX, which carries an
		// explicit column list unlike its CLUSTERED counterpart). Per ADR
		// 0034 SS2.4, this is NOT a declared skip: the dispatch genuinely
		// does not know whether it declares an index, so it must mark the
		// table unproven instead of vanishing silently.
		b.markUnrecognizedIndexShape(file, st)
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
	// Partitioning is declared in the TAIL — the text after the body's
	// matching ')' — in all three dialects, never inside the body. Reading
	// it can only ADD to the model: it never calls MarkUnproven, so a table
	// that is proven complete today stays proven after this slice (locked by
	// TestSQLDDL_PG_PartitionedParent_StaysComplete and, over every vendored
	// corpus, by TestSQLDDL_NoVendoredCorpusDeclaresPartitioning).
	closeIdx := innerStart + len(inner) // index of the body's matching ')'
	if closeIdx+1 <= len(st.text) {
		t.Partitioning = b.readPartitioning(st.text[closeIdx+1:])
	}
}

// readPartitioning reduces a CREATE TABLE's TAIL into db.Partitioning. tail is
// everything after the column-list body's matching ')'.
//
// It reads two grammars:
//
//	PARTITION BY <strategy> (<key>) [ (<partition definitions>) ]   PG + MySQL
//	ON <partition scheme> (<column>)                                T-SQL only
//
// Both are located with firstTopLevelMatch, NOT a bare regex search. That is
// load-bearing twice over: "PARTITION BY" is also WINDOW-FUNCTION syntax
// (OVER (PARTITION BY ...)), and either keyword can appear as ordinary text
// inside a quoted MySQL table COMMENT — firstTopLevelMatch ignores anything
// at paren depth > 0 or inside a single-quoted string, so neither can reach
// this reduction.
//
// It NEVER calls MarkUnproven. A partitioning clause declares no column, no
// primary key and no index, so failing to decompose one is not the kind of
// blindness db.Table.Complete measures; treating it as such would demote
// tables that are fully readable today and mute every absence-based DB rule
// across ordinary partitioned DDL. Partial reads are reported INSIDE
// db.Partitioning instead: Declaration always carries the source clause, and
// Strategy/Key stay empty rather than guessing.
func (b *builder) readPartitioning(tail string) db.Partitioning {
	if m := firstTopLevelMatch(tail, rePartitionBy); m != nil {
		return partitionByClause(tail, m[0], m[1])
	}
	if b.dialect.PartitionSchemeOnClause {
		if m := firstTopLevelMatch(tail, rePartitionSchemeOn); m != nil {
			return b.partitionSchemeClause(tail, m)
		}
	}
	return db.Partitioning{}
}

// partitionByClause reduces "PARTITION BY <strategy> (<key>) ..." starting at
// tail[start:], with kwEnd the offset just past the "BY".
//
// The Declaration runs from the PARTITION keyword to the END of the
// statement, deliberately: MySQL's partition-definition list and MySQL's
// SUBPARTITION BY clause both belong to the partitioning declaration and
// neither can be delimited by the key's closing paren. The cost is that a
// clause a dialect allows AFTER partitioning (PostgreSQL's TABLESPACE) is
// included too. That is the right trade for a field whose purpose is to let
// an agent READ THE SOURCE when the structured fields abstain: a Declaration
// with one extra clause is still the truth, while one cut short would hide
// the subpartitioning that explains the table.
func partitionByClause(tail string, start, kwEnd int) db.Partitioning {
	p := db.Partitioning{Declaration: strings.TrimSpace(tail[start:])}
	open := strings.IndexByte(tail[kwEnd:], '(')
	if open < 0 {
		// No key list at all (malformed, or a form this reducer does not
		// know). The declaration stands; nothing is invented from it.
		return p
	}
	p.Strategy = partitionStrategyWord(tail[kwEnd : kwEnd+open])
	if inner, _, ok := balancedParen(tail, kwEnd+open); ok {
		p.Key = partitionKeyColumns(inner)
	}
	return p
}

// partitionSchemeClause reduces T-SQL's "ON <scheme> (<column>)" table-tail
// clause. m is rePartitionSchemeOn's submatch index slice: m[2:4] is the
// scheme name and m[1] is just past the '(' that opened the column list.
//
// The strategy is resolved through the scheme's own CREATE PARTITION
// FUNCTION when the parsed DDL contained one, and left EMPTY otherwise — it
// is never defaulted to "range" merely because that is T-SQL's only
// partition-function strategy word. An unresolvable scheme is a scheme
// declared in a file this scan did not read, and codefit reports what it
// read.
func (b *builder) partitionSchemeClause(tail string, m []int) db.Partitioning {
	scheme := normalizeName(tail[m[2]:m[3]])
	p := db.Partitioning{Scheme: scheme}
	inner, _, ok := balancedParen(tail, m[1]-1)
	if !ok {
		return db.Partitioning{}
	}
	p.Declaration = strings.TrimSpace(tail[m[0] : m[1]+len(inner)])
	p.Key = partitionKeyColumns(inner)
	if fn, found := b.partSchemeFunc[strings.ToLower(scheme)]; found {
		p.Strategy = b.partFuncStrategy[fn]
	}
	return p
}

// applyCreateTablePartitionOf reduces PostgreSQL's partition CHILD statement,
// "CREATE TABLE <child> PARTITION OF <parent> FOR VALUES ... | DEFAULT".
//
// The child is registered as ITS OWN TABLE — it is one, with its own storage
// and its own indexes — carrying a back-reference to its parent, and is then
// marked UNPROVEN. That statement declares the child's partition bounds and
// nothing else: its columns, primary key and constraints are all inherited
// from the parent and appear nowhere in it. Registering it as a complete
// table with zero columns would hand DB-050 a table that "declares no primary
// key" — a false affirmation over a key the parent declares. Leaving it out
// of the model, which is what happened before this slice, silently deleted a
// real table instead.
func (b *builder) applyCreateTablePartitionOf(file string, st stmt) {
	m := reCreateTablePartitionOf.FindStringSubmatch(st.text)
	pos := db.Pos{File: file, Line: st.line}
	t, _ := b.getTable(normalizeName(m[1]), pos)
	t.Partitioning = db.Partitioning{
		Declaration: partitionOfDeclaration(st.text),
		Of:          normalizeName(m[2]),
	}
	t.MarkUnproven(db.ReasonPartitionChildInheritsStructure, st.text, pos)
}

// partitionOfDeclaration returns the "PARTITION OF ..." clause of a partition
// child statement, verbatim from the source, or the whole statement if the
// keyword cannot be located (never an empty declaration).
func partitionOfDeclaration(text string) string {
	if m := firstTopLevelMatch(text, rePartitionOf); m != nil {
		return strings.TrimSpace(text[m[0]:])
	}
	return strings.TrimSpace(text)
}

// partitionStrategyWord normalizes the text between "PARTITION BY" and the
// key's '(' into the strategy word the SOURCE spells, lowercased and
// whitespace-collapsed: "RANGE" -> "range", "LINEAR HASH" -> "linear hash",
// "RANGE  COLUMNS" -> "range columns". codefit maintains no closed strategy
// vocabulary — whatever word the source used is what is reported.
//
// It returns EMPTY for anything that is not a run of plain words, e.g.
// MySQL's "KEY ALGORITHM=2": that is a form this reducer does not decompose,
// and reporting "key algorithm=2" as a strategy would be inventing a
// vocabulary word no dialect has. The Declaration still carries the clause,
// so the abstention is visible rather than silent.
func partitionStrategyWord(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		isWord := c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
		isSpace := c == ' ' || c == '\t' || c == '\n' || c == '\r'
		if !isWord && !isSpace {
			return ""
		}
	}
	return strings.ToLower(strings.Join(strings.Fields(s), " "))
}

// partitionKeyColumns returns the partition key's plain COLUMN identifiers,
// or nil when the key is not a plain column list.
//
// FABRICATION GUARD. It deliberately does NOT reuse splitIdents, the
// reducer's ordinary column-list splitter: splitIdents cuts each part at its
// first space and strips a schema qualifier, so "YEAR(`sold_on`)" — a
// perfectly ordinary MySQL partition key — comes back as the single "column"
// `YEAR("sold_on")`, a name that exists in no table in the schema. A rule
// reading it would compare a partition key against column names and match
// nothing, or worse, report it to an agent as a real column. Table.Complete
// cannot catch that class at all: it measures DROPS, not FABRICATIONS (see
// its own doc, "BOUNDARY"). So an expression key yields nil here, and the
// caller reports the clause through Declaration instead.
func partitionKeyColumns(inner string) []string {
	parts := splitTopLevelParts(inner)
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		tok := strings.TrimSpace(p.text)
		if !isPlainIdentifier(tok) {
			return nil
		}
		out = append(out, normalizeName(tok))
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// isPlainIdentifier reports whether s is a single bare or double-quoted SQL
// identifier — no function call, no operator, no qualifier, no whitespace.
// Quoting was already canonicalized to ANSI double quotes by split() before
// the reducer ever sees a statement, so one quote style is enough here.
func isPlainIdentifier(s string) bool {
	if s == "" {
		return false
	}
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		inner := s[1 : len(s)-1]
		return inner != "" && !strings.ContainsAny(inner, `"`)
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c != '_' && (c < '0' || c > '9') && (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') {
			return false
		}
	}
	return true
}

// firstTopLevelMatch returns re's first submatch-index slice whose match
// BEGINS at paren depth 0 and outside a single-quoted string, or nil.
//
// This is the guard that keeps query syntax out of the schema model. "OVER
// (PARTITION BY customer_id)" puts its PARTITION BY at paren depth 1, and a
// MySQL table COMMENT='partition by range' puts it inside a string literal;
// a bare regex search would reduce either one into a table's declared
// partitioning. Depth and string tracking follow the same convention as
// balancedParen and splitTopLevelParts elsewhere in this file.
func firstTopLevelMatch(s string, re *regexp.Regexp) []int {
	depth, inStr := 0, false
	topLevel := make([]bool, len(s))
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
		default:
			topLevel[i] = depth == 0
		}
	}
	for _, m := range re.FindAllStringSubmatchIndex(s, -1) {
		if m[0] < len(topLevel) && topLevel[m[0]] {
			return m
		}
	}
	return nil
}

// applyTableItem parses one comma-separated item of a CREATE TABLE body: a table
// constraint or a column definition.
func (b *builder) applyTableItem(t *db.Table, item string, pos db.Pos) {
	item = strings.TrimSpace(item)
	up := strings.ToUpper(item)
	kw := leadingKeyword(item)
	switch {
	case kw == "CONSTRAINT":
		b.applyTableConstraint(t, stripConstraintName(item), pos)
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

// stripConstraintName removes the "CONSTRAINT <name>" preamble from a named
// table constraint, returning the constraint BODY ("PRIMARY KEY (a)",
// "FOREIGN KEY (a) REFERENCES t (b)", …). item must already be known to lead
// with the CONSTRAINT keyword. The separator is ANY whitespace or '(' — not a
// literal single space — because T-SQL's own generated scripts wrap a long
// constraint over several lines ("CONSTRAINT [pk] PRIMARY KEY CLUSTERED\n(\n
// [a]\n)").
//
// An item that is nothing BUT "CONSTRAINT <name>" (no body at all) is
// returned UNCHANGED rather than blanked: the text is what
// applyTableConstraint's honest-abstention floor records for the agent to
// read, and blanking it would hand the agent an empty claim.
func stripConstraintName(item string) string {
	rest := strings.TrimSpace(item[len(leadingKeyword(item)):])
	sp := strings.IndexAny(rest, " \t\n(")
	if sp < 0 {
		return item
	}
	return strings.TrimSpace(rest[sp:])
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

// applyTableConstraint reduces ONE table-constraint body (the text AFTER any
// "CONSTRAINT <name>" preamble) into the neutral model. It is shared by every
// path that can carry a constraint — a CREATE TABLE body item
// (applyTableItem) and an ALTER TABLE ... ADD item (applyAlterAdd) — and by
// every dialect; it must therefore stay dialect-free CODE.
//
// FABRICATION GUARD (tsql-alter-add-constraint): the key-declaring forms all
// read their columns from a BALANCED parenthesized list (parenCols). When
// that list cannot be read at all — absent, unbalanced, or empty — the
// constraint is NOT reduced to a silently empty key/index: it falls to the
// honest-abstention floor (MarkUnproven, ADR 0034), the same floor a
// constraint form the dispatch does not recognize already uses. A silently
// empty PrimaryKey is not a neutral no-op — it is the exact input DB-050
// reads as "this table declares no primary key", so it would AFFIRM an
// absence over DDL the reducer merely failed to read: the very class of false
// affirmation the completeness contract exists to prevent.
func (b *builder) applyTableConstraint(t *db.Table, c string, pos db.Pos) {
	up := strings.ToUpper(strings.TrimSpace(c))
	kw := leadingKeyword(c)
	switch {
	case strings.HasPrefix(up, "PRIMARY KEY"):
		cols := parenCols(c)
		if len(cols) == 0 {
			t.MarkUnproven(db.ReasonUnreducedTableStatement, c, pos)
			return
		}
		t.PrimaryKey = cols
	case strings.HasPrefix(up, "UNIQUE"):
		cols := parenCols(c)
		if len(cols) == 0 {
			t.MarkUnproven(db.ReasonUnreducedTableStatement, c, pos)
			return
		}
		t.Indexes = append(t.Indexes, db.Index{Pos: pos, Columns: cols, Unique: true, Method: indexMethodOutsideParens(c)})
	case strings.HasPrefix(up, "FOREIGN KEY"):
		fk, ok := parseForeignKey(c, pos)
		if !ok {
			t.MarkUnproven(db.ReasonUnreducedTableStatement, c, pos)
			return
		}
		t.ForeignKeys = append(t.ForeignKeys, fk)
	case kw == "KEY" || kw == "INDEX" || kw == "FULLTEXT" || kw == "SPATIAL":
		// MySQL inline KEY/INDEX/FULLTEXT KEY/SPATIAL KEY shorthand (task
		// I4b) — recorded as a plain (non-unique) index by its base columns.
		// Method (F1, coordinator review of index-method-capture): MySQL's
		// index_type/index_option grammar lets USING BTREE|HASH appear
		// either before the column list (index_type) or after it
		// (index_option) — indexMethodOutsideParens reads either position,
		// same convention as the standalone CREATE INDEX capture site.
		cols := parenCols(c)
		if len(cols) == 0 {
			t.MarkUnproven(db.ReasonUnreducedTableStatement, c, pos)
			return
		}
		t.Indexes = append(t.Indexes, db.Index{Pos: pos, Columns: cols, Unique: false, Method: indexMethodOutsideParens(c)})
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
		Type:    b.dialect.mapType(typeLookupKey(rawType)),
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
	// inAddList tracks whether the part just applied was an ADD whose item
	// list a following comma-separated part CONTINUES. "ALTER TABLE t ADD a,
	// b" is ONE ADD taking a list — the later items repeat no verb — while
	// "ALTER TABLE t ADD a, DROP b" is two independent actions. Only a part
	// whose own leading keyword names a constraint form is ever read as a
	// continuation (isAddListContinuation); anything else ends the list and is
	// dispatched as its own action exactly as before, so no dialect that
	// repeats the verb per action is affected.
	inAddList := false
	for i, p := range splitTopLevelParts(m[2]) {
		line := st.line + strings.Count(st.text[:actOff+p.off], "\n")
		pos := db.Pos{File: file, Line: line}
		act := strings.TrimSpace(p.text)
		if i == 0 {
			act = trimWithCheckPrefix(act)
		}
		switch {
		case leadingKeyword(act) == "ADD":
			inAddList = true
			b.applyAlterAdd(t, strings.TrimSpace(act[len("ADD"):]), pos)
		case inAddList && isAddListContinuation(act):
			b.applyAlterAdd(t, act, pos)
		default:
			inAddList = false
			b.applyAlterAction(t, act, pos)
		}
	}
}

// trimWithCheckPrefix removes T-SQL's "WITH CHECK" / "WITH NOCHECK" preamble
// from an ALTER TABLE action group ("ALTER TABLE t WITH CHECK ADD CONSTRAINT
// …" — the shape Microsoft's own generated scripts, including
// AdventureWorksDW's, use for every key they declare). The preamble only
// states whether existing rows are validated against the constraint being
// added; it declares nothing itself, so dropping it leaves the ADD action
// underneath to be dispatched normally.
//
// Nothing else is trimmed: an action group starting with WITH but NOT
// followed by CHECK/NOCHECK is returned unchanged and reaches the dispatch
// as-is, where an unrecognized form still falls to the abstention floor.
func trimWithCheckPrefix(act string) string {
	if leadingKeyword(act) != "WITH" {
		return act
	}
	rest := strings.TrimSpace(act[len("WITH"):])
	switch kw := leadingKeyword(rest); kw {
	case "CHECK", "NOCHECK":
		return strings.TrimSpace(rest[len(kw):])
	default:
		return act
	}
}

// isAddListContinuation reports whether a comma-separated ALTER TABLE part is
// a further ITEM of the preceding ADD rather than an action of its own — i.e.
// whether it leads with a constraint keyword instead of an action verb. The
// vocabulary is deliberately narrow (constraint forms only): a bare column
// definition continuing an "ADD a int, b int" list is NOT claimed here, so it
// keeps falling to the abstention floor rather than being guessed at.
func isAddListContinuation(act string) bool {
	switch leadingKeyword(act) {
	case "CONSTRAINT", "PRIMARY", "UNIQUE", "FOREIGN", "CHECK":
		return true
	default:
		return false
	}
}

// applyAlterAdd reduces ONE item of an ALTER TABLE ADD list — the text AFTER
// the ADD verb, or a comma-continuation item of the same list. It dispatches
// on the item's own LEADING KEYWORD rather than on a fixed-spacing prefix
// ("ADD CONSTRAINT" with exactly one space, as this dispatch used to), so any
// whitespace run between the verb and the item — the newline T-SQL scripts
// wrap long constraints with, SSMS's double space, a tab — reads identically.
//
// The fabrication narrowing this replaces (design §8c, spec "R1 — Fabrication
// Hypothesis") is not lost, it is SUBSUMED: every constraint keyword R1
// diverted to the abstention floor because the prefix match had missed it is
// now dispatched to applyTableConstraint, which reduces it when it can read
// the column list and falls to that same floor when it cannot. No path here
// can reach applyColumn with a constraint keyword in hand, which is what
// produced the phantom column literally named "CONSTRAINT".
func (b *builder) applyAlterAdd(t *db.Table, item string, pos db.Pos) {
	kw := leadingKeyword(item)
	switch {
	case kw == "CONSTRAINT":
		b.applyTableConstraint(t, stripConstraintName(item), pos)
	case kw == "PRIMARY" || kw == "UNIQUE" || kw == "FOREIGN" || kw == "CHECK" || kw == "EXCLUDE":
		b.applyTableConstraint(t, item, pos)
	case (kw == "KEY" || kw == "INDEX" || kw == "FULLTEXT" || kw == "SPATIAL") && b.isInlineKeyIndexForm(item):
		// ADD KEY idx (cols) / ADD INDEX idx (cols) / ADD FULLTEXT KEY (cols) /
		// ADD SPATIAL KEY (cols) — MySQL's secondary-index shorthand via ALTER
		// TABLE. Same TypeMap-driven discriminator as applyTableItem's inline
		// case: without it, a column legitimately named key/index would be
		// misread as an index (Unit I rework, C2).
		b.applyTableConstraint(t, item, pos)
	default:
		rest := trimPrefixFold(item, "COLUMN")
		rest = trimPrefixFold(rest, "IF NOT EXISTS")
		name, _ := firstToken(rest)
		if hasColumn(t, normalizeName(name)) {
			return // idempotent add
		}
		b.applyColumn(t, rest, pos)
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

// applyAlterAction dispatches ONE non-ADD ALTER TABLE action. ADD actions and
// their comma-continuation items are routed by applyAlterTable to
// applyAlterAdd before reaching here.
func (b *builder) applyAlterAction(t *db.Table, act string, pos db.Pos) {
	up := strings.ToUpper(act)
	switch {
	case strings.HasPrefix(up, "DROP COLUMN"), strings.HasPrefix(up, "DROP "):
		rest := strings.TrimSpace(act[len("DROP"):])
		rest = trimPrefixFold(rest, "COLUMN")
		rest = trimPrefixFold(rest, "IF EXISTS")
		name, _ := firstToken(rest)
		dropColumn(t, normalizeName(name))
	case isAlterActionRecognizedSkip(up), isConstraintCheckToggle(act):
		// ALTER COLUMN / RENAME / OWNER / ENABLE / DISABLE / CLUSTER / SET /
		// RESET / VALIDATE / NO … — declared, RECOGNIZED skips (N2): known
		// not to declare a key/index/column, so this is NOT incompleteness.
	default:
		// A genuinely UNRECOGNIZED alter action — the dispatch does not know
		// what this declares, so it cannot rule out a key/index (D2 site 5,
		// design §2).
		t.MarkUnproven(db.ReasonUnreducedTableStatement, act, pos)
	}
}

// isConstraintCheckToggle reports whether an ALTER TABLE action is T-SQL's
// "CHECK CONSTRAINT <name>|ALL" / "NOCHECK CONSTRAINT <name>|ALL" — a
// declared, RECOGNIZED skip: it only enables or disables constraint CHECKING
// on a constraint that already exists, so it can never declare a key, an
// index or a column. SSMS emits one after every foreign key it generates, so
// without this a T-SQL script would demote each of its tables to unproven for
// a statement that says nothing about structure.
//
// It is matched by leading KEYWORDS, not by the alterActionRecognizedSkips
// prefix list, for two reasons: "NOCHECK" is not covered by that list's "NO "
// entry (no space follows NO), and a bare "CHECK" prefix there would also
// swallow a genuine "CHECK (expr)" form — this requires CONSTRAINT to follow.
func isConstraintCheckToggle(act string) bool {
	kw := leadingKeyword(act)
	if kw != "CHECK" && kw != "NOCHECK" {
		return false
	}
	return leadingKeyword(strings.TrimSpace(act[len(kw):])) == "CONSTRAINT"
}

// markUnrecognizedIndexShape attributes a genuinely unrecognized CREATE
// INDEX-shaped statement to its target table (or Schema.Unreduced when no
// table resolves), marking the table's structure unproven. Shared by TWO call
// sites (index-method-capture, F2/F3 coordinator review): apply()'s
// reIndexShapedHead fallback (a shape reCreateIndex's grammar never matches
// at all) AND applyCreateIndex's own paren-balance guard (a shape
// reCreateIndex's grammar MATCHES syntactically but cannot safely reduce — an
// expression index). Both must fall to the exact SAME floor; sharing this
// helper is what keeps that guaranteed rather than merely convenient.
func (b *builder) markUnrecognizedIndexShape(file string, st stmt) {
	pos := db.Pos{File: file, Line: st.line}
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
}

func (b *builder) applyCreateIndex(file string, st stmt) {
	loc := reCreateIndex.FindStringSubmatchIndex(st.text)
	if loc == nil {
		return
	}
	m := reCreateIndex.FindStringSubmatch(st.text)
	// F2 (coordinator review, index-method-capture — regression, confirmed
	// by execution): reCreateIndex's own column-list grammar, \(([^)]*)\),
	// stops at the FIRST ')'. Making the index name optional (to admit
	// PostgreSQL's anonymous form) also brought PostgreSQL's anonymous
	// EXPRESSION index into reach — CREATE INDEX ON t (lower(email)) — whose
	// nested '(' means the naive capture truncates to "lower(email" instead
	// of the true "lower(email)". Left unchecked, that FABRICATES a column
	// the source never declared and leaves the table wrongly proven complete
	// — a regression from honest abstention (before this slice, this
	// statement fell to the floor) to silent fabrication. Parsing SQL
	// EXPRESSIONS is out of scope; balancedParen (already used by
	// applyCreateTable) proves the TRUE column-list span — when it disagrees
	// with the naive regex capture, the statement is not safely reducible,
	// so it falls to the SAME floor a genuinely unrecognized CREATE
	// INDEX-shaped statement uses, via markUnrecognizedIndexShape, instead
	// of inventing a name.
	openIdx := loc[14] - 1 // the '(' reCreateIndex's own grammar requires just before group 7 (the column list)
	trueInner, _, ok := balancedParen(st.text, openIdx)
	if !ok || trueInner != m[7] {
		b.markUnrecognizedIndexShape(file, st)
		return
	}
	unique := m[1] != ""
	name := normalizeName(m[4])
	if name != "" && m[3] != "" && b.seenIndex[name] { // IF NOT EXISTS + already created
		return
	}
	if name != "" {
		b.seenIndex[name] = true
	}
	t, created := b.getTable(normalizeName(m[5]), db.Pos{File: file, Line: st.line})
	if created {
		// F4 (4R ledger obs #1282): CREATE INDEX ... ON x is the FIRST time
		// x was ever seen — no CREATE TABLE declared it. Same disposition as
		// applyAlterTable's phantom-creation case above.
		t.MarkUnproven(db.ReasonTableNeverDeclared, st.text, db.Pos{File: file, Line: st.line})
	}
	// method precedence: at most ONE of these three is ever non-empty for a
	// given statement — T-SQL's CLUSTERED/NONCLUSTERED (m[2]), PostgreSQL's
	// USING before the column list (m[6]), MySQL's USING after the column
	// list (m[8]) — since the grammars are mutually exclusive per dialect.
	// Normalized to lowercase here (once), so every capture site in this
	// package shares one convention (index-method-capture).
	method := m[2]
	if method == "" {
		method = m[6]
	}
	if method == "" {
		method = m[8]
	}
	t.Indexes = append(t.Indexes, db.Index{
		Pos:     db.Pos{File: file, Line: st.line},
		Columns: splitIdents(m[7]),
		Unique:  unique,
		Method:  strings.ToLower(strings.TrimSpace(method)),
	})
}

// applyCreateColumnstoreIndex handles T-SQL's CREATE [CLUSTERED] COLUMNSTORE
// INDEX — the one CREATE INDEX-family shape with NO column list at all (see
// reCreateColumnstoreIndex's own doc comment). Columns is deliberately left
// EMPTY rather than synthesized: this statement never names any column in its
// own grammar (it implicitly covers every column of the table), and inventing
// a column list here would misrepresent the statement as declaring something
// it did not — the same "never fabricate what the source did not say"
// discipline ADR 0034 §2.6 already draws for the reducer generally. Method is
// unconditionally "columnstore" (never "clustered columnstore"): the
// CLUSTERED qualifier is this statement's own default when omitted, so it
// carries no distinguishing information a consumer (e.g. a future columnar-
// index rule) would need beyond "this IS a columnstore index".
func (b *builder) applyCreateColumnstoreIndex(file string, st stmt) {
	m := reCreateColumnstoreIndex.FindStringSubmatch(st.text)
	if m == nil {
		return
	}
	name := normalizeName(m[3])
	if m[2] != "" && b.seenIndex[name] { // IF NOT EXISTS + already created
		return
	}
	b.seenIndex[name] = true
	t, created := b.getTable(normalizeName(m[4]), db.Pos{File: file, Line: st.line})
	if created {
		t.MarkUnproven(db.ReasonTableNeverDeclared, st.text, db.Pos{File: file, Line: st.line})
	}
	t.Indexes = append(t.Indexes, db.Index{Pos: db.Pos{File: file, Line: st.line}, Method: "columnstore"})
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

// reUsingMethod matches a "USING <method>" clause — used ONLY outside a
// balanced column-list paren span (see indexMethodOutsideParens), so it can
// never mistake a column literally named "using" for this clause.
var reUsingMethod = regexp.MustCompile(`(?i)\busing\s+(\w+)`)

// indexMethodOutsideParens extracts a "USING <method>" clause from a
// table/inline index constraint's text OUTSIDE its balanced column-list
// parentheses (F1, coordinator review of index-method-capture) — MySQL's
// grammar allows it either BEFORE the column list (index_type: "KEY idx
// USING BTREE (col)") or AFTER it (index_option: "KEY idx (col) USING
// BTREE"), the same two-position ambiguity CREATE INDEX's own leading/
// trailing USING already has, at a different call site (applyCreateIndex).
// Searching only OUTSIDE the parens is deliberate: it is what stops a column
// literally named "using" inside the list from ever being mistaken for this
// clause. Empty when s has no column-list parens at all (PRIMARY KEY/FOREIGN
// KEY constraints never carry a method) or no USING clause is present.
func indexMethodOutsideParens(s string) string {
	i := strings.IndexByte(s, '(')
	if i < 0 {
		return ""
	}
	inner, innerStart, ok := balancedParen(s, i)
	if !ok {
		return ""
	}
	closeIdx := innerStart + len(inner) // index of the matching ')'
	before, after := s[:i], ""
	if closeIdx+1 <= len(s) {
		after = s[closeIdx+1:]
	}
	if m := reUsingMethod.FindStringSubmatch(after); m != nil {
		return strings.ToLower(m[1])
	}
	if m := reUsingMethod.FindStringSubmatch(before); m != nil {
		return strings.ToLower(m[1])
	}
	return ""
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
