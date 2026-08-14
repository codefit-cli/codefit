package db

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/codefit-cli/codefit/internal/config"
	auditctx "github.com/codefit-cli/codefit/internal/core/context"
	coredb "github.com/codefit-cli/codefit/internal/core/db"
	"github.com/codefit-cli/codefit/internal/core/dbrules"
	"github.com/codefit-cli/codefit/internal/core/dwrules"
	"github.com/codefit-cli/codefit/internal/core/findings"
	"github.com/codefit-cli/codefit/internal/core/paradigm"
	"github.com/codefit-cli/codefit/internal/core/scoring"
	"github.com/codefit-cli/codefit/internal/core/surface"
	"github.com/codefit-cli/codefit/internal/providers"
	"github.com/codefit-cli/codefit/internal/sensors"
)

// Sensor audits the database structure, driven by a schema parser resolved by the
// caller from the input's shape (.prisma / .sql — ADR 0018). The rule logic lives
// in the core (dbrules); the sensor only reads/orders the schema files and stamps
// identity. It depends on exactly the capability it uses, providers.SchemaParser —
// not LanguageProvider — so the "no parser" case is a compile-time concern of the
// adapter, not a runtime branch here.
type Sensor struct {
	parser providers.SchemaParser
}

// New builds a DB sensor backed by a schema parser.
func New(p providers.SchemaParser) *Sensor { return &Sensor{parser: p} }

// compile-time check: the DB sensor satisfies the shared Sensor identity.
var _ sensors.Sensor = (*Sensor)(nil)

func (*Sensor) Name() string                  { return "db" }
func (*Sensor) Dimension() findings.Dimension { return findings.DimensionDB }

// OwnedCategories are the baseline Item categories the DB sensor produces: its
// finding dimension ("db") plus its per-rule surface categories. They scope the
// unified baseline (ADR 0019) and must be disjoint from every other sensor's.
//
// The DW-0xx categories are APPENDED from dwrules.OwnedCategories() rather
// than re-listed here: the DW rules run INSIDE this sensor (design §2f), so
// their categories are the sensor's, and sourcing them from the rule package
// means a new DW rule cannot land with its category missing from the baseline
// scope — the one way to corrupt an existing baseline. This is the deliberate
// contrast with crossrules, whose categories are unioned in the MCP adapter
// because the cross runs outside any sensor (ADR 0029).
func (*Sensor) OwnedCategories() []string {
	own := []string{
		string(findings.DimensionDB),
		string(surface.CategoryDBFKNoIndex),
		string(surface.CategoryDBDupIndex),
		string(surface.CategoryDBMultivalued),
		string(surface.CategoryDBFKTextType),
		string(surface.CategoryDBNoTimestamps),
		string(surface.CategoryDBSensitiveUnencrypted),
		string(surface.CategoryDBRepeatingGroups),
		string(surface.CategoryDBViewSensitiveColumn),
		string(surface.CategoryDBPrefixRedundantIndex),
		string(surface.CategoryDBTableStructureUnproven),
		// Routine-body rule family (DB-030/031/040/041, 0.2.3) — pre-existing
		// gap found by the dbcoverage enforcement work
		// (db-model-completeness-contract, Control D): these four categories
		// were emitted by dbrules but never declared here, the one way to
		// corrupt an existing baseline (ADR 0019).
		string(surface.CategoryDBRoutineNoExceptionHandling),
		string(surface.CategoryDBTriggerCrossTableCascade),
		string(surface.CategoryDBTriggerExternalCall),
		string(surface.CategoryDBDynamicSQLInRoutine),
	}
	return append(own, dwrules.OwnedCategories()...)
}

// Result is the DB audit outcome, including whether the DB was measured at all.
// Measured=false with a Note is the honest "not audited" state (disabled, no
// schema_paths, provider without a schema parser) — distinct from "audited, 0
// findings".
type Result struct {
	Measured bool
	Note     string
	Res      findings.SensorResult
	// Schema is the parsed neutral schema, exposed so the MCP adapter can run the
	// code↔schema cross (crossrules) over it WITHOUT re-reading/re-parsing and
	// without importing a provider here (ADR 0029). Nil on a not-measured result.
	// The sensor stays schema-only: it merely returns what it parsed; the cross is
	// orchestrated in the adapter, never here.
	Schema *coredb.Schema
	// SchemaContent is the raw content of the parsed schema files, keyed by path —
	// exposed alongside Schema so the adapter can STAMP the cross rules' items
	// (snippet + fingerprint) the same way the sensor stamps its own, since those
	// items are produced after Audit returns and would otherwise carry no baseline
	// identity (ADR 0029). Nil on a not-measured result.
	SchemaContent map[string][]byte
}

