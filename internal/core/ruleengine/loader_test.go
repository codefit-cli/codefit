package ruleengine_test

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/codefit-cli/codefit/internal/core/ruleengine"
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
