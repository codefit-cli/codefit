package ruleengine_test

import (
	"fmt"
	"io/fs"
	"path"
	"slices"
	"sort"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/codefit-cli/codefit/internal/core/ruleengine"
	"github.com/codefit-cli/codefit/rules"
)

// file builds an in-memory rule directory for LoadFS, so loader behaviour is
// tested without touching the real embedded rules.
func dir(files map[string]string) fstest.MapFS {
	m := fstest.MapFS{}
	for name, body := range files {
		m[name] = &fstest.MapFile{Data: []byte(body)}
	}
	return m
}

func TestLoadFS_ValidRules(t *testing.T) {
	fsys := dir(map[string]string{
		"sec/crypto.yaml": `
rules:
  - id: SEC-052
    message: "Weak hash"
    suggestion: "Use SHA-256+"
    severity: medium
    dimension: security
    pattern-either:
      - "md5($X)"
      - "sha1($X)"
`,
		"sec/eval.yaml": `
rules:
  - id: SEC-014
    message: "Dangerous eval"
    severity: high
    dimension: security
    pattern: "eval($X)"
`,
	})

	rules, err := ruleengine.LoadFS(fsys, "sec")
	if err != nil {
		t.Fatalf("LoadFS: %v", err)
	}
	if len(rules) != 2 {
		t.Fatalf("want 2 rules, got %d", len(rules))
	}
	ids := map[string]bool{}
	for _, r := range rules {
		ids[r.ID] = true
	}
	if !ids["SEC-052"] || !ids["SEC-014"] {
		t.Errorf("missing expected rule ids: %v", ids)
	}
}

// A malformed YAML file must fail with the filename in the error — a broken rule
// never enters the engine silently.
func TestLoadFS_MalformedYAML(t *testing.T) {
	fsys := dir(map[string]string{
		"sec/broken.yaml": "rules:\n  - id: SEC-1\n    message: [unterminated\n",
	})
	_, err := ruleengine.LoadFS(fsys, "sec")
	if err == nil {
		t.Fatal("malformed YAML must error")
	}
	if !strings.Contains(err.Error(), "broken.yaml") {
		t.Errorf("error must locate the file, got: %v", err)
	}
}

// Each structural defect (missing id, bad severity, bad dimension, no pattern)
// must fail with a located error naming the file and, where known, the rule id.
func TestLoadFS_Validation(t *testing.T) {
	cases := []struct {
		name      string
		body      string
		wantInErr []string
	}{
		{
			name:      "missing id",
			body:      "rules:\n  - message: x\n    severity: high\n    dimension: security\n    pattern: \"eval($X)\"\n",
			wantInErr: []string{"bad.yaml", "id"},
		},
		{
			name:      "missing message",
			body:      "rules:\n  - id: SEC-9\n    severity: high\n    dimension: security\n    pattern: \"eval($X)\"\n",
			wantInErr: []string{"bad.yaml", "SEC-9", "message"},
		},
		{
			name:      "invalid severity",
			body:      "rules:\n  - id: SEC-9\n    message: x\n    severity: spicy\n    dimension: security\n    pattern: \"eval($X)\"\n",
			wantInErr: []string{"bad.yaml", "SEC-9", "severity"},
		},
		{
			name:      "invalid dimension",
			body:      "rules:\n  - id: SEC-9\n    message: x\n    severity: high\n    dimension: vibes\n    pattern: \"eval($X)\"\n",
			wantInErr: []string{"bad.yaml", "SEC-9", "dimension"},
		},
		{
			name:      "no pattern",
			body:      "rules:\n  - id: SEC-9\n    message: x\n    severity: high\n    dimension: security\n",
			wantInErr: []string{"bad.yaml", "SEC-9", "pattern"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ruleengine.LoadFS(dir(map[string]string{"sec/bad.yaml": tc.body}), "sec")
			if err == nil {
				t.Fatalf("%s: expected validation error", tc.name)
			}
			for _, want := range tc.wantInErr {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("%s: error %q missing %q", tc.name, err.Error(), want)
				}
			}
		})
	}
}