// Run adapts Audit to the sensors.Sensor interface (for future aggregate use in
// scan-all). The standalone tool calls Audit directly to get Measured/Note.
func (s *Sensor) Run(ctx auditctx.AuditContext) (findings.SensorResult, error) {
	r, err := s.Audit(ctx)
	return r.Res, err
}

// Audit resolves the schema, runs the core DB rules, and stamps fingerprints.
// It returns Measured=false + a Note (never an error) for the honest not-measured
// cases; a hard error only for a configured-but-missing schema file or a parse
// failure — silently skipping those would be the false "all good" codefit exists
// to catch.
func (s *Sensor) Audit(ctx auditctx.AuditContext) (Result, error) {
	start := time.Now()

	if !enabled(ctx.Config) {
		return notMeasured("db sensor disabled in .codefit.yaml (sensors.db.enabled=false)"), nil
	}
	if ctx.Config == nil || len(ctx.Config.Database.SchemaPaths) == 0 {
		return notMeasured("no database.schema_paths configured in .codefit.yaml"), nil
	}

	resolution, err := readSchemaSources(ctx.ProjectRoot, ctx.Config.Database.SchemaPaths)
	if err != nil {
		return Result{}, err
	}
	content := resolution.Content

	schema, err := s.parser.ParseSchema(resolution.sources())
	if err != nil {
		return Result{}, fmt.Errorf("parsing database schema: %w", err)
	}

	// The input-level floor (unread.go), BEFORE any rule runs: a configured
	// schema input that contributed nothing to the model is declared, and a scan
	// in which NO input contributed anything is not a measurement at all. Running
	// the rules over an empty schema and reporting score 100 is the false
	// all-clear this sensor exists to make impossible.
	//
	// The unit is the CONFIGURED PATH, not the resolved file, and that is the
	// whole of ADR 0072. A resolved-file denominator is a number the resolver
	// itself decides, so a path that resolved to nothing was subtracted from both
	// sides and the floor never engaged — the case the early return above cannot
	// catch, because schema_paths WAS configured.
	unread, unproductive := unreadSources(resolution, schema)
	unreadTrace := unreadNote(unread, len(resolution.sources()), len(resolution.Paths))
	if wholeScanUnproductive(unproductive, len(resolution.Paths)) {
		return notMeasured(unreadTrace), nil
	}

	fs, surf := dbrules.Run(schema)

	// Paradigm assembly (design §2c): the sensor is the natural join point —
	// it already holds both schema AND ctx.Config, and the second input is
	// pure-core (paradigm.Detect) plus a config string, no provider crossing
	// (the deliberate divergence from ADR 0029's cross, which assembles in
	// the MCP adapter because its second input comes FROM a provider).
	override := toParadigmEnum(ctx.Config.Database.Paradigm)
	cls := paradigm.Resolve(paradigm.Detect(schema), override)
	dwF, dwS := dwrules.RunWith(schema, &cls, dwrules.All())
	fs = append(fs, dwF...)
	surf = append(surf, dwS...)

	surf, suppressed := suppress3NF(surf, cls, override)
	note := joinTraces(unreadTrace, completenessNote(schema), withheldNote(schema), schemaGateNote(cls, override), suppressed)

	stampFingerprints(fs, surf, content)

	// The sources this dimension actually READ are its audited files. Its inputs
	// are CONFIGURED (database.schema_paths), not a repository walk, so there is
	// no auditable-file census to be a denominator of — but a caller narrowing a
	// scan to a schema path still needs to be told the schema was audited rather
	// than reported as a path the audit never reached.
	auditedFiles := make([]string, 0, len(content))
	for rel := range content {
		auditedFiles = append(auditedFiles, rel)
	}
	sort.Strings(auditedFiles)

	return Result{Measured: true, Note: note, Schema: schema, SchemaContent: content, Res: findings.SensorResult{
		Sensor:       "db",
		Score:        scoring.DimensionScore(fs),
		Findings:     fs,
		Surface:      surf,
		AuditedFiles: auditedFiles,
		DurationMs:   time.Since(start).Milliseconds(),
	}}, nil
}

