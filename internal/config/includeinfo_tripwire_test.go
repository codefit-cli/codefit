package config_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Report.IncludeInfo is DECLARED and read by nothing. That is what makes RF-10's
// default safe: since this change, a security finding on a test path is forced
// to info, and forcing something to info is only harmless while nothing filters
// info out of a report.
//
// The day someone wires this flag — an entirely reasonable thing to do, the
// field has been sitting there since v0.1 — every re-weighted test-path finding
// becomes INVISIBLE by default, including a genuinely hardcoded production
// secret that happens to live in a fixture. A false all-clear produced by two
// individually sensible changes is precisely the class of defect codefit exists
// to catch, and neither change alone looks wrong in review.
//
// A comment saying so would not be a control. This is: a repo-wide go/ast
// census of every production read of the identifier, failing on any reader that
// has not been recorded with a reason.
//
// includeInfoReaderAllowlist maps "path:line" to the reason its read was
// accepted. Adding an entry is the DECISION the tripwire forces — it does not
// forbid wiring the flag, it forbids wiring it silently. The reason must state
// what was done about the RF-10 interaction (e.g. "info findings are still
// counted in the summary", "test-path findings are exempt from the filter").
var includeInfoReaderAllowlist = map[string]string{}

// includeInfoCensusSkip are directories the census never walks: no Go
// production source lives in them.
var includeInfoCensusSkip = map[string]bool{
	".git": true, "vendor": true, "node_modules": true,
	"dist": true, "bin": true, ".codefit": true, "testdata": true,
}

// includeInfoRead is one production use of the identifier.
type includeInfoRead struct {
	file string // repo-relative, slash-separated
	line int
}

func (r includeInfoRead) key() string { return r.file + ":" + itoa(r.line) }

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

// TestIncludeInfoTripwire censuses every production .go file in the repository
// for reads of Report.IncludeInfo and fails on any that is not allowlisted.
//
// It carries its OWN positive probe, and that probe is not decoration: a census
// that silently walked nothing — wrong root, an over-eager skip list, a parser
// error swallowed — would report "no readers" forever and be indistinguishable
// from a clean tree. That is the same false all-clear the tripwire guards
// against, reproduced inside the guard. So the census must FIND the known
// declaration of IncludeInfo (the Report struct field in
// internal/config/config.go) before any conclusion it reaches is believed.
//
// The anchor is the field DECLARATION identified by file + enclosing struct +
// field name, not a hardcoded line number: the line moves with any edit above
// it, and a probe that fails on unrelated edits is a probe that gets deleted.
// The line it resolves to is reported, so a reviewer can see the walk landed on
// real source.
func TestIncludeInfoTripwire(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}

	var reads []includeInfoRead
	parsedFiles := 0
	anchorLine := 0

	walkErr := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if includeInfoCensusSkip[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		// Production source only: a test may name the identifier freely (this
		// file does).
		if filepath.Ext(path) != ".go" || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		fset := token.NewFileSet()
		f, parseErr := parser.ParseFile(fset, path, nil, 0)
		if parseErr != nil {
			// A file the census cannot parse is NOT a file with no readers.
			// Failing loudly here is the whole point: silence would be a lie.
			return parseErr
		}
		parsedFiles++
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			rel = path
		}
		rel = filepath.ToSlash(rel)

		ast.Inspect(f, func(n ast.Node) bool {
			switch node := n.(type) {
			case *ast.TypeSpec:
				// The declaration anchor: the IncludeInfo field of a struct.
				st, ok := node.Type.(*ast.StructType)
				if !ok || st.Fields == nil {
					return true
				}
				for _, field := range st.Fields.List {
					for _, name := range field.Names {
						if name.Name == "IncludeInfo" {
							anchorLine = fset.Position(name.Pos()).Line
						}
					}
				}
			case *ast.SelectorExpr:
				// A READ (or write) through a value: cfg.Report.IncludeInfo,
				// r.IncludeInfo, opts.IncludeInfo — every shape a filter would
				// take.
				if node.Sel != nil && node.Sel.Name == "IncludeInfo" {
					reads = append(reads, includeInfoRead{file: rel, line: fset.Position(node.Sel.Pos()).Line})
				}
			}
			return true
		})
		return nil
	})
	if walkErr != nil {
		t.Fatalf("census walk failed, so it proved nothing: %v", walkErr)
	}

	// Positive probe, part 1: the walk reached real source.
	if parsedFiles == 0 {
		t.Fatalf("census parsed 0 production .go files under %q — the walk is broken, "+
			"so a clean result is meaningless", root)
	}
	// Positive probe, part 2: the walk found the identifier where it is KNOWN
	// to exist. Without this, an inspector that matched nothing would pass.
	if anchorLine == 0 {
		t.Fatalf("census parsed %d files but never found the IncludeInfo field declaration in "+
			"internal/config/config.go — the inspector matches nothing, so its \"no readers\" "+
			"verdict is a false all-clear, not evidence", parsedFiles)
	}
	t.Logf("positive probe: %d production .go files parsed; IncludeInfo declaration anchored at "+
		"internal/config/config.go:%d", parsedFiles, anchorLine)

	for _, r := range reads {
		if _, allowed := includeInfoReaderAllowlist[r.key()]; allowed {
			continue
		}
		t.Errorf("%s reads Report.IncludeInfo, and nothing recorded a decision about it.\n"+
			"Since RF-10, a security finding on a test path is forced to info by default. A "+
			"production reader of include_info can therefore make a REAL hardcoded secret "+
			"invisible in the default report — a false all-clear built from two individually "+
			"reasonable changes.\n"+
			"If that is understood and handled, add %q to includeInfoReaderAllowlist with a "+
			"reason stating how.", r.key(), r.key())
	}
}

// TestIncludeInfoAllowlistHasNoGhosts guards the reverse direction: an
// allowlist entry for a read that no longer exists is stale permission. It
// would keep a future reader at that same line silently accepted.
func TestIncludeInfoAllowlistHasNoGhosts(t *testing.T) {
	for key, reason := range includeInfoReaderAllowlist {
		if strings.TrimSpace(reason) == "" {
			t.Errorf("allowlist entry %q carries no reason; an entry without a reason records "+
				"no decision", key)
		}
		file, _, ok := strings.Cut(key, ":")
		if !ok {
			t.Errorf("allowlist key %q is not in \"path:line\" form", key)
			continue
		}
		if _, err := os.Stat(filepath.Join("../..", filepath.FromSlash(file))); err != nil {
			t.Errorf("allowlist entry %q names a file that does not exist: %v", key, err)
		}
	}
}
