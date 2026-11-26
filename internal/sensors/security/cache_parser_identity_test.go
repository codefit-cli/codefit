package security

import (
	"bytes"
	"crypto/sha256"
	"debug/buildinfo"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"testing"

	"github.com/codefit-cli/codefit/internal/core/cache"
)

// parserModule is the module the TypeScript provider parses with (ADR 0002,
// ADR 0088).
const parserModule = "github.com/odvcencio/gotreesitter"

// TestCache_ParserVersionIsPartOfTheAnalyzerIdentity answers, with evidence, the
// question the parser upgrade of ADR 0088 raises: can a finding-cache entry
// written by a codefit built with the OLD parser be served by a codefit built
// with the new one? It cannot, and this test pins why.
//
// Serving it would be a false all-clear. Under gotreesitter v0.20.2 a file that
// tripped on an ordinary line lost every finding after it (issue #189), and a
// cached copy of that truncated result would keep it truncated after the
// upgrade.
//
// The chain is three links, and the test checks each of them:
//
//  1. The sensor keys every entry on the analyzer identity of the cache it
//     opens: Cache.Key hashes Cache.Analyzer first, so two identities give two
//     keys for the same file (checked at the end of this test;
//     TestKeyIncludesAnalyzerIdentity and TestCache_DifferentAnalyzerIdentityIsAMiss
//     pin the resulting miss end to end).
//  2. The identity cache.Open binds is the SHA-256 of the running executable's
//     bytes — checked here against this very test binary.
//  3. A codefit binary's bytes carry the parser module's path and version: the
//     Go toolchain embeds the module build list in every binary `go build`
//     links, stripped (-s -w) or not. A codefit built with another parser
//     version therefore has other bytes, another identity and other keys —
//     checked here by building codefit with the release flags and finding the
//     module line, version included, in its bytes.
//
// Nothing about the parser is special-cased in the key, and nothing needs to be:
// the version is already inside what is hashed.
func TestCache_ParserVersionIsPartOfTheAnalyzerIdentity(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the codefit binary")
	}
	// Link 3 is a property of the RELEASED binary, so it is checked on one: a
	// test binary carries no module build list, and checking it there would
	// prove nothing. The build uses the release tags and the stripping flags
	// (Makefile, .goreleaser.yaml), because -s -w is what ships.
	name := "codefit"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	bin := filepath.Join(t.TempDir(), name)
	out, err := exec.Command("go", "build",
		"-tags", "grammar_subset,grammar_subset_typescript,grammar_subset_tsx",
		"-ldflags", "-s -w",
		"-o", bin, "github.com/codefit-cli/codefit/cmd/codefit").CombinedOutput()
	if err != nil {
		t.Fatalf("building codefit: %v\n%s", err, out)
	}
	info, err := buildinfo.ReadFile(bin)
	if err != nil {
		t.Fatalf("reading the build information of the built binary: %v", err)
	}
	var parser *debug.Module
	for _, dep := range info.Deps {
		if dep.Path == parserModule {
			parser = dep
		}
	}
	// The positive control: the parser is in the binary's build list, so the
	// search below looks for something that must be there.
	if parser == nil || parser.Version == "" {
		t.Fatalf("%s is not in the built binary's build list; the test would prove nothing", parserModule)
	}
	released, err := os.ReadFile(bin)
	if err != nil {
		t.Fatalf("reading the built binary: %v", err)
	}
	line := []byte(parser.Path + "\t" + parser.Version)
	if !bytes.Contains(released, line) {
		t.Fatalf("the built binary does not contain %q: the parser version is not part of the bytes the "+
			"analyzer identity hashes, so a cache entry written under another parser could be served", line)
	}

	// Link 2: the identity the sensor's cache binds is the hash of the running
	// executable's bytes, whatever that executable is.
	self, err := os.Executable()
	if err != nil {
		t.Skipf("os.Executable unavailable on this platform: %v", err)
	}
	running, err := os.ReadFile(self)
	if err != nil {
		t.Fatalf("reading the running executable: %v", err)
	}
	c, err := openCache(t.TempDir())
	if err != nil {
		t.Fatalf("opening the cache the sensor opens: %v", err)
	}
	sum := sha256.Sum256(running)
	if want := hex.EncodeToString(sum[:]); c.Analyzer != want {
		t.Fatalf("the cache's analyzer identity is %q, not the SHA-256 of the running executable (%q): "+
			"the key no longer covers the parser that produced an entry", c.Analyzer, want)
	}

	// Link 1, through the production key: the same file under an identity that
	// differs only in the analyzer is a different key.
	other := &cache.Cache{Dir: c.Dir, Analyzer: c.Analyzer + "-other-parser"}
	if c.Key("src/a.ts", []byte("x")) == other.Key("src/a.ts", []byte("x")) {
		t.Fatal("two analyzers produce the same key for the same file: an old parser's entry would be served")
	}
}
