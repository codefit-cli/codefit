package ruleengine

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/codefit-cli/codefit/internal/core/findings"
)

// LoadFS reads and validates every *.yaml rule file under dir in fsys and
// returns the rules in a stable, file-sorted order. fsys is supplied by the
// caller (the embedded rules/ FS in production, an in-memory FS in tests), so
// the engine never reaches the real filesystem itself.
//
// Validation is strict and located: a malformed file or a structurally invalid
// rule fails with an error naming the file (and the rule id when known), so a
// broken rule can never enter the engine silently — a silent rule is a silent
// vulnerability.
//
// Decoding is strict too: an unknown key is an error, not a key to skip. See
// [decodeRuleFile] for why.
func LoadFS(fsys fs.FS, dir string) ([]Rule, error) {
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return nil, fmt.Errorf("reading rules dir %q: %w", dir, err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if n := e.Name(); len(n) > 5 && n[len(n)-5:] == ".yaml" {
			names = append(names, n)
		}
	}
	sort.Strings(names) // deterministic load order regardless of FS iteration

	var out []Rule
	for _, name := range names {
		path := dir + "/" + name
		data, err := fs.ReadFile(fsys, path)
		if err != nil {
			return nil, fmt.Errorf("reading %s: %w", name, err)
		}
		fileRules, err := decodeRuleFile(data)
		if err != nil {
			return nil, fmt.Errorf("%s: parsing YAML: %w", name, err)
		}
		for _, r := range fileRules {
			if err := validateRule(r); err != nil {
				return nil, fmt.Errorf("%s: %w", name, err)
			}
			out = append(out, r)
		}
	}
	return out, nil
}

// decodeRuleFile decodes one rule file with unknown keys REJECTED.
//
// The lenient yaml.Unmarshal this replaced skipped any key that [Rule] does not
// declare, so a misspelled operator — `metavariable-regx:`, or
// `metavariable-valu-veto:` — loaded cleanly and the rule silently lost the
// constraint it was written to carry. For a constraint that narrows a match
// that is a rule firing on what it was told to skip; for a veto it is the veto
// quietly switched off. Neither is visible anywhere, which is the one failure
// this loader exists to prevent.
//
// An empty file is not an error and declares no rules: a streaming decoder
// reports empty input as io.EOF, which yaml.Unmarshal used to absorb. Only the
// first YAML document is read, as before.
func decodeRuleFile(data []byte) ([]Rule, error) {
	var doc struct {
		Rules []Rule `yaml:"rules"`
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&doc); err != nil {
		if errors.Is(err, io.EOF) {
			return nil, nil
		}
		if hasUnknownKey(err) {
			return nil, fmt.Errorf("a key the loader does not know is rejected, so a misspelled "+
				"operator cannot be dropped in silence: %w", err)
		}
		return nil, err
	}
	return doc.Rules, nil
}

// hasUnknownKey reports whether err is a yaml.TypeError listing at least one
// unknown field. The same error type also carries duplicate keys and values of
// the wrong type, which are different mistakes and must not be explained as a
// misspelled key. yaml.v3 exposes no structured field for the cause, so the
// message it writes for an unknown field ("field X not found in type T") is
// what is read; if that wording ever changes, the explanation is dropped and
// the error itself still names the file, the line and the key.
func hasUnknownKey(err error) bool {
	var typeErr *yaml.TypeError
	if !errors.As(err, &typeErr) {
		return false
	}
	for _, msg := range typeErr.Errors {
		if strings.Contains(msg, " not found in type ") {
			return true
		}
	}
	return false
}

var validSeverity = map[findings.Severity]bool{
	findings.SeverityCritical: true,
	findings.SeverityHigh:     true,
	findings.SeverityMedium:   true,
	findings.SeverityLow:      true,
	findings.SeverityInfo:     true,
}

var validDimension = map[findings.Dimension]bool{
	findings.DimensionSecurity:   true,
	findings.DimensionReview:     true,
	findings.DimensionDB:         true,
	findings.DimensionComplexity: true,
	findings.DimensionPractices:  true,
	findings.DimensionTests:      true,
}

// validateRule enforces the rule contract before compilation. The id is checked
// first so every later message can name the offending rule.
func validateRule(r Rule) error {
	if r.ID == "" {
		return fmt.Errorf("rule with empty id (message %q): every rule needs a stable id", r.Message)
	}
	if r.Message == "" {
		return fmt.Errorf("rule %s: message is required (it is shown to the developer)", r.ID)
	}
	if !validSeverity[r.Severity] {
		return fmt.Errorf("rule %s: invalid severity %q (want critical|high|medium|low|info)", r.ID, r.Severity)
	}
	if !validDimension[r.Dimension] {
		return fmt.Errorf("rule %s: invalid dimension %q (want security|review|db|complexity|practices|tests)", r.ID, r.Dimension)
	}
	if r.Pattern == "" && len(r.PatternEither) == 0 {
		return fmt.Errorf("rule %s: needs a pattern or pattern-either (a rule without a base pattern matches nothing)", r.ID)
	}
	return nil
}
