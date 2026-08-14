package scaffold_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/codefit-cli/codefit/internal/config"
	"github.com/codefit-cli/codefit/internal/scaffold"
)

// copyTree copies the fixture project into a writable temp dir so Generate can
// write artifacts without polluting testdata.
func copyTree(t *testing.T, src string) string {
	t.Helper()
	return copyTreeInto(t, src, t.TempDir())
}

// copyTreeInto is copyTree with the destination chosen by the caller. A test that
// asserts the project NAME needs the root's base name to be its own choice rather
// than t.TempDir()'s counter ("001").
func copyTreeInto(t *testing.T, src, dst string) string {
	t.Helper()
	if err := os.MkdirAll(dst, 0o755); err != nil {
		t.Fatalf("copyTreeInto: %v", err)
	}
	err := filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, path)
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
	if err != nil {
		t.Fatalf("copyTree: %v", err)
	}
	return dst
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func TestGenerateCreatesConfigAndSkill(t *testing.T) {
	root := copyTree(t, sampleNext)
	mkdirs(t, root, ".claude", ".opencode")

	res, err := scaffold.Generate(scaffold.Options{Root: root})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if res.ConfigAction != scaffold.ConfigCreated {
		t.Errorf("config action = %q, want created", res.ConfigAction)
	}
	if !fileExists(filepath.Join(root, ".codefit.yaml")) {
		t.Errorf(".codefit.yaml was not written")
	}
	if res.UsedFallback {
		t.Errorf("usedFallback = true, want false (agents present)")
	}
	if len(res.Skills) != 2 {
		t.Fatalf("skills written = %d, want 2 (Claude Code + OpenCode)", len(res.Skills))
	}
	for _, sw := range res.Skills {
		if !fileExists(filepath.Join(root, sw.Path)) {
			t.Errorf("skill for %s not on disk at %s", sw.Agent, sw.Path)
		}
	}
}

