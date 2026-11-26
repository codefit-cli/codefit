package typescript_test

import (
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/codefit-cli/codefit/internal/providers"
	"github.com/codefit-cli/codefit/internal/providers/typescript"
)

// clearParserSettings unsets, for the duration of the test, every parser
// setting the process inherited, so a CI environment that sets one cannot make
// these tests depend on it. t.Setenv records the variable for restoration first.
func clearParserSettings(t *testing.T) {
	t.Helper()
	for _, name := range typescript.New().SettingsOverridden() {
		t.Setenv(name, "")
		if err := os.Unsetenv(name); err != nil {
			t.Fatal(err)
		}
	}
}

// TestSettingsFingerprint_ByteIdenticalPin pins the fingerprint's exact output.
// It is part of the cache key (ADR 0089, D2): any change to it — the separator,
// the sort, what is hashed, the hex encoding — silently re-keys every cache.
// SettingsOverridden shares its prefix loop (ADR 0090), and this test is what
// proves sharing it changed nothing. The expected values were produced by the
// fingerprint as it was before the loop was shared (main@22617b8), in this
// exact environment.
func TestSettingsFingerprint_ByteIdenticalPin(t *testing.T) {
	clearParserSettings(t)
	p := typescript.New()
	if got := p.SettingsFingerprint(); got != "" {
		t.Fatalf("with no parser setting the fingerprint is %q, want \"\"", got)
	}
	t.Setenv("GOT_CODEFIT_PIN_PROBE", "1")
	if got, want := p.SettingsFingerprint(), "5c4a1f077c6feb8a26abf2bf0a8d0828dae4943e080042f20faee23da823ef91"; got != want {
		t.Errorf("one setting: fingerprint %s, want %s", got, want)
	}
	// The second setting is placed BEFORE the first in the environment's own
	// order (unset, set the other, set again), so an unsorted join hashes
	// differently from the sorted one and the pin catches a dropped sort.
	if err := os.Unsetenv("GOT_CODEFIT_PIN_PROBE"); err != nil {
		t.Fatal(err)
	}
	t.Setenv("gts_codefit_pin_lower", "two words=x")
	t.Setenv("GOT_CODEFIT_PIN_PROBE", "1")
	if env := strings.Join(os.Environ(), "\n"); strings.Index(env, "gts_codefit_pin_lower=") > strings.Index(env, "GOT_CODEFIT_PIN_PROBE=") {
		t.Fatal("positive control: the environment does not hold the second setting first; the sort is not exercised")
	}
	if got, want := p.SettingsFingerprint(), "0e638393da68716d8a41bb57d7334203592f9ae7f2a4636fab9914c2eb930762"; got != want {
		t.Errorf("two settings: fingerprint %s, want %s", got, want)
	}
}

// TestSettingsOverridden_NamesSortedNeverValues: the provider names the parser
// settings set in this process — every prefix, case-insensitively, sorted — and
// never their values. Negative cases, each with its reason: an unrelated
// variable is not named (the parser does not read it), and a name that only
// CONTAINS a prefix is not named (the match is a prefix match).
func TestSettingsOverridden_NamesSortedNeverValues(t *testing.T) {
	clearParserSettings(t)
	var so providers.SettingsOverrideReporter = typescript.New()
	if got := so.SettingsOverridden(); len(got) != 0 {
		t.Fatalf("with no parser setting: %v, want none", got)
	}

	t.Setenv("GTS_CODEFIT_OVERRIDE_PROBE", "secret-value-1")
	t.Setenv("GOT_CODEFIT_OVERRIDE_PROBE", "secret-value-2")
	t.Setenv("gotreesitter_codefit_override_probe", "secret-value-3")
	t.Setenv("CODEFIT_UNRELATED_OVERRIDE_PROBE", "1")
	t.Setenv("XGOT_CODEFIT_OVERRIDE_PROBE", "1")

	got := so.SettingsOverridden()
	want := []string{"GOT_CODEFIT_OVERRIDE_PROBE", "GTS_CODEFIT_OVERRIDE_PROBE", "gotreesitter_codefit_override_probe"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("SettingsOverridden = %v, want %v", got, want)
	}
	for _, name := range got {
		if strings.Contains(name, "=") || strings.Contains(name, "secret-value") {
			t.Errorf("a value reached the names: %q", name)
		}
	}
}
