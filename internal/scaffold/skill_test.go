package scaffold_test

import (
	"strings"
	"testing"

	"github.com/codefit-cli/codefit/internal/config"
	"github.com/codefit-cli/codefit/internal/providers/registry"
	"github.com/codefit-cli/codefit/internal/scaffold"
	"gopkg.in/yaml.v3"
)

// renderSkill renders the skill for a typescript project and splits it into its
// YAML frontmatter and markdown body.
func renderSkill(t *testing.T, info scaffold.ProjectInfo) (front map[string]any, body string) {
	t.Helper()
	out, err := scaffold.RenderSkill(info)
	if err != nil {
		t.Fatalf("RenderSkill: %v", err)
	}
	s := string(out)
	if !strings.HasPrefix(s, "---\n") {
		t.Fatalf("skill must start with YAML frontmatter, got:\n%s", s)
	}
	rest := s[len("---\n"):]
	end := strings.Index(rest, "\n---\n")
	if end < 0 {
		t.Fatalf("skill frontmatter is not closed with ---, got:\n%s", s)
	}
	if err := yaml.Unmarshal([]byte(rest[:end]), &front); err != nil {
		t.Fatalf("frontmatter is not valid YAML: %v", err)
	}
	return front, rest[end+len("\n---\n"):]
}

func tsInfo() scaffold.ProjectInfo {
	return scaffold.ProjectInfo{Name: "bitacora", Language: "typescript", Framework: "next", ORM: "prisma"}
}

// The frontmatter must satisfy the Anthropic Agent Skills spec: name + description
// required, name == the skill/dir identifier.
func TestSkillFrontmatterValid(t *testing.T) {
	front, _ := renderSkill(t, tsInfo())

	name, _ := front["name"].(string)
	if name != scaffold.SkillName {
		t.Errorf("frontmatter name = %q, want %q", name, scaffold.SkillName)
	}
	desc, _ := front["description"].(string)
	if desc == "" {
		t.Errorf("frontmatter description is required and must be non-empty")
	}
	if len(desc) > 1024 {
		t.Errorf("description length = %d, must be <= 1024 (spec limit)", len(desc))
	}
	// The trigger words must lead the description for progressive disclosure.
	low := strings.ToLower(desc)
	for _, kw := range []string{"audit", "security", "idor"} {
		if !strings.Contains(low, kw) {
			t.Errorf("description should contain trigger word %q, got %q", kw, desc)
		}
	}
}

func TestSkillBodyHasLoopEssentials(t *testing.T) {
	_, body := renderSkill(t, tsInfo())

	for _, must := range []string{
		"codefit-scan-all",
		"codefit-scan-endpoint",
		"actionable",
		"resolved_clean",
		"frontier_pending",
	} {
		if !strings.Contains(body, must) {
			t.Errorf("skill body is missing %q", must)
		}
	}
	// The golden rule learned in dogfooding: frontier is not clean.
	if !strings.Contains(strings.ToLower(body), "not concluded") {
		t.Errorf("skill body must carry the golden rule that 'not concluded' is not 'clean'")
	}
}

// The detected language must be baked into the example commands so they are
// copy-paste exact for this project.
func TestSkillBakesLanguage(t *testing.T) {
	_, body := renderSkill(t, tsInfo())
	if !strings.Contains(body, `"typescript"`) {
		t.Errorf("skill body must bake the detected language into the commands, got:\n%s", body)
	}
}

// The skill must teach the baseline loop, including the human-only safeguard for
// accept and the rule that deterministic findings are not auto-silenced.
func TestSkillTeachesBaselineLoop(t *testing.T) {
	_, body := renderSkill(t, tsInfo())
	for _, must := range []string{
		"codefit-baseline-list",
		"codefit-baseline-accept",
		"codefit-baseline-prune",
		".codefit-baseline",
		"gone_candidates",
	} {
		if !strings.Contains(body, must) {
			t.Errorf("skill must mention %q", must)
		}
	}
	low := strings.ToLower(body)
	if !strings.Contains(low, "never accept an item on your own") {
		t.Errorf("skill must carry the human-only safeguard for accept")
	}
	if !strings.Contains(low, "not auto-silenced") {
		t.Errorf("skill must say deterministic findings are not auto-silenced")
	}
}

// The skill must be IMPERATIVE about using codefit rather than auditing by hand —
// density of signal, so even a small model triggers the tool instead of reading
// files manually.
func TestSkillIsImperative(t *testing.T) {
	_, body := renderSkill(t, tsInfo())
	low := strings.ToLower(body)
	if !strings.Contains(low, "must call") || !strings.Contains(low, "codefit-scan-all") {
		t.Errorf("skill must imperatively tell the agent to call codefit-scan-all first, got:\n%s", body)
	}
	if !strings.Contains(low, "do not audit by reading files") && !strings.Contains(low, "not audit by reading files") {
		t.Errorf("skill must tell the agent NOT to audit by reading files manually, got:\n%s", body)
	}
}