func TestGenerateDoesNotOverwriteWithoutPermission(t *testing.T) {
	root := copyTree(t, sampleNext)
	cfgPath := filepath.Join(root, ".codefit.yaml")
	if err := os.WriteFile(cfgPath, []byte("version: \"existing\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := scaffold.Generate(scaffold.Options{Root: root, OverwriteConfig: false})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if res.ConfigAction != scaffold.ConfigSkipped {
		t.Errorf("config action = %q, want skipped", res.ConfigAction)
	}
	data, _ := os.ReadFile(cfgPath)
	if string(data) != "version: \"existing\"\n" {
		t.Errorf("existing config was modified: %q", data)
	}
}

func TestGenerateOverwritesWithPermission(t *testing.T) {
	root := copyTree(t, sampleNext)
	cfgPath := filepath.Join(root, ".codefit.yaml")
	if err := os.WriteFile(cfgPath, []byte("version: \"existing\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := scaffold.Generate(scaffold.Options{Root: root, OverwriteConfig: true})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if res.ConfigAction != scaffold.ConfigOverwritten {
		t.Errorf("config action = %q, want overwritten", res.ConfigAction)
	}
	data, _ := os.ReadFile(cfgPath)
	if string(data) == "version: \"existing\"\n" {
		t.Errorf("config was not overwritten")
	}
}

func TestConfigExists(t *testing.T) {
	root := t.TempDir()
	if scaffold.ConfigExists(root) {
		t.Errorf("ConfigExists = true on an empty dir")
	}
	if err := os.WriteFile(filepath.Join(root, ".codefit.yaml"), []byte("version: \"1\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !scaffold.ConfigExists(root) {
		t.Errorf("ConfigExists = false after writing .codefit.yaml")
	}
}

// TestGenerate_UndetectedRoundTrips drives the WHOLE init path on a root
// holding only a build manifest codefit registers no provider for, and then
// re-reads the file that landed on disk.
//
// The round trip is not ceremony: writeConfig itself re-loads what it just
// wrote and turns a validation failure into a hard error, so a sentinel the
// validator rejected would make init fail on exactly the projects it now exists
// to serve.
//
// PathCriticalityFor is asserted through the REAL written file rather than
// through the in-memory ProjectInfo: this test's subject is the artifact the
// developer keeps, so what it proves is that the file ON DISK loads, carries the
// sentinel, and classifies no path — and that the skill written beside it never
// passes the sentinel as a tool argument.
//
// What it does NOT prove on its own, stated because a test comment that
// overstates its reach is how a gap goes unnoticed: it is BLIND to globs that
// exist only in ProjectInfo. The config template gates the whole
// path_criticality block behind {{if .Detected}}, so invented globs never reach
// the file this test reads. Measured, not assumed — a mutation making Detect
// invent globs for the undetected case (go vet 0) left this test GREEN and
// reddened only TestDetectUnregisteredStackSucceeds.
//
// The behaviour is locked by the three tests together, each on a different
// surface:
//   - TestDetectUnregisteredStackSucceeds — the SOURCE: Detect invents no globs
//     when no provider supplied defaults (the mutation above).
//   - TestRenderConfig_UndetectedOmitsPathCriticalityWhole — the RENDERING: the
//     key is omitted whole rather than emitted empty (a mutation removing the
//     template's Detected gate reddens it, and this test stays GREEN).
//   - this test — the ROUND TRIP: what landed on disk re-loads through the very
//     validator writeConfig runs, and PathCriticalityFor returns "" for it.
//
// Only their composition rules out both directions; none of the three replaces
// the other two.
func TestGenerate_UndetectedRoundTrips(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "pom.xml", "<project/>\n")

	res, err := scaffold.Generate(scaffold.Options{Root: root})
	if err != nil {
		t.Fatalf("Generate on an unregistered stack must not refuse, got: %v", err)
	}
	if res.Info.Detected() {
		t.Fatalf("fixture is not the undetected case: language = %q", res.Info.Language)
	}
	if res.ConfigAction != scaffold.ConfigCreated {
		t.Errorf("config action = %q, want created", res.ConfigAction)
	}

	cfg, err := config.Load(filepath.Join(root, scaffold.ConfigName))
	if err != nil {
		t.Fatalf("the config init wrote does not load: %v", err)
	}
	if cfg.Project.Language != config.LanguageUndetected {
		t.Errorf("language = %q, want %q", cfg.Project.Language, config.LanguageUndetected)
	}
	for _, path := range []string{"src/x_test.go", "src/main.go", "examples/demo.go"} {
		if got := cfg.PathCriticalityFor(path); got != "" {
			t.Errorf("PathCriticalityFor(%q) = %q through the real written config, want \"\" — "+
				"codefit classified a path it invented globs for", path, got)
		}
	}

	// The skill landed, and it is the undetected one.
	if len(res.Skills) != 1 {
		t.Fatalf("skills written = %d, want 1 fallback", len(res.Skills))
	}
	skill, err := os.ReadFile(filepath.Join(root, res.Skills[0].Path))
	if err != nil {
		t.Fatalf("reading the written skill: %v", err)
	}
	if strings.Contains(string(skill), `"`+config.LanguageUndetected+`"`) {
		t.Errorf("the written skill passes the sentinel as a tool argument:\n%s", skill)
	}
}

// TestGenerate_UndetectedWritesNothingElse pins the write set on the new path.
// codefit generates its own skill and NEVER touches the user's CLAUDE.md /
// AGENTS.md — agents.go uses CLAUDE.md purely as an os.Stat marker and never
// opens it. Adding a whole new branch to Generate is exactly when that boundary
// could slip unnoticed.
func TestGenerate_UndetectedWritesNothingElse(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "pom.xml", "<project/>\n")
	const sentinelText = "PRE-EXISTING CONTENT, CODEFIT MUST NOT TOUCH THIS\n"
	writeFile(t, root, "CLAUDE.md", sentinelText)

	res, err := scaffold.Generate(scaffold.Options{Root: root})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	want := map[string]bool{
		"pom.xml":           true,
		"CLAUDE.md":         true,
		scaffold.ConfigName: true,
		res.Skills[0].Path:  true,
	}
	var got []string
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		got = append(got, rel)
		return nil
	})
	if err != nil {
		t.Fatalf("walking the generated tree: %v", err)
	}
	for _, rel := range got {
		if !want[rel] {
			t.Errorf("Generate wrote an unexpected file %q; the write set is %s plus the skill", rel, scaffold.ConfigName)
		}
	}
	if len(got) != len(want) {
		t.Errorf("files on disk = %v, want exactly %d entries", got, len(want))
	}

	// CLAUDE.md is a detection MARKER, never an artifact codefit edits.
	after, err := os.ReadFile(filepath.Join(root, "CLAUDE.md"))
	if err != nil {
		t.Fatalf("reading CLAUDE.md back: %v", err)
	}
	if string(after) != sentinelText {
		t.Errorf("codefit modified the user's CLAUDE.md:\n%s", after)
	}
}

