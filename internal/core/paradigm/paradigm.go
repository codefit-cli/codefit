package paradigm

import (
	"strings"

	"github.com/codefit-cli/codefit/internal/core/db"
)

// Paradigm classifies a schema (or an explicit override) as analytic,
// transactional, or a mix of both. ParadigmAuto is a valid CONFIG value
// (meaning "let detection decide") but Detect itself never returns it —
// detection always resolves to one of oltp/olap/mixed.
type Paradigm string

const (
	ParadigmOLTP  Paradigm = "oltp"
	ParadigmOLAP  Paradigm = "olap"
	ParadigmMixed Paradigm = "mixed"
	// ParadigmAuto is the config sentinel for "run detection" — never a
	// Detect() result, only a valid Resolve() override input.
	ParadigmAuto Paradigm = "auto"
)

// Role classifies a single table's warehouse role: an ordinary OLTP table,
// or a table whose name carries no recognized warehouse token (see
// candidateRole) or that lacks structural corroboration, gets the explicit
// RoleUnclassified value ("unclassified") — NOT the bare Go zero value (""),
// which Detect never returns. Every entry in a Classification's Roles map is
// always one of the five named constants below.
type Role string

const (
	RoleFact         Role = "fact"
	RoleDimension    Role = "dimension"
	RoleStaging      Role = "staging"
	RoleMart         Role = "mart"
	RoleUnclassified Role = "unclassified"
)

// Classification is a schema's computed paradigm plus its per-table role map
// (keyed by db.Table.Name). Roles always has an entry for every table in the
// schema Detect ran over.
type Classification struct {
	Paradigm Paradigm
	Roles    map[string]Role

	// Unprovable names tables whose role could not be DECIDED because the
	// model does not prove their structure complete (db-model-completeness-
	// contract, design SS6). Distinct from an ordinary RoleUnclassified ("the
	// name nominates nothing"): a table demoted to RoleUnclassified is ALSO in
	// Unprovable only when its demotion cause might be a dropped statement
	// rather than a genuine structural absence. Never set for a table whose
	// name carries no recognized warehouse token (that demotion's cause is
	// vocabulary, not structure), and never set for a PROMOTED table
	// (promotion is unconditionally safe — a dropped FK can only undercount
	// fan-out/fan-in, never fabricate it).
	//
	// Membership is therefore the one PUBLIC signal that separates the two
	// demotion causes: candidateRole is unexported, and a name-demoted and a
	// structure-demoted table carry the identical RoleUnclassified. (A caller
	// can also ask StripRoleToken whether a name carries a recognized token at
	// all — but that answers a question about the NAME alone, and says nothing
	// about which of the two causes demoted a given table.)
	//
	// A THIRD demotion cause exists as of ADR 0037 and is deliberately NOT
	// recorded here: a closed schema gate. Gate.Withheld carries those, because
	// their cause is the schema-level verdict rather than anything about the
	// table's own structure, and conflating the two would make Unprovable lie
	// about why.
	Unprovable map[string]bool

	// Gate is the schema-level warehouse verdict that decided whether any role
	// in Roles could be a warehouse role at all (ADR 0037).
	Gate GateVerdict
}

// GateVerdict is the schema gate's decision plus the evidence behind it. It is
// carried on every Classification so a consumer can always be told WHY —
// codefit never reports a classification it cannot justify.
type GateVerdict struct {
	// Open is the verdict: this schema may hold warehouse roles.
	Open bool

	// ByOverride distinguishes the two ways Open becomes true. False means the
	// EVIDENCE opened it (Deciding names which signals). True means the
	// DEVELOPER opened it with an explicit database.paradigm: olap/mixed, over
	// a schema whose evidence said otherwise — a distinction the sensor's note
	// must keep, because "codefit judged this a warehouse" and "you told codefit
	// this is a warehouse" are different claims.
	ByOverride bool

	// Fired is every signal that fired, all six evaluated, in the gate's fixed
	// order. Reported whether or not the gate opened: the three excluded signals
	// are still evidence an agent may want, they just do not vote.
	Fired []Signal

	// Deciding is the subset of Fired that opened the gate — empty when the gate
	// is closed, and empty when ByOverride is true (config opened it, no signal
	// did).
	Deciding []Signal

	// Withheld names every table whose bottom-up role the CLOSED gate took away,
	// mapped to the role it would have received. Empty whenever Open is true.
	// This is the raw material for the sensor's audit trace: withholding a role
	// changes what codefit reports, so it is never silent.
	Withheld map[string]Role
}