func notMeasured(note string) Result { return Result{Measured: false, Note: note} }

// joinTraces composes the sensor's audit traces into Result.Note (design
// SS7a). Each trace is INDEPENDENT and self-contained; a trace with nothing
// to say contributes nothing. Order is FIXED — the unread-source floor, then
// the measurement inventory, then withholding, then the schema-gate verdict,
// then suppression — so the note is deterministic and diffable, and so each
// trace qualifies the ones after it: "I read nothing at all from schema.sql"
// changes how "I could not prove 3 tables complete" should be read, which in
// turn changes how "this schema is not a warehouse" should be read, and that
// verdict is WHY the suppression line says what it says. FOUR producers share
// this one channel by construction, never by one overwriting another.
func joinTraces(traces ...string) string {
	var nonEmpty []string
	for _, t := range traces {
		if t != "" {
			nonEmpty = append(nonEmpty, t)
		}
	}
	return strings.Join(nonEmpty, " ")
}

// completenessInventoryTableCap and completenessInventoryReasonCap bound the
// per-scan measurement inventory to O(1) in schema size (design SS7a): a
// systematic parser gap across 200 tables is ONE line naming the reason and
// the count, not 200 lines.
const (
	completenessInventoryTableCap  = 5
	completenessInventoryReasonCap = 3
)

// routingClause renders the note's claim about DB-050 routing, ACCURATELY
// scoped to what actually happened (F2, 4R ledger obs #1282, CRITICAL): the
// note must never promise a surface item that does not exist. routed is how
// many of a reason group's unproven tables DB-050 actually routed (those
// with no primary key currently in the model); total is the group's size.
func routingClause(routed, total int) string {
	switch routed {
	case 0:
		return "None of them currently show a missing primary key in the model, so DB-050 did not route any " +
			"of them to a surface item — this note is their only trace in scan output."
	case total:
		return "DB-050 routed them to the db-table-structure-unproven surface items rather than affirming. " +
			"Read those items for the raw statements and their file:line."
	default:
		return fmt.Sprintf(
			"DB-050 routed %d of them (those with no primary key currently in the model) to the "+
				"db-table-structure-unproven surface items rather than affirming — read those items for the raw "+
				"statements and their file:line. The rest already show a primary key and have no further trace "+
				"beyond this note.",
			routed,
		)
	}
}

