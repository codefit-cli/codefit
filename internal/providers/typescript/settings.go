package typescript

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"sort"
	"strings"

	"github.com/codefit-cli/codefit/internal/providers"
)

// compile-time check that Provider declares its run-time parser settings.
var _ providers.SettingsFingerprinter = (*Provider)(nil)

// parserEnvPrefixes are the name prefixes of every environment variable
// gotreesitter v0.54.0 reads in its non-test sources: GOT_ (57 distinct names,
// GOT_PARSE_MEMORY_BUDGET_MB among them — a budget that stops a parse early),
// GOTREESITTER_ (10, the grammar loader) and GTS_ (17, the admission and
// dispatch switches). Prefixes, not a list of names: a name a later release
// adds under the same prefix is covered without anyone noticing it. The match
// ignores case, because Windows' environment does.
//
// Deliberately left out: ISSUE454_CPUPROFILE and ISSUE454_FULLPROFILE, which
// only write a profile, and every variable the Go runtime itself reads.
var parserEnvPrefixes = []string{"GOT_", "GOTREESITTER_", "GTS_"}

// SettingsFingerprint fingerprints the environment variables gotreesitter may
// read (ADR 0089): every variable under parserEnvPrefixes, as the process sees
// it now, as name=value pairs, sorted, hashed. "" when none is set.
//
// It is read on every call, not once: the value is what THIS scan's parser
// sees. GOT_PARSE_MEMORY_BUDGET_MB is read once per process by the parser, so a
// process that changed it after its first parse would fingerprint a value its
// parser no longer obeys — one extra miss, the safe direction.
func (*Provider) SettingsFingerprint() string {
	var pairs []string
	for _, kv := range os.Environ() {
		name, _, _ := strings.Cut(kv, "=")
		upper := strings.ToUpper(name)
		for _, prefix := range parserEnvPrefixes {
			if strings.HasPrefix(upper, prefix) {
				pairs = append(pairs, kv)
				break
			}
		}
	}
	if len(pairs) == 0 {
		return ""
	}
	sort.Strings(pairs)
	sum := sha256.Sum256([]byte(strings.Join(pairs, "\x00")))
	return hex.EncodeToString(sum[:])
}