// TestLoadFS_RejectsUnknownKeys pins the strict decoding contract. A rule file is
// decoded with unknown keys REJECTED, because the lenient decoder this replaced
// dropped a misspelled operator without a word: a rule carrying
// `metavariable-regx:` loaded cleanly and simply lost its constraint, and a
// rule that loses a constraint is a rule that fires on what it was told not to,
// or a veto that silently stops vetoing. The error must name the file and the
// key, so the author is pointed at the typo rather than at a rule that "works".
func TestLoadFS_RejectsUnknownKeys(t *testing.T) {
	cases := []struct {
		name      string
		body      string
		wantInErr []string
	}{
		{
			name: "misspelled operator inside a rule",
			body: "rules:\n  - id: SEC-9\n    message: x\n    severity: high\n    dimension: security\n" +
				"    pattern: \"eval($X)\"\n    metavariable-regx:\n      $X: \"^a$\"\n",
			wantInErr: []string{"bad.yaml", "metavariable-regx"},
		},
		{
			name: "misspelled top-level key",
			body: "rulez:\n  - id: SEC-9\n    message: x\n    severity: high\n    dimension: security\n" +
				"    pattern: \"eval($X)\"\n",
			wantInErr: []string{"bad.yaml", "rulez"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ruleengine.LoadFS(dir(map[string]string{"sec/bad.yaml": tc.body}), "sec")
			if err == nil {
				t.Fatalf("%s: LoadFS accepted an unknown key; the rule would silently lose it", tc.name)
			}
			for _, want := range tc.wantInErr {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("%s: error %q missing %q", tc.name, err.Error(), want)
				}
			}
		})
	}
}

// TestLoadFS_EmptyFileStillLoadsAsNoRules keeps the one lenient behaviour the
// strict decoder must not lose: an empty rule file declares nothing and is not
// an error. A streaming decoder reports an empty input as end-of-stream, and
// treating that as a failure would turn an empty file into a broken corpus.
func TestLoadFS_EmptyFileStillLoadsAsNoRules(t *testing.T) {
	fsys := dir(map[string]string{
		"sec/empty.yaml": "",
		"sec/eval.yaml":  "rules:\n  - id: SEC-014\n    message: m\n    severity: high\n    dimension: security\n    pattern: \"eval($X)\"\n",
	})
	got, err := ruleengine.LoadFS(fsys, "sec")
	if err != nil {
		t.Fatalf("LoadFS with an empty file: %v", err)
	}
	if len(got) != 1 || got[0].ID != "SEC-014" {
		t.Fatalf("want exactly SEC-014 from the non-empty file, got %+v", got)
	}
}

// strictLoadReport is what checkEveryRuleFileLoadsStrictly observed.
type strictLoadReport struct {
	dirs     []string // every directory holding at least one rule file
	visited  int      // rule files loaded on their own
	present  int      // rule files the walk found, counted apart from the loads
	failures []string // one line per file that did not load, or loaded no rules
}

// checkEveryRuleFileLoadsStrictly walks fsys, finds every directory holding a
// *.yaml rule file, and loads each such file ON ITS OWN under the strict
// decoder, then the whole directory as production does. A file that does not
// load, or loads zero rules, is a failure named by its path.
func checkEveryRuleFileLoadsStrictly(t *testing.T, fsys fs.FS) strictLoadReport {
	t.Helper()
	var rep strictLoadReport
	byDir := map[string][]string{}
	err := fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(d.Name(), ".yaml") {
			return nil
		}
		rep.present++
		byDir[path.Dir(p)] = append(byDir[path.Dir(p)], p)
		return nil
	})
	if err != nil {
		t.Fatalf("walking the rules FS: %v", err)
	}
	for d := range byDir {
		rep.dirs = append(rep.dirs, d)
	}
	sort.Strings(rep.dirs)
	for _, d := range rep.dirs {
		perFile := 0
		for _, p := range byDir[d] {
			rep.visited++
			data, err := fs.ReadFile(fsys, p)
			if err != nil {
				t.Fatalf("reading %s: %v", p, err)
			}
			one := dir(map[string]string{"only/" + path.Base(p): string(data)})
			loaded, err := ruleengine.LoadFS(one, "only")
			switch {
			case err != nil:
				rep.failures = append(rep.failures, fmt.Sprintf("%s does not load under strict decoding: %v", p, err))
			case len(loaded) == 0:
				rep.failures = append(rep.failures, p+" loaded zero rules")
			}
			perFile += len(loaded)
		}
		// The directory as a whole, the way the provider loads it.
		whole, err := ruleengine.LoadFS(fsys, d)
		if err != nil {
			rep.failures = append(rep.failures, fmt.Sprintf("directory %s does not load: %v", d, err))
		} else if len(whole) != perFile {
			rep.failures = append(rep.failures, fmt.Sprintf("directory %s loads %d rules, its files one by one %d",
				d, len(whole), perFile))
		}
	}
	return rep
}

