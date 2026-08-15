package typescript_test

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/codefit-cli/codefit/internal/providers/typescript"
)

// COVERAGE.md is the human mirror at the end of this project's three-level truth
// chain (CLAUDE.md's doc map): rules → the in-code manifests → this file. It
// keeps FULL prose, because a human has no token limit and abridging it is pure
// loss. What it lacked was identity: an agent that cites db.sqlddl-dialect-limits
// and a human reading the mirror had no shared name for the same entry, so the
// two could only be lined up by reading and guessing.
//
// This control makes that correspondence mechanical instead. It is the same
// source-before-mirror discipline the rest of the chain already has, applied to
// the one link that had nothing but discipline holding it.

// coverageMirrorPath is COVERAGE.md relative to THIS PACKAGE'S DIRECTORY — go
// test runs a package's tests with that directory as the working directory.
const coverageMirrorPath = "../../../COVERAGE.md"

// entryIDMarker is the mirror's id marker: a literal "[id: ...]" carrying one id
// or a comma-separated list of them, placed next to the prose that mirrors those
// entries.
//
// THE MARKER IS DELIBERATELY A SIGIL AND NOT A BARE ID, and this is the whole
// reason the control can be trusted. COVERAGE.md already spells rule ids inside
// running prose in a dozen places — `DB-001` appears inside another rule's
// paragraph, `DB-201` inside a cross-reference — so a control that merely
// searched for the id would find one of those, report the mirror complete, and
// have read a line that was never its subject. A marker cannot occur by
// accident, so a hit means somebody put the id where the entry's prose is.
var entryIDMarker = regexp.MustCompile(`\[id: ([^\]]+)\]`)

func mirrorIDs(t *testing.T) map[string]bool {
	t.Helper()
	raw, err := os.ReadFile(filepath.FromSlash(coverageMirrorPath))
	if err != nil {
		// A mirror that cannot be read yields an EMPTY id set, which would fail
		// loudly below rather than pass — but say WHY, not just what.
		t.Fatalf("reading the human mirror at %s: %v", coverageMirrorPath, err)
	}
	// A fixture is verified by its CONTENT, never by the fact that it opened.
	if !strings.Contains(string(raw), "# Coverage manifest") {
		t.Fatalf("%s is not the coverage mirror: its heading is missing", coverageMirrorPath)
	}
	ids := map[string]bool{}
	for _, m := range entryIDMarker.FindAllStringSubmatch(string(raw), -1) {
		for _, id := range strings.Split(m[1], ",") {
			if id = strings.TrimSpace(id); id != "" {
				ids[id] = true
			}
		}
	}
	return ids
}

// TestCoverageMirror_NamesEveryEntry is the control for the spec's scenario "a
// cited id resolves in the human mirror". It reads BOTH directions, and each
// catches a different real failure:
//
//   - an entry the manifest declares with no marker in COVERAGE.md is an entry a
//     human cannot find when an agent cites it — and, far more often, an entry
//     whose prose was never mirrored at all, which is the drift this project's
//     doc map exists to kill;
//   - a marker naming an id no entry carries is a mirror describing something
//     that no longer exists, which is the same drift pointing the other way, and
//     it is the direction that has actually bitten here: the SOURCE was left less
//     truthful than its mirror twice.
//
// WHAT IT DOES NOT GUARANTEE, stated rather than implied: correspondence, never
// accuracy. A marker sitting next to prose that describes some OTHER entry
// passes. That is the same limit Control A states about itself in
// internal/core/dbcoverage; claiming more would be the over-promising manifest
// this whole effort exists to prevent.
func TestCoverageMirror_NamesEveryEntry(t *testing.T) {
	index := typescript.New().CoverageManifest().Index()
	if len(index) == 0 {
		t.Fatal("vacuum: the manifest declares no entries, so this control would ask nothing")
	}
	mirror := mirrorIDs(t)
	if len(mirror) == 0 {
		t.Fatalf("%s carries no [id: ...] markers at all — the mirror names nothing the manifest declares, "+
			"so a human and an agent still have no shared name for an entry", coverageMirrorPath)
	}

	declared := map[string]bool{}
	var missing []string
	for _, e := range index {
		declared[e.ID] = true
		if !mirror[e.ID] {
			missing = append(missing, e.ID)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		t.Errorf("%d of %d declared entries are named nowhere in %s: %v\n"+
			"Put [id: <id>] next to the prose that mirrors the entry. An agent that cites the id has to "+
			"be able to point a human at the same paragraph.", len(missing), len(index), coverageMirrorPath, missing)
	}

	var stale []string
	for id := range mirror {
		if !declared[id] {
			stale = append(stale, id)
		}
	}
	if len(stale) > 0 {
		sort.Strings(stale)
		t.Errorf("%s marks ids the manifest no longer declares: %v\n"+
			"The mirror is describing an entry that does not exist. Edit the source first, then the mirror.",
			coverageMirrorPath, stale)
	}
}