// completenessNote is the per-scan INVENTORY of what codefit could not
// measure (design SS7a, ADR 0034's measurement/diagnostics boundary). It
// aggregates by REASON, never by table, and states the fact — "codefit could
// not prove N tables complete" — never a parser diagnosis (which regex
// branch, which dialect quirk): the only inputs are t.Note (drawn from this
// package's closed Reason* vocabulary) and table names, both of which the
// core, not the provider, controls. Empty when everything was proven (never
// spammed).
func completenessNote(s *coredb.Schema) string {
	if s == nil {
		return ""
	}
	byReason := map[string][]string{}
	var reasonOrder []string
	// routedCount tracks, per reason, how many of that reason's unproven
	// tables were ACTUALLY routed by DB-050 (F2, 4R ledger obs #1282,
	// CRITICAL, verified: DB-050's own guard clause — dbrules/rules.go,
	// `if len(t.PrimaryKey) > 0 { continue }` — runs BEFORE the routing
	// check, so an unproven table that already shows a primary key in the
	// model is never routed. The note must not claim otherwise for those
	// tables; a rule about the ABSENCE of a primary key has nothing to
	// route when a key is already visible, proven or not).
	routedCount := map[string]int{}
	for _, t := range s.Tables {
		if t.StructureProven() || t.Note == "" {
			continue
		}
		if _, seen := byReason[t.Note]; !seen {
			reasonOrder = append(reasonOrder, t.Note)
		}
		byReason[t.Note] = append(byReason[t.Note], t.Name)
		if len(t.PrimaryKey) == 0 {
			routedCount[t.Note]++
		}
	}

	var parts []string
	reasonsShown := 0
	for _, reason := range reasonOrder {
		if reasonsShown >= completenessInventoryReasonCap {
			parts = append(parts, fmt.Sprintf("(+%d more reasons)", len(reasonOrder)-reasonsShown))
			break
		}
		names := byReason[reason]
		shown := names
		suffix := ""
		if len(names) > completenessInventoryTableCap {
			shown = names[:completenessInventoryTableCap]
			suffix = fmt.Sprintf(" (+%d more)", len(names)-completenessInventoryTableCap)
		}
		parts = append(parts, fmt.Sprintf(
			"codefit could not prove the structure of %d table(s) complete — %s: %s%s. "+
				"Absence-based DB/DW rules abstained on them. %s",
			len(names), reason, strings.Join(shown, ", "), suffix,
			routingClause(routedCount[reason], len(names)),
		))
		reasonsShown++
	}

	if len(s.Unreduced) > 0 {
		shown := s.Unreduced
		suffix := ""
		if len(shown) > completenessInventoryTableCap {
			suffix = fmt.Sprintf(" (+%d more)", len(shown)-completenessInventoryTableCap)
			shown = shown[:completenessInventoryTableCap]
		}
		locs := make([]string, 0, len(shown))
		for _, u := range shown {
			locs = append(locs, fmt.Sprintf("%s:%d", u.Pos.File, u.Pos.Line))
		}
		parts = append(parts, fmt.Sprintf(
			"%d statement(s) affecting an unidentified table could not be reduced (%s%s). "+
				"No table could be attributed, so no rule was gated on them.",
			len(s.Unreduced), strings.Join(locs, ", "), suffix,
		))
	}

	if len(parts) > 0 {
		// F5 (4R ledger obs #1282): "Absence-based DB/DW rules abstained on
		// them" is true for dbrules/dwrules, but a reader could reasonably
		// take it as comprehensive. It is not: internal/core/crossrules
		// (DB-010/DB-013) is a SEPARATE, absence-based rule family that runs
		// in scan-all and does NOT consult StructureProven() today — a real,
		// if bounded, limit (crossrules only emits surface, never an
		// affirmation, so the blast radius is a possible surface item over
		// unproven structure, not a false deterministic finding). Stated
		// once, not per reason group, so it never spams a clean scan and
		// never repeats per reason (ADR 0018's corollary: a declared limit
		// must be machine-visible, not left to prose alone — locked by
		// TestSensorDB_CompletenessNote_DeclaresCrossrulesExceptionWhenNonEmpty).
		parts = append(parts, "Declared exception: the code×schema cross (DB-010/DB-013) does not consult "+
			"this completeness signal and may still emit surface items over these tables.")
	}

	return strings.Join(parts, " ")
}

// withheldNote is the per-scan trace of what codefit RECOGNIZED and
// deliberately did not model (ADR 0043). It is a SEPARATE trace from
// completenessNote, and deliberately so: that one reports what codefit could
// not measure, this one reports a scoping DECISION. Merging them would tell an
// agent the parser failed where it in fact succeeded — a lie in the opposite
// direction from the silence this slice removed.
//
// It obeys the same three disciplines as the measurement inventory: aggregate
// by REASON (a migration suite that stages 200 temporary tables is one line,
// not 200), state the FACT and never a parser diagnosis (the only inputs are
// the core's closed WithheldReason vocabulary and names read from the user's own
// DDL), and stay EMPTY when there is nothing to say.
//
// Withholding is never silent because the developer decides, not codefit
// (CLAUDE.md): the consequence — these declarations reached no rule at all —
// is stated, not implied.
func withheldNote(s *coredb.Schema) string {
	if s == nil || len(s.Withheld) == 0 {
		return ""
	}
	byReason := map[coredb.WithheldReason][]string{}
	var reasonOrder []coredb.WithheldReason
	for _, w := range s.Withheld {
		if _, seen := byReason[w.Reason]; !seen {
			reasonOrder = append(reasonOrder, w.Reason)
		}
		// A form whose name the parser cannot read faithfully is identified by
		// its POSITION instead of by a guessed name (ADR 0043's fabrication
		// boundary) — the agent can still open the file and read the DDL.
		label := w.Name
		if label == "" {
			label = fmt.Sprintf("%s:%d", w.Pos.File, w.Pos.Line)
		}
		byReason[w.Reason] = append(byReason[w.Reason], label)
	}

	var parts []string
	for i, reason := range reasonOrder {
		if i >= completenessInventoryReasonCap {
			parts = append(parts, fmt.Sprintf("(+%d more reasons)", len(reasonOrder)-i))
			break
		}
		names := byReason[reason]
		shown, suffix := names, ""
		if len(names) > completenessInventoryTableCap {
			shown = names[:completenessInventoryTableCap]
			suffix = fmt.Sprintf(" (+%d more)", len(names)-completenessInventoryTableCap)
		}
		parts = append(parts, fmt.Sprintf(
			"codefit read %d table declaration(s) and deliberately did NOT model them — %s: %s%s. "+
				"They are absent from the schema every DB and DW rule reads, so no rule saw them at all.",
			len(names), reason, strings.Join(shown, ", "), suffix,
		))
	}
	return strings.Join(parts, " ")
}

