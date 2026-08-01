package paradigm

import (
	"strings"

	"github.com/codefit-cli/codefit/internal/core/db"
)

// ---------------------------------------------------------------------------
// THE SCHEMA GATE — stage 1: computed, named, and wired to NOTHING.
//
// WHY IT EXISTS. Detect works BOTTOM-UP: it assigns a role to each table from
// its name plus local structural corroboration, then folds the schema-level
// Paradigm out of those roles. The sensor's 3NF suppression
// (internal/sensors/db) then consults the PER-TABLE role and never the folded
// Paradigm. The consequence is a real hole: one table named dim_status with
// fan-in >= 1, sitting in an otherwise purely transactional schema, decides its
// own silencing of the DB-002/DB-003 1NF surface. The schema gets no vote.
//
// The inversion this file prepares: decide from SCHEMA-WIDE evidence whether
// this is a warehouse AT ALL, and only then assign roles inside it. The reason
// the question has to be asked at the schema level is that it cannot be
// answered at the table level — order_items(order_id, product_id, quantity,
// price) is structurally INDISTINGUISHABLE from TPC-DS's
// store_sales(ss_item_sk, ss_customer_sk, ss_quantity). The table cannot be
// told apart; the schema can.
//
// STAGE 1 IS DELIBERATELY INERT. Nothing in this file is called by Detect,
// roleFor, fold, Resolve or any sensor — codefit's observable behavior with
// this file present is identical to its behavior without it. The signals exist
// to be MEASURED over real corpora first; the decision of how many, and which,
// constitute a warehouse is stage 2's, made from that measurement rather than
// from a hunch. The inertness is itself test-locked, in
// schemagate_inertness_test.go.
//
// NO SCORE, NO FUZZ. Each signal is an independently computable predicate over
// *db.Schema and is NAMED in the result, so a consumer can always be told WHY —
// never "warehouse-ness 0.7".
// ---------------------------------------------------------------------------

// Signal names one piece of schema-wide warehouse evidence. The six values
// below are the complete set; the gate reports which of them a schema exhibits
// and nothing more.
type Signal string

const (
	// SignalCalendarTable: the schema contains a dedicated calendar/date/time
	// table.
	SignalCalendarTable Signal = "calendar_table"
	// SignalSurrogateKeyNames: the schema uses the _sk surrogate-key naming
	// convention as a convention, not incidentally.
	SignalSurrogateKeyNames Signal = "surrogate_key_names"
	// SignalBulkLoadShape: no declared foreign keys anywhere, yet many
	// key-like columns — the shape of a warehouse that drops FK constraints
	// for load speed.
	SignalBulkLoadShape Signal = "bulk_load_shape"
	// SignalNoAuditTimestamps: not one table in the schema carries
	// created_at/updated_at.
	SignalNoAuditTimestamps Signal = "no_audit_timestamps"
	// SignalStarTopology: a hub-and-spoke of depth exactly 1 — a table whose
	// foreign keys reach two or more tables that themselves reference nothing.
	SignalStarTopology Signal = "star_topology"
	// SignalTypeProfileSplit: the schema's COLUMN TYPES split into two poles —
	// a few numeric-dominated tables plus several text-dominated ones — instead
	// of the uniform mix a transactional schema shows.
	SignalTypeProfileSplit Signal = "type_profile_split"
)

// allSignals fixes the evaluation and reporting order. A slice, not a map, so
// WarehouseEvidence.Fired is deterministic — a consumer that reports WHICH
// signals fired must not have that report reordered by map iteration.
var allSignals = []struct {
	name Signal
	fire func(*db.Schema) bool
}{
	{SignalCalendarTable, hasCalendarTable},
	{SignalSurrogateKeyNames, hasSurrogateKeyVocabulary},
	{SignalBulkLoadShape, hasBulkLoadShape},
	{SignalNoAuditTimestamps, hasNoAuditTimestamps},
	{SignalStarTopology, hasStarTopology},
	{SignalTypeProfileSplit, hasTypeProfileSplit},
}

// WarehouseEvidence is the gate's result: exactly which signals a schema
// exhibits, in the fixed order of allSignals.
//
// It deliberately carries NO verdict and NO threshold. "How many signals make a
// warehouse" is a decision the measurement is supposed to make, and stage 1
// exists to produce that measurement — publishing a threshold now would be
// answering the question the data has not been asked yet.
type WarehouseEvidence struct {
	Fired []Signal
}