// The skill must teach the custom authz-helper registration, with the human-only
// safeguard and the fact-not-verdict nuance (clears authz, not IDOR/ownership).
func TestSkillTeachesCustomAuthzHelpers(t *testing.T) {
	_, body := renderSkill(t, tsInfo())
	low := strings.ToLower(body)
	if !strings.Contains(body, "codefit-baseline-register-authz-helper") {
		t.Errorf("skill must mention the register-authz-helper tool")
	}
	if !strings.Contains(low, "never register a helper") {
		t.Errorf("skill must carry the human-only safeguard for registering helpers")
	}
	if !strings.Contains(low, "ownership") {
		t.Errorf("skill must say registering clears authz but not the IDOR/ownership gap, got:\n%s", body)
	}
}

// The database dimension shipped across v0.2.0–v0.2.5 while the skill still
// described only endpoint security. The skill must teach scan-db, and it must
// teach the honest-abstention contract — `measured: false` is codefit saying it
// could not read the schema, which is the opposite of "clean".
func TestSkillTeachesDatabaseDimension(t *testing.T) {
	_, body := renderSkill(t, tsInfo())
	for _, must := range []string{
		"codefit-scan-db",
		"measured",
		"schema_paths",
	} {
		if !strings.Contains(body, must) {
			t.Errorf("skill must teach the database dimension: missing %q", must)
		}
	}
	low := strings.ToLower(body)
	if !strings.Contains(low, "never a clean") && !strings.Contains(low, "not \"clean\"") && !strings.Contains(low, "not clean") {
		t.Errorf("skill must say measured:false is NOT a clean result, got:\n%s", body)
	}
}

// The DB section is rendered UNCONDITIONALLY. Detect() only recognizes a database
// through enrichTypeScript (Prisma/Drizzle/TypeORM) — SQL-DDL/Flyway migrations
// are not detected at all — so gating the section on detection would hide the
// dimension on exactly the projects the SQL-DDL parser was built for.
func TestSkillTeachesDatabaseEvenWhenNoORMDetected(t *testing.T) {
	_, body := renderSkill(t, scaffold.ProjectInfo{Name: "flyway-app", Language: "go"})
	if !strings.Contains(body, "codefit-scan-db") {
		t.Errorf("the DB section must render with no ORM detected (SQL-DDL projects are undetectable today), got:\n%s", body)
	}
}

// Progressive disclosure loads the skill from the frontmatter alone. A description
// that speaks only of endpoints means a database task never loads the skill at
// all — the agent does not see an incomplete skill, it sees none.
func TestSkillDescriptionTriggersOnDatabaseWork(t *testing.T) {
	front, _ := renderSkill(t, tsInfo())
	desc, _ := front["description"].(string)
	low := strings.ToLower(desc)
	if !strings.Contains(low, "database") && !strings.Contains(low, "schema") {
		t.Errorf("description must also trigger on database/schema work, got %q", desc)
	}
}

// Dependency CVEs are unreachable from scan-all (it does not run them), so an
// agent that only knows scan-all never checks them. The coverage manifest is how
// the agent learns codefit's declared limits instead of assuming them.
func TestSkillTeachesCVEsAndCoverage(t *testing.T) {
	_, body := renderSkill(t, tsInfo())
	for _, must := range []string{"codefit-check-cves", "codefit-coverage"} {
		if !strings.Contains(body, must) {
			t.Errorf("skill must mention %q", must)
		}
	}
}

// TestSkillStatesSurfaceReachBeforeInstalling is R4/"Also in scope"
// (docs/specs/declared-partial-language-exposure.md): the generated skill —
// read by the agent BEFORE it audits anything — must state the language's
// surface reach as an N-of-M claim, the same shape as the per-dimension
// reach statement in README, DERIVED from Capability()/ProviderCategories,
// never a hardcoded per-language sentence. TypeScript maps every category
// (4 of 4); Go maps only authz (1 of 4) — the two must read differently.
func TestSkillStatesSurfaceReachBeforeInstalling(t *testing.T) {
	_, tsBody := renderSkill(t, tsInfo())
	if !strings.Contains(tsBody, "4 of 4") {
		t.Errorf("typescript's skill must state its 4-of-4 surface reach, got:\n%s", tsBody)
	}

	_, goBody := renderSkill(t, scaffold.ProjectInfo{Name: "x", Language: "go"})
	if !strings.Contains(goBody, "1 of 4") {
		t.Errorf("go's skill must state its 1-of-4 surface reach, got:\n%s", goBody)
	}
	low := strings.ToLower(goBody)
	for _, notMapped := range []string{"idor", "overfetch", "nplus1"} {
		if !strings.Contains(low, notMapped) {
			t.Errorf("go's skill must name the unmapped surface category %q, got:\n%s", notMapped, goBody)
		}
	}
}