// factFanOutMin is the minimum distinct-table FK fan-out that corroborates a
// fact-candidate table's role (design §2a: "a fact table references many dim
// tables"). Exact thresholds are TDD-refined downstream; this fixes the shape
// only.
const factFanOutMin = 2

// Detect computes a Classification as a pure function of s.
//
// TOP-DOWN, as of ADR 0037: the SCHEMA is judged first. WarehouseSignals asks
// whether schema-wide evidence makes this a warehouse at all, and only inside a
// schema that qualifies is any warehouse role assigned. When the gate is CLOSED,
// every table is RoleUnclassified — the roles that would have been assigned are
// preserved in Gate.Withheld so the decision is reportable, never silent — and
// the schema folds to oltp.
//
// This is the fix for the hole ADR 0035 documented: the sensor's 3NF
// suppression reads the PER-TABLE role, so before the gate a single table named
// dim_status could silence its own DB-002/DB-003 1NF surface inside an
// otherwise purely transactional schema. It no longer can, because the schema
// now votes first.
//
// INSIDE a qualifying schema nothing about role assignment changed. Everything
// the rest of this comment describes is exactly as it was.
//
// Table role is
// determined by the NAME as the primary signal (locked decision A5) — see
// candidateRole for the recognized spellings, which cover underscore-
// delimited leading and trailing tokens and separator-free PascalCase, all
// case-insensitively. A fact- or dimension-CANDIDATE table is further
// corroborated by REAL relational structure — a fact candidate needs FK
// fan-out to factFanOutMin+ distinct tables; a dimension candidate needs to
// be referenced (fan-in) by at least one other table — and is demoted to
// unclassified when that structural evidence is absent. A lone single-column
// (surrogate) primary key is deliberately NOT, by itself, corroboration:
// almost every ordinary OLTP table has one, so accepting it alone made the
// corroboration vacuous (CRITICAL C1, fixed post-review — see ADR 0033
// decision 2 and the S1 review ledger). Staging/mart candidates need no
// structural signal in S1 (design §2a). A table whose name nominates nothing
// is always unclassified, regardless of structure (structure corroborates a
// name, it never substitutes for one).
//
// The schema-level Paradigm folds from the resulting role mix: any
// olap-role table (fact/dimension/staging/mart) coexisting with at least one
// non-olap-role (unclassified) table yields mixed — checked FIRST, since a
// schema is not purely analytic just because it contains a star shape
// somewhere; a schema of ONLY olap-role tables with at least one fact AND one
// dimension yields olap; otherwise oltp.
func Detect(s *db.Schema) Classification {
	if s == nil {
		return Classification{Paradigm: ParadigmOLTP, Roles: map[string]Role{}}
	}

	cls := Classification{Roles: make(map[string]Role, len(s.Tables))}
	fanIn := fanInCounts(s)

	for _, t := range s.Tables {
		cls.Roles[t.Name] = roleFor(s, t, fanIn)
	}

	evidence := WarehouseSignals(s)
	cls.Gate = GateVerdict{Open: evidence.Qualifies(), Fired: evidence.Fired, Deciding: evidence.Deciding()}
	if !cls.Gate.Open {
		cls.Gate.Withheld = withholdRoles(cls.Roles)
	}

	// Unprovable answers "which demotions might be a dropped statement rather
	// than a genuine structural absence" — a question about STRUCTURE. With the
	// gate closed there is no structural demotion to explain: every table is
	// unclassified for one schema-level reason, recorded in Gate.Withheld.
	// Computing it here anyway would attribute the withholding to the parser.
	if cls.Gate.Open {
		cls.Unprovable = unprovableDemotions(s, cls.Roles)
	} else {
		cls.Unprovable = map[string]bool{}
	}
	cls.Paradigm = fold(cls.Roles)
	return cls
}

// withholdRoles strips every warehouse role out of roles IN PLACE and returns
// what it took, keyed by table name. A table the vocabulary never nominated is
// already RoleUnclassified and contributes nothing — the map names exactly the
// tables whose reported behavior the closed gate changed.
func withholdRoles(roles map[string]Role) map[string]Role {
	withheld := map[string]Role{}
	for name, r := range roles {
		if r == RoleUnclassified {
			continue
		}
		withheld[name] = r
		roles[name] = RoleUnclassified
	}
	return withheld
}