// Has reports whether sig is among the fired signals.
func (e WarehouseEvidence) Has(sig Signal) bool {
	for _, f := range e.Fired {
		if f == sig {
			return true
		}
	}
	return false
}

// Count is how many signals fired.
func (e WarehouseEvidence) Count() int { return len(e.Fired) }

// WarehouseSignals evaluates all six schema-wide warehouse signals over s as a
// pure function, and returns the ones that fired.
//
// It is not called by anything in codefit's production paths (stage 1 is
// inert — see this file's header and schemagate_inertness_test.go).
func WarehouseSignals(s *db.Schema) WarehouseEvidence {
	e := WarehouseEvidence{}
	if !judgeable(s) {
		return e
	}
	for _, sig := range allSignals {
		if sig.fire(s) {
			e.Fired = append(e.Fired, sig.name)
		}
	}
	return e
}

// minJudgeableTables is the floor below which the gate declines to evaluate ANY
// signal.
//
// It exists because of the guard that matters more than any individual signal:
// NO VACUOUS TRUTHS. Two of the six conclude from ABSENCE — "no declared
// foreign keys" and "no audit timestamps anywhere" — and both are trivially
// true of an empty schema and of a one-table schema. A gate that fires on an
// empty schema would classify anything as a warehouse, which is the exact
// failure this whole inversion exists to prevent.
//
// 3 is the smallest schema in which any of the six shapes can genuinely
// exist: a hub-and-spoke needs a hub plus at least two spokes, and a type
// profile needs one numeric pole plus two text-pole tables. Below it the
// absence-based signals are vacuous and the structural ones are unreachable, so
// one floor serves all six and no signal can be the one that fires on a toy.
const minJudgeableTables = 3

// judgeable reports whether s is large enough for any signal to be evidence.
func judgeable(s *db.Schema) bool {
	return s != nil && len(s.Tables) >= minJudgeableTables
}

// ---------------------------------------------------------------------------
// Signal 1 — calendar/time table present
// ---------------------------------------------------------------------------

// calendarNames is the time vocabulary a table name must equal (after an
// OPTIONAL role token is stripped) to count as the schema's calendar. It
// matches the vocabulary dwrules' DW-005 uses, on purpose: this asks a
// DIFFERENT question of the same words.
var calendarNames = map[string]bool{"date": true, "time": true, "calendar": true}