// TestSkillNeverFabricatesALanguage replaces TestSkillEmptyLanguageDefaults,
// which PINNED the defect: RenderSkill silently baked `language: "typescript"`
// whenever Language was empty.
//
// The skill is the FIRST artifact an agent reads, before any tool description,
// and its examples are copy-paste instructions. Baking a language codefit never
// detected tells the agent to scan a Java repository as TypeScript — a
// fabrication in the one file whose whole job is to be trusted verbatim.
//
// Both inputs a caller can supply for "no language" are covered: the sentinel
// Detect really returns, and the zero value an external caller can still pass.
func TestSkillNeverFabricatesALanguage(t *testing.T) {
	for _, lang := range []string{config.LanguageUndetected, ""} {
		t.Run("language="+lang, func(t *testing.T) {
			_, body := renderSkill(t, scaffold.ProjectInfo{Name: "x", Language: lang})
			for _, fabricated := range []string{"typescript", "go", "java", "python"} {
				if strings.Contains(body, `"`+fabricated+`"`) {
					t.Errorf("skill baked language %q for an undetected project — codefit never detected it, got:\n%s",
						fabricated, body)
				}
			}
			if strings.Contains(body, `"`+config.LanguageUndetected+`"`) {
				t.Errorf("skill passes the sentinel as a tool argument; it resolves no provider, got:\n%s", body)
			}
		})
	}
}

// TestSkillUndetectedStatesTheDBOnlyScope: an agent must not be left to guess
// why the language examples are missing. The skill states that no language
// provider resolved, that code scanning does not run, and that the DB dimension
// still applies — with the marker names taken from the registry.
// Both spellings of "no language" are covered. The sentinel is what Detect
// returns; "" is what an external caller can still pass, and it is the input the
// deleted fallback existed to paper over — rendering the code-scanning body with
// `language: ""` baked into every example would just be a different fabrication.
func TestSkillUndetectedStatesTheDBOnlyScope(t *testing.T) {
	for _, lang := range []string{config.LanguageUndetected, ""} {
		t.Run("language="+lang, func(t *testing.T) {
			_, body := renderSkill(t, scaffold.ProjectInfo{Name: "x", Language: lang})
			low := strings.ToLower(body)

			if !strings.Contains(body, "codefit-scan-db") || !strings.Contains(body, "schema_paths") {
				t.Errorf("the undetected skill must still teach the DB dimension and how to turn it on, got:\n%s", body)
			}
			if !strings.Contains(low, "no language provider") && !strings.Contains(low, "did not detect") {
				t.Errorf("the undetected skill must state that no language provider resolved, got:\n%s", body)
			}
			markers := registry.InitDetectMarkerFiles()
			if len(markers) == 0 {
				t.Fatal("no InitDetect markers registered; the loop below would pass vacuously")
			}
			for _, m := range markers {
				if !strings.Contains(body, m) {
					t.Errorf("the undetected skill must name the marker %q codefit looks for, got:\n%s", m, body)
				}
			}
			// A code-scanning instruction the agent cannot carry out is
			// worse than its absence: it would call the tool, get a null
			// security dimension, and have to work out why.
			if strings.Contains(body, "codefit-scan-endpoint") {
				t.Errorf("the undetected skill must not instruct the agent to scan endpoints; no provider resolves, got:\n%s", body)
			}
			if strings.Contains(body, "language`: ") || strings.Contains(body, "language: \"") {
				t.Errorf("the undetected skill must not spell a `language` argument at all, got:\n%s", body)
			}
		})
	}
}

// TestSkillDescriptionSurvivesUndetection guards the one thing that must NOT be
// gated. Progressive disclosure loads the skill from the frontmatter alone: if
// the description stopped naming database and schema triggers for an undetected
// project, a schema task would never load the skill at all. The agent would not
// see a narrower skill — it would see none.
func TestSkillDescriptionSurvivesUndetection(t *testing.T) {
	front, _ := renderSkill(t, scaffold.ProjectInfo{Name: "x", Language: config.LanguageUndetected})
	desc, _ := front["description"].(string)
	if desc == "" {
		t.Fatal("frontmatter description is required and must be non-empty")
	}
	low := strings.ToLower(desc)
	for _, trigger := range []string{"database", "schema", "audit"} {
		if !strings.Contains(low, trigger) {
			t.Errorf("the description must keep trigger %q even when no language was detected, got %q", trigger, desc)
		}
	}
}

// HONESTY: resolved_clean must NOT claim codefit "verified it clean" (overstates
// the guarantee). It says the expected check is present locally but its
// sufficiency was not verified — same nuance as the verification facts.
func TestSkillResolvedCleanDoesNotOverpromise(t *testing.T) {
	_, body := renderSkill(t, tsInfo())
	low := strings.ToLower(body)
	if strings.Contains(low, "verified it clean") || strings.Contains(low, "verified clean") {
		t.Errorf("resolved_clean must not over-promise 'verified clean':\n%s", body)
	}
	if !strings.Contains(low, "present") || !strings.Contains(low, "sufficient") {
		t.Errorf("resolved_clean must say the check is present but not verified sufficient, got:\n%s", body)
	}
}
