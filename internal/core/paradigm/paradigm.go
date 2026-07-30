package paradigm

import "github.com/codefit-cli/codefit/internal/core/db"

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
// or a table with no fact_/dim_/stg_/mart_ prefix or insufficient structural
// corroboration, gets the explicit RoleUnclassified value ("unclassified") —
// NOT the bare Go zero value (""), which Detect never returns. Every entry
// in a Classification's Roles map is always one of the five named constants
// below.
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
	// contract, design SS6). Distinct from an ordinary RoleUnclassified ("no
	// recognized prefix"): a table demoted to RoleUnclassified is ALSO in
	// Unprovable only when its demotion cause might be a dropped statement
	// rather than a genuine structural absence. Never set for a table with no
	// recognized prefix (that demotion's cause is vocabulary, not
	// structure), and never set for a PROMOTED table (promotion is
	// unconditionally safe — a dropped FK can only undercount fan-out/fan-in,
	// never fabricate it).
	Unprovable map[string]bool
}

// factFanOutMin is the minimum distinct-table FK fan-out that corroborates a
// fact_-prefixed table's candidate role (design §2a: "a fact table
// references many dim tables"). Exact thresholds are TDD-refined downstream;
// this fixes the shape only.
const factFanOutMin = 2

// Detect computes a Classification as a pure function of s. Table role is
// determined by NAME PREFIX as the primary signal (fact_/dim_/stg_/mart_,
// locked decision A5); a prefixed fact_/dim_ table is further corroborated
// by REAL relational structure — fact_ needs FK fan-out to factFanOutMin+
// distinct tables; dim_ needs to be referenced (fan-in) by at least one
// other table — and demoted to unclassified when that structural evidence
// is absent. A lone single-column (surrogate) primary key is deliberately
// NOT, by itself, corroboration: almost every ordinary OLTP table has one,
// so accepting it alone made the corroboration vacuous (CRITICAL C1, fixed
// post-review — see ADR 0033 decision 2 and the S1 review ledger). stg_/
// mart_ need no structural signal in S1 (design §2a). A table with no
// recognized prefix is always unclassified, regardless of structure
// (name-only demotion never promotes: structure corroborates, it never
// substitutes for the name).
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

	cls.Unprovable = unprovableDemotions(s, cls.Roles)
	cls.Paradigm = fold(cls.Roles)
	return cls
}

// unprovableDemotions computes Classification.Unprovable (design SS6):
// promotion is unconditionally safe (a dropped FK can only UNDERcount
// fan-out/fan-in, never fabricate it), so only a DEMOTION is ever suspect —
// and only when the table carried a recognized prefix in the first place (a
// table with no recognized prefix is unclassified for a vocabulary reason,
// never a structural one). A recognized-prefix demotion is marked unprovable
// when EITHER the table's own structure is unproven OR any OTHER table in
// the schema is (a lost FK anywhere could be the one that would have
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
		if _, ok := prefixRole(t.Name); !ok {
			continue // no recognized prefix — vocabulary, not structure
		}
		if !t.StructureProven() || anyIncomplete {
			out[t.Name] = true
		}
	}
	return out
}

// roleFor determines one table's role: name prefix as the primary signal,
// corroborated by REAL relational structure — FK fan-out for fact_, fan-in
// for dim_ — never by a lone surrogate primary key alone (CRITICAL C1 fix).
func roleFor(s *db.Schema, t db.Table, fanIn map[string]int) Role {
	candidate, ok := prefixRole(t.Name)
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
		// stg_/mart_: name alone is sufficient in S1 — no structural signal
		// is specified for staging/mart tables (design §2a).
		return candidate
	}
}

// prefixRole maps a table name's prefix to its candidate role. ok is false
// when no recognized prefix matches.
func prefixRole(name string) (Role, bool) {
	switch {
	case hasPrefix(name, "fact_"):
		return RoleFact, true
	case hasPrefix(name, "dim_"):
		return RoleDimension, true
	case hasPrefix(name, "stg_"):
		return RoleStaging, true
	case hasPrefix(name, "mart_"):
		return RoleMart, true
	default:
		return RoleUnclassified, false
	}
}

func hasPrefix(name, prefix string) bool {
	return len(name) >= len(prefix) && name[:len(prefix)] == prefix
}

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

// Resolve applies an explicit developer override on top of detected. An
// empty or "auto" override returns detected unchanged (detection decides).
// An explicit oltp/olap/mixed override REPLACES the schema-level Paradigm
// only — Roles stay detection-derived, so per-table suppression still works
// under an override that keeps a mixed reality (developer autonomy:
// explicit config always wins, but it does not erase the structural facts
// Roles carries).
func Resolve(detected Classification, override Paradigm) Classification {
	if override == "" || override == ParadigmAuto {
		return detected
	}
	return Classification{Paradigm: override, Roles: detected.Roles, Unprovable: detected.Unprovable}
}
