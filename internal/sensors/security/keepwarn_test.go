package security_test

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"

	"github.com/codefit-cli/codefit/internal/config"
)

// captureLogs redirects the default slog logger for the duration of a test and
// returns the buffer it wrote into.
func captureLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn})))
	t.Cleanup(func() { slog.SetDefault(previous) })
	return &buf
}

// warnRecords counts LOG RECORDS mentioning needle. slog's text handler emits
// one record per line, so a line is the unit; counting raw substrings would
// double-count a message that names the same key twice.
func warnRecords(logs, needle string) int {
	n := 0
	for _, line := range strings.Split(strings.TrimSpace(logs), "\n") {
		if strings.Contains(line, needle) {
			n++
		}
	}
	return n
}

// twoTestFileSecrets writes TWO test-classified files each carrying a
// credential-shaped literal. Two is the point: the warning must be stated ONCE
// per run, and a per-file warning would be indistinguishable from a per-run one
// on a single-file fixture.
func twoTestFileSecrets(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	writeFile(t, root, "a_test.go", "package x\nvar k = \"sk-ant-api03-AbCdEf1234567890\"\n")
	writeFile(t, root, "b_test.go", "package x\nvar j = \"ghp_ABCDEFGHIJKLMNOPQRSTUV12345\"\n")
	return root
}

// TestKeepModeWarnsOncePerRun pins WHERE and HOW OFTEN the "keep" consequence
// is stated. codefit does not refuse a PRD-named mode, so the only thing
// standing between a developer and a project silently blocked by its own test
// fixtures is this warning — and a warning repeated per file is noise the
// developer learns to scroll past.
func TestKeepModeWarnsOncePerRun(t *testing.T) {
	logs := captureLogs(t)
	root := twoTestFileSecrets(t)

	runSensorMode(t, root, config.TestSeverityKeep)

	got := logs.String()
	// Counted per RECORD, not per substring: the message names test_severity
	// twice inside one line, so a naive strings.Count would report two warnings
	// where one was emitted — a probe that measures the wrong thing.
	if n := warnRecords(got, "test_severity"); n != 1 {
		t.Errorf("want exactly ONE keep warning per run, got %d:\n%s", n, got)
	}
	// The warning must carry the consequence, not just name the key.
	for _, want := range []string{"keep", "blocked", "critical_findings_on_test_paths=2"} {
		if !strings.Contains(got, want) {
			t.Errorf("the warning must state the consequence and the count; missing %q in:\n%s", want, got)
		}
	}
}

// TestNonKeepModesAreSilent triangulates: the warning is a consequence of keep
// alone. Under info and downgrade nothing survives at critical, so there is
// nothing to inform and codefit says nothing.
func TestNonKeepModesAreSilent(t *testing.T) {
	for _, mode := range []string{"", config.TestSeverityInfo, config.TestSeverityDowngrade} {
		t.Run("mode="+mode, func(t *testing.T) {
			logs := captureLogs(t)
			runSensorMode(t, twoTestFileSecrets(t), mode)
			if got := logs.String(); strings.Contains(got, "test_severity") {
				t.Errorf("mode %q must not warn about test_severity, got:\n%s", mode, got)
			}
		})
	}
}