// unprovableDemotions computes Classification.Unprovable (design SS6):
// promotion is unconditionally safe (a dropped FK can only UNDERcount
// fan-out/fan-in, never fabricate it), so only a DEMOTION is ever suspect —
// and only when the table's name nominated a role in the first place (a table
// whose name nominates nothing is unclassified for a vocabulary reason, never
// a structural one). A name-recognized demotion is marked unprovable when
// EITHER the table's own structure is unproven OR any OTHER table in the
// schema is (a lost FK anywhere could be the one that would have
// referenced/been-referenced-by this table — the schema-wide half of the
// corroboration is fan-in, computed over every table's foreign keys).
func unprovableDemotions(s *db.Schema, roles map[string]Role) map[string]bool {
	anyIncomplete := false
	for _, t := range s.Tables {
		if !t.StructureProven() {
			anyIncomplete = true
			break
		}
	}

	out := map[string]bool{}
	for _, t := range s.Tables {
		if roles[t.Name] != RoleUnclassified {
			continue // promoted — unconditionally safe, never unprovable
		}
		if _, ok := candidateRole(t.Name); !ok {
			continue // the name nominates nothing — vocabulary, not structure
		}
		if !t.StructureProven() || anyIncomplete {
			out[t.Name] = true
		}
	}
	return out
}

// roleFor determines one table's role: the name nominates a CANDIDATE
// (candidateRole), which REAL relational structure must then corroborate —
// FK fan-out for a fact candidate, fan-in for a dimension candidate — never a
// lone surrogate primary key (CRITICAL C1 fix). Widening the name vocabulary
// never touches this gate: locked decision A5 holds regardless of how the
// candidate was spelled.
func roleFor(s *db.Schema, t db.Table, fanIn map[string]int) Role {
	candidate, ok := candidateRole(t.Name)
	if !ok {
		return RoleUnclassified
	}

	switch candidate {
	case RoleFact:
		if fkFanOut(t) >= factFanOutMin {
			return RoleFact
		}
		return RoleUnclassified
	case RoleDimension:
		if fanIn[t.Name] >= 1 {
			return RoleDimension
		}
		return RoleUnclassified
	default:
		// staging/mart (nominated by a leading stg_/mart_ segment only): the
		// name alone is sufficient in S1 — no structural signal is specified
		// for staging/mart tables (design §2a).
		return candidate
	}
}

// nameToken pairs a recognized warehouse naming token with the role it
// nominates. A slice, not a map, so matching order is deterministic.
type nameToken struct {
	token string
	role  Role
}

// The role-name vocabulary. Every entry is evidence-backed by the yield
// measurement over 22 real public corpora, never speculative: widening a
// vocabulary on a hunch buys coverage with false positives, and a false
// promotion here does not merely add noise — it SILENCES the table's
// DB-002/DB-003 1NF findings through the sensor's 3NF-suppression pass.
var (
	// prefixTokens match the FIRST underscore-delimited segment.
	// fct_ is dbt's own convention; f_/d_ are dw-jobtech's (d_date,
	// d_country) — recorded by the measurement both as misses AND as the
	// cause of DW-001/DW-005 false positives, since codefit reported "the
	// fact reaches no dimension" while the dimensions sat there unrecognized.
	prefixTokens = []nameToken{
		{"fact", RoleFact}, {"fct", RoleFact}, {"f", RoleFact},
		{"dim", RoleDimension}, {"d", RoleDimension},
		{"stg", RoleStaging},
		{"mart", RoleMart},
	}

	// suffixTokens match the LAST underscore-delimited segment — TPC-DS's
	// date_dim, dw-supermarket's *_fact. Deliberately narrower than
	// prefixTokens: no single-letter or staging/mart suffix was observed, and
	// an unobserved suffix is a guess, not a convention.
	suffixTokens = []nameToken{
		{"fact", RoleFact}, {"facts", RoleFact},
		{"dim", RoleDimension}, {"dims", RoleDimension},
	}

	// pascalTokens match a leading token in separator-free PascalCase —
	// Microsoft's own AdventureWorksDW spelling (FactInternetSales,
	// DimCustomer), which lowercasing alone cannot reach because
	// "factinternetsales" carries no delimited token at all.
	pascalTokens = []nameToken{
		{"fact", RoleFact},
		{"dim", RoleDimension},
	}
)