// skillNoSuchKeyClaim is the sentence the undetected skill body states about the
// config generated beside it. It is quoted here as an ANCHOR, not as a substring
// convenience: the lock below fails loudly when the sentence is reworded, rather
// than quietly passing because its `if` never fired.
const skillNoSuchKeyClaim = "Generated configs\nfor a project like this carry NO such key"

// liveYAMLKeyPresent reports whether raw carries key as a LIVE (non-comment) YAML
// key. The generated config discusses `schema_paths` in prose for every project
// that has none, so a plain substring search would answer "yes" for the exact
// case this lock exists to keep saying "no" about.
func liveYAMLKeyPresent(raw, key string) bool {
	for _, line := range strings.Split(raw, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if strings.HasPrefix(trimmed, key+":") {
			return true
		}
	}
	return false
}

// TestGenerate_SkillClaimHoldsForBaitedMigrationDir is R7, the fixture is STILL
// BAITED on purpose, and the test's meaning has been RETARGETED because the bait
// finally fired.
//
// The root holds a build manifest codefit registers no provider for AND a real
// Flyway-shaped SQL migration directory. When this lock was written, detection
// found neither, so the skill's unconditional claim that "generated configs for
// a project like this carry NO such key" was TRUE and the test was green. The
// bait existed to make that stop being true the day SQL-migration detection
// landed — and it did: this same fixture now acquires SchemaPaths, the config
// gains a live `schema_paths:` key, and the old body could not pass.
//
// THE CORRECT RESPONSE WAS NOT TO DELETE IT. The test keeps its exact NAME (a
// rename is deletion by another name) and its baited fixture; what changed is
// that the claim it guards is now CONDITIONAL, so the lock became a
// two-directional EQUIVALENCE over the bytes one Generate run really wrote:
//
//	no live schema_paths ⇒ the skill MUST state the no-key claim
//	   live schema_paths ⇒ the skill MUST NOT state it, and MUST name the key
//	                       and the path init actually wrote
//
// Both directions are needed. Checking only the first would pass for a skill
// that dropped the conditional and never mentioned a schema again; checking only
// the second would pass for a skill that lost the claim it makes on the
// projects that still have no schema (TestGenerate_SkillClaimHoldsForUnbaitedRoot
// is that half's fixture).
//
// The anchored positive probe still runs FIRST in whichever branch applies, so a
// REWORDED skill fails loudly instead of passing vacuously.
func TestGenerate_SkillClaimHoldsForBaitedMigrationDir(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "pom.xml", "<project/>\n")
	mkdirs(t, root, filepath.Join("db", "migrations"))
	writeFile(t, root, filepath.Join("db", "migrations", "V1__init.sql"), `
CREATE TABLE customer (
  id      BIGINT PRIMARY KEY,
  email   VARCHAR(255) NOT NULL,
  created TIMESTAMP NOT NULL
);
CREATE TABLE invoice (
  id          BIGINT PRIMARY KEY,
  customer_id BIGINT NOT NULL REFERENCES customer(id),
  total_cents BIGINT NOT NULL
);
`)

	res, err := scaffold.Generate(scaffold.Options{Root: root})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if res.Info.Detected() {
		t.Fatalf("fixture is not the undetected case: language = %q", res.Info.Language)
	}

	// The bait has to still BE bait. If this fixture ever stops producing a live
	// key, the live branch below stops running and the test degrades into the
	// unbaited case while still carrying this name.
	if !liveYAMLKeyPresent(mustReadConfig(t, root), "schema_paths") {
		t.Fatalf("the baited fixture no longer yields a live schema_paths. This test's whole "+
			"value is exercising the branch where the skill's claim must NOT be made; without "+
			"it the lock is a duplicate of the unbaited case\n--- config ---\n%s",
			mustReadConfig(t, root))
	}

	assertSkillMatchesConfig(t, root, res)
}