// gateNoteTableCap bounds the table list in the schema-gate trace, the same way
// completenessInventoryTableCap bounds the measurement inventory: a warehouse
// vocabulary applied across 200 tables is ONE line naming a count and a sample,
// never 200 names.
const gateNoteTableCap = 5

// schemaGateNote is the audit trace for the schema-level warehouse verdict
// (ADR 0037). The gate changes what codefit reports, so it never decides
// silently — "siempre se informan las consecuencias" (CLAUDE.md).
//
// Three states, and each is a different claim:
//
//   - CLOSED with roles withheld. This is the one a developer cannot otherwise
//     discover: the consequence is items that WOULD have been suppressed simply
//     appearing, which looks like nothing happened. The trace states the count,
//     names the tables, names the three deciding signals it looked for, and
//     names the escape hatch — database.paradigm: olap or mixed, the setting
//     that reopens the gate (paradigm.Resolve).
//   - OPEN on evidence. codefit concluded "warehouse"; it must be able to say
//     WHICH signals concluded it. This sentence is the reason the verdict names
//     signals rather than counting them — a score could not be rendered into
//     anything an agent can check.
//   - OPEN by explicit config. The developer asserted it and won. That is a
//     different claim from codefit having judged it, and the two must not read
//     alike.
//
// Empty when the gate changed nothing: no role withheld and none granted. Every
// ordinary transactional scan lands there, and a line on each of those would be
// pure noise.
func schemaGateNote(cls paradigm.Classification, override paradigm.Paradigm) string {
	if !cls.Gate.Open {
		if len(cls.Gate.Withheld) == 0 {
			return ""
		}
		return fmt.Sprintf(
			"Schema gate: this schema shows none of the three deciding warehouse signals "+
				"(calendar_table, surrogate_key_names, type_profile_split), so it is not classified as a "+
				"warehouse and the warehouse role of %d %s was withheld (%s). Their DB-002/DB-003 1NF "+
				"surface was NOT suppressed; set database.paradigm: olap (or mixed) if this really is a "+
				"warehouse.",
			len(cls.Gate.Withheld), tableWord(len(cls.Gate.Withheld)), sampleTables(namesOf(cls.Gate.Withheld)),
		)
	}

	holders := warehouseRoleTables(cls)
	if len(holders) == 0 {
		return ""
	}
	if cls.Gate.ByOverride {
		return fmt.Sprintf(
			"Schema gate: no deciding warehouse signal fired on this schema, but the explicit "+
				"database.paradigm: %s setting wins over detection, so %d %s kept a warehouse role (%s).",
			override, len(holders), tableWord(len(holders)), sampleTables(holders),
		)
	}
	return fmt.Sprintf(
		"Schema gate: this schema is classified as a warehouse on the deciding signal(s) %s, so %d %s "+
			"hold a warehouse role (%s).",
		joinSignals(cls.Gate.Deciding), len(holders), tableWord(len(holders)), sampleTables(holders),
	)
}