// candidateRole maps a table name to the warehouse role its NAME nominates.
// ok is false when no recognized token matches; a nominated role is only ever
// a CANDIDATE — roleFor still requires real structural corroboration before
// promoting it (locked decision A5, ADR 0033 decision 2: structure
// corroborates a name, it never substitutes for one).
//
// Three spellings are recognized, in order: an underscore-delimited leading
// token, an underscore-delimited trailing token, and separator-free
// PascalCase. Matching is case-insensitive throughout — the single
// highest-leverage gap the yield measurement found, since 4 of 9 real
// warehouse corpora used the exact Kimball convention this package already
// knew, only capitalized.
func candidateRole(name string) (Role, bool) {
	r, _, ok := roleToken(name)
	return r, ok
}

// StripRoleToken removes the recognized warehouse role token from name and
// returns the REMAINDER, preserving that remainder's original spelling
// ("D_DATE" → "DATE", "date_dim" → "date", "DimDate" → "Date"). ok is false
// when the name carries no recognized token, in exactly the cases candidateRole
// refuses — the two share one implementation, which is the entire point.
//
// WHY THIS IS EXPORTED. A second, parallel name vocabulary lived in
// internal/core/dwrules (DW-005's time-dimension list: dim_date / dim_time /
// dim_calendar) and did NOT move when this package's vocabulary widened. The
// result was measured over 22 real corpora: two warehouses spelling their
// calendar D_DATE / D_Date went from "no fact table, rule abstains" to a
// confident "this fact table reaches no time dimension" — over schemas that
// plainly declare one. Composing on this seam means the next widening here
// cannot silently re-open that hole; a copied token list would.
//
// It deliberately does NOT return the nominated Role. A caller that wanted the
// role would be doing name-only classification, and the A5 corroboration gate
// (structure corroborates a name, it never substitutes for one) is not
// something this package hands out a bypass for. Callers get the remainder, so
// they can ask their own question about the rest of the name, and nothing else.
func StripRoleToken(name string) (string, bool) {
	_, rest, ok := roleToken(name)
	return rest, ok
}

// roleToken is the single matcher behind candidateRole and StripRoleToken. It
// returns the nominated role, the name with the matched token removed, and
// whether anything matched. Three spellings, in order: underscore-delimited
// leading token, underscore-delimited trailing token, separator-free
// PascalCase.
func roleToken(name string) (Role, string, bool) {
	if r, rest, ok := segmentRole(name); ok {
		return r, rest, true
	}
	if r, rest, ok := pascalRole(name); ok {
		return r, rest, true
	}
	return RoleUnclassified, "", false
}

// segmentRole matches an underscore-delimited leading or trailing token, and
// returns the remaining segments rejoined. A name with no underscore has no
// delimited token and never matches here: the delimiter IS the word boundary
// that keeps "factory_settings" (first segment "factory") and
// "dimension_config" (first segment "dimension") out of the vocabulary.
// Without it, a bare "fact"/"dim" prefix would swallow ordinary English words
// and silence their 1NF findings.
func segmentRole(name string) (Role, string, bool) {
	segs := strings.Split(name, "_")
	if len(segs) < 2 {
		return RoleUnclassified, "", false
	}
	for _, nt := range prefixTokens {
		if strings.EqualFold(segs[0], nt.token) {
			return nt.role, strings.Join(segs[1:], "_"), true
		}
	}
	for _, nt := range suffixTokens {
		if strings.EqualFold(segs[len(segs)-1], nt.token) {
			return nt.role, strings.Join(segs[:len(segs)-1], "_"), true
		}
	}
	return RoleUnclassified, "", false
}

// pascalRole matches a leading token in separator-free PascalCase, and returns
// the rest of the name. The word boundary is PascalCase's OWN signal: the token
// is followed by an uppercase character AND THEN a lowercase one — "Fact" +
// "In" of FactInternetSales.
//
// Requiring BOTH is what makes this discriminating rather than greedy.
// "Uppercase follows the token" alone also accepts FACTORY_SETTINGS ('O' is
// uppercase), promoting an ordinary all-caps OLTP table — the spelling Oracle
// and DB2 DDL routinely use. "FACT" + "OR" fails the lowercase half, so an
// all-caps name stays unclassified: genuinely ambiguous, and this package
// declares ambiguity rather than guessing at it.
func pascalRole(name string) (Role, string, bool) {
	for _, nt := range pascalTokens {
		n := len(nt.token)
		if len(name) <= n+1 {
			continue
		}
		if !strings.EqualFold(name[:n], nt.token) {
			continue
		}
		if isUpperASCII(name[n]) && isLowerASCII(name[n+1]) {
			return nt.role, name[n:], true
		}
	}
	return RoleUnclassified, "", false
}