// TestGenerate_SkillClaimHoldsForUnbaitedRoot is C-R7b: the same manifest with
// NO DDL beside it. It keeps the no-key claim itself under test, which the
// baited fixture no longer can now that the bait fires.
func TestGenerate_SkillClaimHoldsForUnbaitedRoot(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "pom.xml", "<project/>\n")

	res, err := scaffold.Generate(scaffold.Options{Root: root})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if liveYAMLKeyPresent(mustReadConfig(t, root), "schema_paths") {
		t.Fatalf("a root with no DDL must not produce a live schema_paths\n--- config ---\n%s",
			mustReadConfig(t, root))
	}
	assertSkillMatchesConfig(t, root, res)
}

// assertSkillMatchesConfig holds the two artifacts one Generate run wrote to the
// equivalence R7 requires, in whichever direction that run landed in.
//
// It reads the FILES rather than re-rendering, because what the developer's
// agent loads is the file. Re-rendering would test the template against itself.
func assertSkillMatchesConfig(t *testing.T, root string, res scaffold.Result) {
	t.Helper()

	cfgText := mustReadConfig(t, root)
	if len(res.Skills) != 1 {
		t.Fatalf("skills written = %d, want 1 fallback", len(res.Skills))
	}
	rawSkill, err := os.ReadFile(filepath.Join(root, res.Skills[0].Path))
	if err != nil {
		t.Fatalf("reading the written skill: %v", err)
	}
	skill := string(rawSkill)

	if !liveYAMLKeyPresent(cfgText, "schema_paths") {
		// Direction 1. Positive probe FIRST: without it, "the skill states the
		// claim" is satisfied by any skill that happens not to contradict it, and
		// a rewording would pass as silently as a deletion.
		if !strings.Contains(skill, skillNoSuchKeyClaim) {
			t.Fatalf("the config carries NO live schema_paths, so the skill must state %q — and "+
				"it does not. If the wording changed legitimately, move the anchor; if the CLAIM "+
				"changed, decide what the skill should now tell an agent about a config with no "+
				"schema key\n--- skill ---\n%s", skillNoSuchKeyClaim, skill)
		}
		return
	}

	// Direction 2. The config DOES carry the key, so the claim is now false and
	// must be gone — and its absence is not enough on its own: the skill has to
	// tell the agent what the config really says, or the agent learns nothing.
	if strings.Contains(skill, skillNoSuchKeyClaim) {
		t.Errorf("the config init wrote in this same run carries a live schema_paths, yet the "+
			"skill beside it still claims %q. The first artifact an agent reads would tell it to "+
			"expect no such key in a config that has one\n--- config ---\n%s\n--- skill ---\n%s",
			skillNoSuchKeyClaim, cfgText, skill)
	}
	if !strings.Contains(skill, "schema_paths") {
		t.Errorf("the skill must NAME the `schema_paths` key the config carries\n--- skill ---\n%s", skill)
	}
	for _, p := range res.Info.SchemaPaths {
		if !strings.Contains(skill, filepath.ToSlash(p)) {
			t.Errorf("the skill must name the path init actually wrote (%q), not a generic example "+
				"— an agent that reads a different path audits a different schema\n--- skill ---\n%s",
				filepath.ToSlash(p), skill)
		}
	}
}

func TestGenerateFallbackWhenNoAgents(t *testing.T) {
	root := copyTree(t, sampleNext) // no agent marker dirs

	res, err := scaffold.Generate(scaffold.Options{Root: root})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if !res.UsedFallback {
		t.Errorf("usedFallback = false, want true")
	}
	if len(res.Skills) != 1 {
		t.Fatalf("skills = %d, want 1 fallback", len(res.Skills))
	}
	want := filepath.FromSlash(".agents/skills/codefit/SKILL.md")
	if res.Skills[0].Path != want {
		t.Errorf("fallback skill path = %q, want %q", res.Skills[0].Path, want)
	}
	if !fileExists(filepath.Join(root, want)) {
		t.Errorf("fallback skill not on disk")
	}
}