// warehouseRoleTables lists, sorted, every table that ended up holding a
// warehouse role — the tables whose reporting the OPEN gate changed.
func warehouseRoleTables(cls paradigm.Classification) []string {
	var out []string
	for name, role := range cls.Roles {
		if role != paradigm.RoleUnclassified {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}

// namesOf returns the sorted keys of a role map. Sorted because a note built
// from map iteration order is not diffable.
func namesOf(roles map[string]paradigm.Role) []string {
	out := make([]string, 0, len(roles))
	for name := range roles {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// sampleTables renders at most gateNoteTableCap names and says how many were
// elided, so the line stays O(1) in schema size without ever pretending the list
// was complete.
func sampleTables(names []string) string {
	if len(names) <= gateNoteTableCap {
		return strings.Join(names, ", ")
	}
	return fmt.Sprintf("%s, and %d more", strings.Join(names[:gateNoteTableCap], ", "), len(names)-gateNoteTableCap)
}

func joinSignals(sigs []paradigm.Signal) string {
	out := make([]string, 0, len(sigs))
	for _, s := range sigs {
		out = append(out, string(s))
	}
	return strings.Join(out, ", ")
}

func tableWord(n int) string {
	if n == 1 {
		return "table"
	}
	return "tables"
}

// suppress3NF drops DB-002 (CategoryDBMultivalued) / DB-003
// (CategoryDBRepeatingGroups) surface items on OLAP-classified tables (design
// §2e, spec "3NF-Suppression on OLAP-Classified Schemas"). DB-002/DB-003
// themselves stay schema-only in dbrules (ADR-0015 untouched, no signature
// change, no provider) — this is a sensor-level pass over the already-merged
// surface, the layer that legitimately knows the paradigm.
//
// override (the RAW config value BEFORE Resolve, distinct from cls.Paradigm)
// decides the suppression MODE, honoring developer autonomy — explicit
// config always wins over detection:
//   - explicit "oltp": NEVER suppress, regardless of any detected role (a
//     dev who explicitly declares oltp gets the classic 1NF checks
//     unconditionally, even on a fact_/dim_-prefixed schema).
//   - explicit "olap": DROP schema-wide, regardless of role (the dev asserts
//     the WHOLE schema is a warehouse — even an unclassified-role table's
//     item is dropped).
//   - auto/empty or explicit "mixed": per-table role — drop only tables
//     whose cls.Roles entry is fact/dimension/mart; a table with no
//     recognized role (unclassified/ordinary OLTP) keeps firing unchanged,
//     preventing over-suppression on a mixed schema (design §7 boundary
//     risk #3).
//
// R2 audit trace (WARNING, S1 review ledger — "siempre se informan las
// consecuencias", CLAUDE.md): suppression is never silent. When one or more
// items are withheld, the returned note records how many and on how many
// OLAP-classified tables, and points at database.paradigm: oltp to reveal
// them. The note is empty (never spammed) when nothing was suppressed.
func suppress3NF(surf []findings.SurfaceItem, cls paradigm.Classification, override paradigm.Paradigm) ([]findings.SurfaceItem, string) {
	if override == paradigm.ParadigmOLTP {
		return surf, ""
	}

	suppressedCategory := func(c string) bool {
		return c == string(surface.CategoryDBMultivalued) || c == string(surface.CategoryDBRepeatingGroups)
	}

	dropAll := override == paradigm.ParadigmOLAP

	kept := surf[:0:0] //nolint:gocritic // deliberate zero-length, non-aliased slice: never mutate surf in place
	droppedCount := 0
	droppedTables := map[string]bool{}
	for _, it := range surf {
		if suppressedCategory(it.Category) {
			if dropAll {
				droppedCount++
				droppedTables[itemTable(it)] = true
				continue
			}
			if role, ok := tableRole(it, cls); ok && isOLAPRole(role) {
				droppedCount++
				droppedTables[itemTable(it)] = true
				continue
			}
		}
		kept = append(kept, it)
	}

	if droppedCount == 0 {
		return kept, ""
	}
	return kept, suppressionNote(droppedCount, len(droppedTables))
}

// itemTable extracts a surface item's "table: <name>" StructuralSignal —
// the same signal tableRole resolves a role from. Empty when absent (should
// not happen for DB-002/DB-003, which always carry it).
func itemTable(it findings.SurfaceItem) string {
	for _, sig := range it.StructuralSignals {
		if table, found := strings.CutPrefix(sig, "table: "); found {
			return table
		}
	}
	return ""
}

// suppressionNote renders the R2 audit trace: a factual, minimal statement
// of what 3NF-suppression withheld and how to see it.
func suppressionNote(items, tables int) string {
	itemWord := "item"
	if items != 1 {
		itemWord += "s"
	}
	tableWord := "table"
	if tables != 1 {
		tableWord += "s"
	}
	return fmt.Sprintf(
		"3NF-suppression withheld %d 1NF surface %s (DB-002/DB-003) on %d OLAP-classified %s "+
			"(fact/dimension/mart); set database.paradigm: oltp to see them.",
		items, itemWord, tables, tableWord,
	)
}

// tableRole resolves a surface item's table via its "table: <name>"
// StructuralSignal (added to DB-002/DB-003 for exactly this purpose) and
// looks it up in cls.Roles. ok is false when the item carries no table
// signal or the table has no role entry.
func tableRole(it findings.SurfaceItem, cls paradigm.Classification) (paradigm.Role, bool) {
	for _, sig := range it.StructuralSignals {
		table, found := strings.CutPrefix(sig, "table: ")
		if !found {
			continue
		}
		role, ok := cls.Roles[table]
		return role, ok
	}
	return "", false
}

func isOLAPRole(r paradigm.Role) bool {
	return r == paradigm.RoleFact || r == paradigm.RoleDimension || r == paradigm.RoleMart
}

// toParadigmEnum maps ctx.Config.Database.Paradigm to a paradigm.Paradigm —
// identity, since config validation already restricts the value to
// auto/oltp/olap/mixed/"" and paradigm.Resolve treats ""/"auto" as
// no-override. This is the ONE place config meets core, keeping the
// paradigm/dwrules packages config-free (design §2c).
func toParadigmEnum(s string) paradigm.Paradigm { return paradigm.Paradigm(s) }

// enabled applies the three-state toggle: unset (nil) → on (opt-out default),
// explicit true/false overrides.
func enabled(cfg *config.Config) bool {
	if cfg == nil || cfg.Sensors.DB.Enabled == nil {
		return true
	}
	return *cfg.Sensors.DB.Enabled
}

// stampFingerprints assigns baseline identity at the single sensor boundary (as the
// security sensor does): a finding is hashed from its rule ID + the source line at
// its position; a surface item from its snippet (filled from the source line when
// the rule left it empty). It also stamps the stable surface IDs. The schema
// content never leaves codefit unhashed.
func stampFingerprints(fs []findings.Finding, surf []findings.SurfaceItem, content map[string][]byte) {
	for i := range fs {
		if fs[i].Fingerprint == "" {
			fs[i].Fingerprint = findings.Fingerprint(string(fs[i].Dimension)+"/"+fs[i].ID, fs[i].File, lineAt(content[fs[i].File], fs[i].Line))
		}
	}
	StampSurface(surf, content)
}

// StampSurface fills the snippet (from the source line at the item's position),
// the baseline fingerprint, and the stable id of any surface item that lacks them.
// It is exported so the MCP adapter can stamp the cross rules' items (produced
// after Audit) with the SAME discipline the sensor uses on its own — one
// stamping, no drift (ADR 0029). Idempotent: an item already stamped is untouched.
func StampSurface(surf []findings.SurfaceItem, content map[string][]byte) {
	for i := range surf {
		if surf[i].Snippet == "" {
			surf[i].Snippet = strings.TrimSpace(lineAt(content[surf[i].File], surf[i].Line))
		}
		if surf[i].Fingerprint == "" {
			surf[i].Fingerprint = findings.Fingerprint(surf[i].Category, surf[i].File, surf[i].Snippet)
		}
	}
	surface.StampIDs(surf)
}

// lineAt returns the 1-based n-th line of content (empty when out of range).
func lineAt(content []byte, n int) string {
	if n <= 0 || len(content) == 0 {
		return ""
	}
	lines := strings.Split(string(content), "\n")
	if n > len(lines) {
		return ""
	}
	return lines[n-1]
}