// TestLoadFS_EveryShippedRuleFileLoadsStrictly loads each embedded rule file,
// in EVERY directory of the embedded rules FS that holds one, ON ITS OWN under
// the strict decoder, so a shipped file relying on a key the loader does not
// know fails here by name instead of hiding inside the corpus. It is the proof
// the strict loader was allowed to land on: if any shipped file had carried an
// unknown key, that rule had been silently missing it.
func TestLoadFS_EveryShippedRuleFileLoadsStrictly(t *testing.T) {
	rep := checkEveryRuleFileLoadsStrictly(t, rules.FS)
	for _, f := range rep.failures {
		t.Error(f)
	}
	// Positive control: every rule file the walk found was loaded, and there was
	// at least one. Counted against the walk, not a number written here, so a
	// rule file added in a new directory is covered without editing this test.
	if rep.present == 0 || rep.visited != rep.present {
		t.Fatalf("vacuum: loaded %d rule files of %d found in %v", rep.visited, rep.present, rep.dirs)
	}
	t.Logf("loaded %d rule files strictly, in %v", rep.visited, rep.dirs)
}

// TestLoadFS_StrictWalkCatchesAPlantedKey proves the walk above bites: a copy
// of the embedded rules with one unknown key planted in one file, and one file
// added in a directory the shipped tree does not have, must report exactly that
// file and visit the new directory.
func TestLoadFS_StrictWalkCatchesAPlantedKey(t *testing.T) {
	plant := fstest.MapFS{}
	var victim string
	err := fs.WalkDir(rules.FS, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := fs.ReadFile(rules.FS, p)
		if err != nil {
			return err
		}
		if victim == "" && strings.HasSuffix(p, ".yaml") && strings.Contains(string(data), "\n    message:") {
			victim = p
			data = []byte(strings.Replace(string(data), "\n    message:", "\n    mesage: planted\n    message:", 1))
		}
		plant[p] = &fstest.MapFile{Data: data}
		return nil
	})
	if err != nil || victim == "" {
		t.Fatalf("building the planted copy: victim %q, err %v", victim, err)
	}
	plant["planted/elsewhere/one.yaml"] = &fstest.MapFile{Data: []byte(
		"rules:\n  - id: SEC-9\n    message: m\n    severity: high\n    dimension: security\n    pattern: \"eval($X)\"\n")}

	rep := checkEveryRuleFileLoadsStrictly(t, plant)
	var named int
	for _, f := range rep.failures {
		if strings.Contains(f, victim) && strings.Contains(f, "mesage") {
			named++
		}
	}
	if named == 0 {
		t.Errorf("the planted key in %s was not reported; failures: %v", victim, rep.failures)
	}
	if !slices.Contains(rep.dirs, "planted/elsewhere") || rep.visited != rep.present {
		t.Errorf("the walk did not reach every directory: visited %d of %d in %v", rep.visited, rep.present, rep.dirs)
	}
}

// TestLoadFS_OtherYAMLErrorsAreNotBlamedOnAnUnknownKey: the unknown-key wording
// is for unknown keys only. A duplicate key, a value of the wrong type and a
// top-level list are different mistakes, and each keeps an accurate message:
// the file, and the YAML error with its line.
func TestLoadFS_OtherYAMLErrorsAreNotBlamedOnAnUnknownKey(t *testing.T) {
	const unknownKeyWording = "a key the loader does not know"
	cases := []struct {
		name      string
		body      string
		wantInErr []string
	}{
		{
			name: "duplicate key",
			body: "rules:\n  - id: SEC-9\n    id: SEC-10\n    message: x\n    severity: high\n" +
				"    dimension: security\n    pattern: \"eval($X)\"\n",
			wantInErr: []string{"bad.yaml", "line", "id"},
		},
		{
			name: "value of the wrong type",
			body: "rules:\n  - id: SEC-9\n    message: x\n    severity: high\n    dimension: security\n" +
				"    pattern-either: \"eval($X)\"\n",
			wantInErr: []string{"bad.yaml", "line", "cannot unmarshal"},
		},
		{
			name:      "top-level list",
			body:      "- id: SEC-9\n  message: x\n",
			wantInErr: []string{"bad.yaml", "line", "cannot unmarshal"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ruleengine.LoadFS(dir(map[string]string{"sec/bad.yaml": tc.body}), "sec")
			if err == nil {
				t.Fatalf("%s: LoadFS accepted it", tc.name)
			}
			for _, want := range tc.wantInErr {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("%s: error %q missing %q", tc.name, err.Error(), want)
				}
			}
			if strings.Contains(err.Error(), unknownKeyWording) {
				t.Errorf("%s: error blames an unknown key, which is not the cause: %q", tc.name, err.Error())
			}
		})
	}
	// The unknown-key case keeps its wording, so the row above is not passing
	// because the wording disappeared everywhere.
	_, err := ruleengine.LoadFS(dir(map[string]string{"sec/bad.yaml": "rules:\n  - id: SEC-9\n    patern: x\n"}), "sec")
	if err == nil || !strings.Contains(err.Error(), unknownKeyWording) {
		t.Errorf("an unknown key lost its wording: %v", err)
	}
}