func isUpperASCII(c byte) bool { return c >= 'A' && c <= 'Z' }
func isLowerASCII(c byte) bool { return c >= 'a' && c <= 'z' }

// fkFanOut counts the distinct tables t references via foreign key.
func fkFanOut(t db.Table) int {
	seen := make(map[string]bool, len(t.ForeignKeys))
	for _, fk := range t.ForeignKeys {
		seen[fk.RefTable] = true
	}
	return len(seen)
}

// fanInCounts counts, for every table name in s, how many OTHER tables
// declare a foreign key referencing it.
func fanInCounts(s *db.Schema) map[string]int {
	counts := make(map[string]int, len(s.Tables))
	for _, t := range s.Tables {
		seen := make(map[string]bool, len(t.ForeignKeys))
		for _, fk := range t.ForeignKeys {
			if !seen[fk.RefTable] {
				seen[fk.RefTable] = true
				counts[fk.RefTable]++
			}
		}
	}
	return counts
}

// fold derives the schema-level Paradigm from the per-table role mix.
func fold(roles map[string]Role) Paradigm {
	var factCount, dimCount, olapCount, nonOlapCount int
	for _, r := range roles {
		switch r {
		case RoleFact:
			factCount++
			olapCount++
		case RoleDimension:
			dimCount++
			olapCount++
		case RoleStaging, RoleMart:
			olapCount++
		default:
			nonOlapCount++
		}
	}

	switch {
	case olapCount > 0 && nonOlapCount > 0:
		return ParadigmMixed
	case factCount >= 1 && dimCount >= 1:
		return ParadigmOLAP
	default:
		return ParadigmOLTP
	}
}

// Resolve applies an explicit developer override on top of detected. An empty
// or "auto" override returns detected unchanged (detection decides). An
// explicit oltp/olap/mixed override REPLACES the schema-level Paradigm — Roles
// stay detection-derived, so per-table suppression still works under an
// override that keeps a mixed reality.
//
// AND IT OUTRANKS THE SCHEMA GATE, in one direction only. This is the project's
// innegotiable rule (CLAUDE.md, "el developer decide") applied to ADR 0037:
//
//   - explicit olap or mixed is the developer ASSERTING that this is a
//     warehouse. If the gate closed, its withheld roles are RESTORED and the
//     verdict is reopened with ByOverride set. Leaving them withheld would not
//     merely keep 1NF findings — it would hand the whole DW-0xx family an
//     all-unclassified role map and silently run ZERO warehouse rules over a
//     schema the developer just declared to be a warehouse.
//   - explicit oltp is the developer asserting the opposite, and it restores
//     NOTHING. Manufacturing a warehouse role there would overrule the developer
//     in the one direction that SILENCES findings — the exact failure the gate
//     exists to close. (The sensor also short-circuits 3NF suppression on
//     explicit oltp; this is the second, independent lock on the same promise.)
//
// The EVIDENCE (Gate.Fired, Gate.Deciding) always survives an override
// unchanged: it is a measured fact about the schema, and the override changes
// only what is done with it.
func Resolve(detected Classification, override Paradigm) Classification {
	if override == "" || override == ParadigmAuto {
		return detected
	}

	out := Classification{
		Paradigm:   override,
		Roles:      detected.Roles,
		Unprovable: detected.Unprovable,
		Gate:       detected.Gate,
	}
	if (override != ParadigmOLAP && override != ParadigmMixed) || detected.Gate.Open {
		return out
	}

	restored := make(map[string]Role, len(detected.Roles))
	for name, r := range detected.Roles {
		restored[name] = r
	}
	for name, r := range detected.Gate.Withheld {
		restored[name] = r
	}
	out.Roles = restored
	out.Gate.Open = true
	out.Gate.ByOverride = true
	out.Gate.Withheld = nil
	return out
}