// hasCalendarTable reports whether the schema contains a dedicated
// calendar/date/time table. A dedicated calendar is near-exclusive to analytic
// schemas: transactional systems store timestamps on rows, they do not
// materialize one row per day.
//
// HOW THIS DIFFERS FROM dwrules.timeDimensionName, which looks almost
// identical and is not: that function asks "is THIS DIMENSION a time
// dimension", so it REQUIRES a recognized role token (StripRoleToken must
// succeed) — a bare `calendar` is not a dimension it would ever be handed. The
// gate asks "does this SCHEMA contain a calendar table at all", so the role
// token is OPTIONAL and a bare `calendar` or `date_dim` both count.
//
// It composes on StripRoleToken rather than listing the spellings itself. That
// is not tidiness: a SECOND parallel role vocabulary living in dwrules is
// precisely what drifted from this package's when the role vocabulary widened,
// and turned DW-005 from a silent miss into a confident false claim over two
// real warehouses that plainly declare a calendar. A third copy here would
// re-open exactly that.
//
// The remainder is matched by EQUALITY on the separator-stripped form, never by
// containment — dim_candidate, dim_update and dim_validate all CONTAIN "date"
// once separators are dropped, and none of them is a calendar.
//
// DECLARED LIMIT: a spelled-out or qualified calendar (date_dimension,
// dim_fiscal_date, calendar_lookup) is not recognized. This signal is one of
// five and abstaining is cheap; guessing is not.
func hasCalendarTable(s *db.Schema) bool {
	for _, t := range s.Tables {
		name := t.Name
		if rest, ok := StripRoleToken(name); ok {
			name = rest
		}
		if calendarNames[normalizeGateIdent(name)] {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Signal 2 — surrogate-key vocabulary
// ---------------------------------------------------------------------------

// The _sk surrogate-key suffix is near-exclusive to warehouses (TPC-DS's
// ss_item_sk, i_item_sk). The thresholds below are what turn "a column" into "a
// convention":
//
//   - surrogateKeyColumnsMin = 3: one _sk column is an accident (someone's
//     abbreviation), two could be a composite key. Three is the smallest count
//     that reads as a naming rule someone applied.
//   - surrogateKeyTablesMin = 2: three _sk columns inside ONE table is one
//     developer's local habit. Requiring two distinct tables is what makes this
//     signal about the SCHEMA rather than about a table — which is the entire
//     reason the gate exists.
//
// Both must hold; either alone is satisfiable by a single incidental table.
const (
	surrogateKeyColumnsMin = 3
	surrogateKeyTablesMin  = 2
)

// hasSurrogateKeyVocabulary reports whether the _sk convention is used
// schema-wide.
//
// DECLARED LIMIT: only the underscore-delimited trailing segment `_sk` is
// recognized. A separator-free PascalCase spelling (CustomerSk) is NOT — no
// real corpus was observed using it, and this package's own rule about
// unobserved spellings is that they are a guess, not a convention (see
// suffixTokens). Note also that AdventureWorksDW's `CustomerKey` is a different
// convention entirely and is deliberately not counted here.
func hasSurrogateKeyVocabulary(s *db.Schema) bool {
	columns, tables := 0, 0
	for _, t := range s.Tables {
		inTable := 0
		for _, c := range t.Columns {
			if trailingSegment(c.Name) == "sk" {
				inTable++
			}
		}
		if inTable > 0 {
			columns += inTable
			tables++
		}
	}
	return columns >= surrogateKeyColumnsMin && tables >= surrogateKeyTablesMin
}

// ---------------------------------------------------------------------------
// Signal 3 — bulk-load shape
// ---------------------------------------------------------------------------

// keyLikeSegments are the trailing identifier segments that mark a column as a
// key reference.
var keyLikeSegments = map[string]bool{"key": true, "sk": true, "id": true}

// A warehouse built for load speed drops its FK constraints and keeps the
// references as plain columns. The thresholds separate that from every toy
// schema that simply never declared a constraint:
//
//   - bulkKeyColumnsInOneTableMin = 4: the load-shaped warehouse always has at
//     least one table that is MOSTLY keys — a fact row is a tuple of dimension
//     references. Four in a single table is the smallest count that cannot be
//     explained as "a primary key plus a couple of unenforced references".
//     Without this, every eight-table CRUD schema with one *_id per table
//     qualifies.
//   - bulkKeyColumnsSchemaMin = 8: keeps one wide table from carrying the whole
//     claim on its own; the convention must be visible across the schema.
//
// Both, plus zero declared foreign keys ANYWHERE, plus the proven-structure
// guard below.
const (
	bulkKeyColumnsInOneTableMin = 4
	bulkKeyColumnsSchemaMin     = 8
)

// hasBulkLoadShape reports the no-FKs-but-many-key-columns pattern.
//
// "This schema declares no foreign keys" is an ABSENCE claim, so it abstains
// whenever ANY table's structure is unproven — a dropped statement may be the
// very FK block that falsifies it. That guard is not theoretical:
// AdventureWorksDW's real DDL declares eight foreign keys that the T-SQL
// reducer currently drops, so without it this signal would confidently report
// "no foreign keys" over DDL that plainly declares them.
func hasBulkLoadShape(s *db.Schema) bool {
	schemaWide, inOneTable := 0, 0
	for _, t := range s.Tables {
		if !t.StructureProven() {
			return false
		}
		if len(t.ForeignKeys) > 0 {
			return false
		}
		n := 0
		for _, c := range t.Columns {
			if keyLikeSegments[trailingSegment(c.Name)] {
				n++
			}
		}
		schemaWide += n
		if n > inOneTable {
			inOneTable = n
		}
	}
	return inOneTable >= bulkKeyColumnsInOneTableMin && schemaWide >= bulkKeyColumnsSchemaMin
}

// ---------------------------------------------------------------------------
// Signal 4 — no audit timestamps anywhere
// ---------------------------------------------------------------------------

// hasNoAuditTimestamps reports whether NOT ONE table in the schema carries a
// created_at/updated_at audit stamp. Row-level audit stamps are an OLTP habit;
// a warehouse timestamps its LOAD, not its rows.
//
// The per-table notion and its spelling convention are dbrules' db052
// (internal/core/dbrules/rules_names.go): lowercase, drop separators, compare
// by EQUALITY to createdat/updatedat. normalizeGateIdent below is that
// convention, replicated rather than imported — internal/core/paradigm imports
// ONLY internal/core/db, and a core→core edge to dbrules to reach a four-line
// string normalizer would be a poor trade. If a third consumer appears, the
// normalizer should move to a shared home rather than be copied again.
//
// Two abstentions keep it from being vacuously true:
//   - an unproven table (a dropped ADD COLUMN might have declared created_at —
//     exactly why db052 skips such tables);
//   - a table with no columns at all, which cannot testify to an absence.
//
// DECLARED LIMIT, and the one worth reading before trusting this signal: only
// the created_at/updated_at spelling counts. Real OLTP corpora frequently use
// other names — Sakila and Pagila spell it last_update, AdventureWorks spells
// it ModifiedDate — and this signal fires on all of them. Widening the
// vocabulary would mean inventing a second spelling rule alongside db052's;
// measuring first, and deciding with the numbers, is the point of stage 1.
func hasNoAuditTimestamps(s *db.Schema) bool {
	for _, t := range s.Tables {
		if !t.StructureProven() || len(t.Columns) == 0 {
			return false
		}
		for _, c := range t.Columns {
			switch normalizeGateIdent(c.Name) {
			case "createdat", "updatedat":
				return false
			}
		}
	}
	return true
}

// ---------------------------------------------------------------------------
// Signal 5 — star topology
// ---------------------------------------------------------------------------

// starFanOutMin is the minimum number of DISTINCT referenced tables a hub needs.
// Two is the definition of hub-and-spoke: one spoke is a chain link, not a star.
const starFanOutMin = 2

// hasStarTopology reports a hub-and-spoke of depth exactly 1: some table whose
// foreign keys reach starFanOutMin or more DISTINCT tables, every one of which
// itself references nothing.
//
// The depth is the whole discriminator. An OLTP mesh runs 3+ deep — order ->
// customer -> address -> country — so its "hub" has a spoke that is not a leaf.
// A star's dimensions are terminal by construction.
//
// Two fail-closed guards, both about the SPOKES, because "the spoke references
// nothing" is the absence half of the claim:
//   - a referenced table ABSENT from the schema abstains — codefit never read
//     it and cannot show it references nothing;
//   - a referenced table whose structure is UNPROVEN abstains — a dropped
//     statement may be the FK that makes it a non-leaf.
//
// The HUB needs no such proof: a dropped statement can only UNDERcount its
// fan-out, never fabricate it — the same promotion/demotion asymmetry
// unprovableDemotions rests on.
//
// KNOWN COLLISION, stated rather than papered over: this signal fires on an
// ordinary OLTP join table. Sakila's film_actor references actor and film, and
// neither references anything — a textbook star by this definition. That is not
// a defect in the signal, it is the architectural premise restated: no single
// table tells a warehouse from a transactional schema, which is why the gate
// reports five signals instead of one.
func hasStarTopology(s *db.Schema) bool {
	byName := make(map[string]db.Table, len(s.Tables))
	for _, t := range s.Tables {
		byName[t.Name] = t
	}

	for _, hub := range s.Tables {
		spokes := make(map[string]bool, len(hub.ForeignKeys))
		for _, fk := range hub.ForeignKeys {
			spokes[fk.RefTable] = true
		}
		if len(spokes) < starFanOutMin {
			continue
		}
		if allSpokesAreLeaves(byName, spokes) {
			return true
		}
	}
	return false
}

// allSpokesAreLeaves reports whether every spoke is present, proven, and
// references nothing.
func allSpokesAreLeaves(byName map[string]db.Table, spokes map[string]bool) bool {
	for name := range spokes {
		spoke, present := byName[name]
		if !present || !spoke.StructureProven() || len(spoke.ForeignKeys) > 0 {
			return false
		}
	}
	return true
}

// ---------------------------------------------------------------------------
// Signal 6 — column-type profile split
// ---------------------------------------------------------------------------

// The other five signals read NAMES and RELATIONAL STRUCTURE. This one reads
// the one discriminating fact the neutral model already carries and no rule
// had asked for: db.Column.Type, normalized per dialect by the provider.
//
// The shapes, stated before the numbers:
//   - a FACT-like table is numeric-dominated — dimension keys plus numeric
//     measures, and near-zero descriptive text;
//   - a DIMENSION-like table is text-dominated — a key plus many descriptive
//     attributes;
//   - an ordinary OLTP table is a MIX, and usually carries datetime/bool too.
//
// THE SIGNAL IS NOT PER-TABLE, and cannot be: order_items(order_id int,
// product_id int, quantity int, price float) has an exact fact profile. It is
// the SCHEMA-LEVEL BIMODALITY — a warehouse splits into a few numeric-dominated
// tables plus several text-dominated ones; a transactional schema does not
// split, its tables are uniformly mixed.
//
// Every threshold below was chosen from a measurement over 26 public corpora
// (13 analytic, 13 transactional; the reproducible list is in ADR 0036), never
// from shape alone. Where the measurement did NOT discriminate a value, this
// file says so rather than dressing a guess as evidence.
const (
	// numericPolePct / numericPoleTextMaxPct: what "numeric-dominated" means.
	// 60% numeric measured identically to 50%, 70% and 80% over the whole
	// corpus set (only at 90% do real warehouse facts start dropping out), so
	// 60 sits in the middle of a flat region rather than on a cliff. The text
	// cap at 15% allows a fact table exactly one degenerate-dimension text
	// column in eight; at 5% two real warehouses' fact tables drop out.
	numericPolePct        = 60
	numericPoleTextMaxPct = 15
	// numericPoleMinColumns is the load-bearing threshold of this signal, and
	// the only one whose value was decided by a false positive. A narrow
	// all-numeric table is a JOIN or LINE-ITEM table, not a fact: northwind's
	// order_details (5 columns, 5 numeric), chinook's invoice_line (5/5),
	// sakila's film_actor (3/2), pagila's 22 payment partitions (6 columns, 5
	// numeric). Real fact tables measured 9 to 34 columns wide. At a floor of
	// 7 or below, pagila fires; at 4, sakila fires too; at 8, no transactional
	// corpus in the set fires at all.
	numericPoleMinColumns = 8
	// textPolePct / textPoleMinColumns: what "text-dominated" means. Half the
	// columns descriptive is the least a word like "dominated" can honestly
	// mean; measured real dimensions run 55%-85%. The 4-column floor keeps a
	// 2-column (id, name) lookup table from being descriptive by arithmetic —
	// that floor measured IDENTICALLY at 2, 3 and 4 over these corpora, so it
	// is reasoned-from-shape, not measured.
	textPolePct        = 50
	textPoleMinColumns = 4
	// numericPoleMinTables / textPoleMinTables: "a FEW numeric-dominated tables
	// plus SEVERAL text-dominated ones". One text-dominated table is a lookup
	// table and every schema has one.
	numericPoleMinTables = 1
	textPoleMinTables    = 2
	// numericPoleMinSharePct is what turns a table into a POLE. Synapse — a
	// chat server's transactional schema — has exactly one wide
	// numeric-dominated table (room_stats_current, an aggregate) among 133
	// profiled, and firing on it would be reading an outlier as a pole. 1 in
	// 133 is 0.75%; the lowest share measured on a real warehouse that this
	// signal catches is 20%. 10% has a 13x margin below the false positive and
	// a 2x margin under the true ones. It costs one real warehouse whose fact
	// pole is genuinely 1-in-16 because the corpus is mostly staging tables —
	// a deliberate false negative, in a signal that is one of six.
	numericPoleMinSharePct = 10
	// maxUnclassifiedPct is the FAIL-CLOSED budget: a table whose types the
	// parser could not classify beyond this share is not profiled at all,
	// because a proportion computed over unclassified types is a guess. This is
	// not hypothetical — the real AdventureWorksDW install script parses with
	// 359 of 359 columns at TypeUnknown, since its T-SQL brackets the type
	// names ([int], [nvarchar](50)) and the dialect type map never sees them.
	// The budget measured identically at every value from 0% to 99% over these
	// corpora (no real corpus sits between "clean" and "entirely unclassified"),
	// so 20% — one column in five — is reasoned, not measured.
	maxUnclassifiedPct = 20
	// minProfiledCoveragePct is the NO-VACUOUS-TRUTHS half, applied to a
	// DISTRIBUTIONAL claim rather than an absence one: "this SCHEMA splits into
	// two poles" cannot be concluded from a minority of its tables. Like the
	// budget above, this floor changed no measured row (0% and 90% produce the
	// same table) — it guards a shape the corpora do not exhibit.
	minProfiledCoveragePct = 50
)

// typeProfile is one table's column-type census. Only tables that clear
// profileOf's guards ever get one.
type typeProfile struct {
	columns, numeric, descriptive int
}

func (p typeProfile) numericDominated() bool {
	return p.columns >= numericPoleMinColumns &&
		p.numeric*100 >= p.columns*numericPolePct &&
		p.descriptive*100 <= p.columns*numericPoleTextMaxPct
}

func (p typeProfile) textDominated() bool {
	return p.columns >= textPoleMinColumns && p.descriptive*100 >= p.columns*textPolePct
}

// profileOf censuses t's column types, and reports ok=false when the census
// cannot be trusted.
//
// Three abstentions, all fail-closed:
//   - a table with no columns has no profile;
//   - a table whose structure is not proven complete (ADR 0034) is EXCLUDED
//     rather than poisoning the distribution — a dropped ADD COLUMN changes a
//     proportion. Note this is a per-table exclusion, not the whole-signal
//     abstention the ABSENCE-based signals use, because this claim is
//     distributional: one unreadable table does not falsify a split, it only
//     shrinks the evidence, and minProfiledCoveragePct is what keeps that
//     shrinking honest;
//   - a table over maxUnclassifiedPct is excluded.
//
// The unclassified branch is the DEFAULT of the type switch on purpose: the
// zero value ("" — not TypeUnknown — is what a Column nobody typed carries) and
// any db.Type added to the core later both land there, which is the safe
// direction for a signal that must never guess.
func profileOf(t db.Table) (typeProfile, bool) {
	if !t.StructureProven() || len(t.Columns) == 0 {
		return typeProfile{}, false
	}
	p := typeProfile{columns: len(t.Columns)}
	unclassified := 0
	for _, c := range t.Columns {
		switch c.Type {
		case db.TypeInt, db.TypeFloat:
			p.numeric++
		case db.TypeString, db.TypeText:
			p.descriptive++
		case db.TypeBool, db.TypeDateTime, db.TypeJSON, db.TypeBytes, db.TypeEnum:
			// Classified, and evidence for neither pole: a column the schema
			// spelled as a date or a flag is exactly what an ordinary OLTP
			// table carries, so it counts in the denominator and nowhere else.
		default:
			unclassified++
		}
	}
	if unclassified*100 > p.columns*maxUnclassifiedPct {
		return typeProfile{}, false
	}
	return p, true
}

// hasTypeProfileSplit reports the schema-level bimodality described above.
func hasTypeProfileSplit(s *db.Schema) bool {
	profiled, numericPole, textPole := 0, 0, 0
	for _, t := range s.Tables {
		p, ok := profileOf(t)
		if !ok {
			continue
		}
		profiled++
		switch {
		case p.numericDominated():
			numericPole++
		case p.textDominated():
			textPole++
		}
	}

	if profiled < minJudgeableTables || profiled*100 < len(s.Tables)*minProfiledCoveragePct {
		return false
	}
	if numericPole < numericPoleMinTables || numericPole*100 < profiled*numericPoleMinSharePct {
		return false
	}
	return textPole >= textPoleMinTables
}

// ---------------------------------------------------------------------------
// Shared identifier spelling
// ---------------------------------------------------------------------------

// normalizeGateIdent lowercases an identifier and drops separators, so
// createdAt / created_at both normalize to "createdat". This is dbrules'
// normalizeIdent convention (internal/core/dbrules/rules_names.go, db052),
// replicated here rather than imported to keep this package's single import
// (internal/core/db) intact.
//
// It is only ever used for WHOLE-name EQUALITY comparisons. Using it for a
// SUFFIX test would be a bug: dropping the underscore makes "risk", "disk" and
// "asterisk" all end in "sk". Suffix questions go through trailingSegment.
func normalizeGateIdent(s string) string {
	return strings.ToLower(strings.NewReplacer("_", "", "-", "").Replace(s))
}

// trailingSegment returns the last underscore-delimited segment of an
// identifier, lowercased, or "" when the identifier carries no underscore.
//
// The delimiter IS the word boundary — the same doctrine segmentRole applies to
// role tokens, and for the same reason: without it, a suffix test on the
// separator-stripped name accepts `risk` as a surrogate key and `void` as an
// id. A name with no underscore therefore has no trailing segment at all, which
// is deliberate: a bare `id` column is not evidence of anything.
func trailingSegment(name string) string {
	i := strings.LastIndex(name, "_")
	if i < 0 || i == len(name)-1 {
		return ""
	}
	return strings.ToLower(name[i+1:])
}
