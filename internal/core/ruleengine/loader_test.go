package ruleengine_test

import (
	"io/fs"
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

// TestLoadFS_EveryShippedRuleFileLoadsStrictly loads each embedded rule file ON
// ITS OWN under the strict decoder, so a shipped file relying on a key the
// loader does not know fails here by name instead of hiding inside the corpus.
// It is the proof the strict loader was allowed to land on: if any shipped file
// had carried an unknown key, that rule had been silently missing it.
func TestLoadFS_EveryShippedRuleFileLoadsStrictly(t *testing.T) {
	const shippedDir = "typescript/security"
	entries, err := fs.ReadDir(rules.FS, shippedDir)
	if err != nil {
		t.Fatalf("reading %s: %v", shippedDir, err)
	}
	var files int
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		files++
		data, err := fs.ReadFile(rules.FS, shippedDir+"/"+e.Name())
		if err != nil {
			t.Fatalf("reading %s: %v", e.Name(), err)
		}
		one := dir(map[string]string{"only/" + e.Name(): string(data)})
		loaded, err := ruleengine.LoadFS(one, "only")
		if err != nil {
			t.Errorf("shipped rule file %s does not load under strict decoding: %v", e.Name(), err)
			continue
		}
		if len(loaded) == 0 {
			t.Errorf("shipped rule file %s loaded zero rules", e.Name())
		}
	}
	if files == 0 {
		t.Fatal("vacuum: no shipped rule files were found")
	}
}
