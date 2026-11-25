package golang_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/codefit-cli/codefit/internal/core/namematch"
	"github.com/codefit-cli/codefit/internal/core/valueshape"
	"github.com/codefit-cli/codefit/internal/providers/golang"
)

// TestCapability_PRAC004DeclaredPermanentlyExcluded is P1-4b's owed manifest
// entry: PRAC-004 was permanently dropped (ADR 0056 — it fired on every `go`
// statement with no synchronization detection at all) and must stay stated in
// Go's capability declaration, with its reason, rather than silently vanish
// if Practices.Declared is ever edited by someone who never reads the ADR.
func TestCapability_PRAC004DeclaredPermanentlyExcluded(t *testing.T) {
	cap := golang.New().Capability()

	var reason string
	var found bool
	for _, ex := range cap.Practices.Excluded {
		if ex.ID == "PRAC-004" {
			found = true
			reason = ex.Reason
		}
	}
	if !found {
		t.Fatal("golang.Provider.Capability().Practices.Excluded does not name PRAC-004 — " +
			"its permanent drop (ADR 0056) must be declared, not silently omitted (P1-4b)")
	}
	if reason == "" {
		t.Error("PRAC-004's ExcludedRule must carry a non-empty Reason")
	}
	if !cap.Practices.ValidExclusions() {
		t.Error("Practices.Excluded overlaps Practices.Declared — PRAC-004 must not be claimed as both covered and excluded (C6)")
	}
}

// TestCapability_SEC001DeclaresTheValueVeto keeps the value veto (ADR 0086) a
// DECLARED limit of SEC-001 rather than a silent one. A literal the veto
// silences is, from the agent's side, indistinguishable from "no credential
// here" unless the capability says the rule skips those shapes.
//
// The declaration must name every form internal/core/valueshape can veto: a
// form added there and not declared here would be exactly the undeclared
// blindness the census doctrine exists to prevent. It must also keep the name
// gate's own limit (ADR 0075) — the two qualify the same rule and neither
// replaces the other.
func TestCapability_SEC001DeclaresTheValueVeto(t *testing.T) {
	sec := golang.New().Capability().Security
	if ok, orphan := sec.ValidLimits(); !ok {
		t.Fatalf("security limits reference undeclared rule ids %v", orphan)
	}

	var limits []string
	for _, l := range sec.Limits {
		if l.ID == "SEC-001" {
			limits = append(limits, l.Limit)
		}
	}
	if len(limits) != 2 {
		t.Fatalf("SEC-001 carries %d declared limits, want 2 (name gate + value veto): %q", len(limits), limits)
	}

	var veto string
	for _, l := range limits {
		if strings.Contains(l, "internal/core/valueshape") {
			veto = l
		}
	}
	if veto == "" {
		t.Fatalf("no SEC-001 limit declares the value veto: %q", limits)
	}
	// Each form must be named AS ITSELF, in the limit's own form list. A plain
	// substring check is satisfied by prose that merely contains the letters:
	// "urn" by "a urn: URN", "url" by "a URL", so a form dropped from the list
	// would still pass. The list is parsed and compared as a set.
	declared := declaredForms(t, veto)
	for _, form := range valueshape.Forms() {
		if !declared[string(form)] {
			t.Errorf("the value-veto limit's form list does not name %q, which the provider silences: %v", form, declared)
		}
		delete(declared, string(form))
	}
	for extra := range declared {
		t.Errorf("the value-veto limit's form list names %q, which valueshape does not declare", extra)
	}
	// The limit text is the shared constant, cited and not retyped: the Go
	// provider adds only how its literal reaches the predicate. Checked beside
	// the form-list parse above, never instead of it: an emptied constant would
	// satisfy this line alone, and a second hand-written copy beside the
	// constant would carry a second form list, which declaredForms refuses.
	if valueshape.DeclaredLimit == "" || !strings.Contains(veto, valueshape.DeclaredLimit) {
		t.Errorf("the Go value-veto limit does not carry valueshape.DeclaredLimit verbatim:\n%s", veto)
	}
	if !slices.Contains(limits, namematch.LimitLowercaseConcatenation) {
		t.Error("declaring the value veto dropped SEC-001's name-gate limit (ADR 0075)")
	}
}

// valueVetoSite is one entry of the site list the Go prefix of the value-veto
// limit declares: the phrase the served text uses, the go/ast node type
// security.go dispatches to a secret check for it, and real Go source for every
// spelling the phrase covers. src takes a name and Go SOURCE for a string
// literal, quotes included.
type valueVetoSite struct {
	phrase  string
	astType string
	srcs    map[string]func(name, lit string) string
}

var valueVetoSites = []valueVetoSite{
	{
		phrase:  "an assignment",
		astType: "AssignStmt",
		srcs: map[string]func(name, lit string) string{
			"define": func(name, lit string) string {
				return "package p\n\nfunc f() { " + name + " := " + lit + "; _ = " + name + " }\n"
			},
			// The var spec carries no value, so it never reaches the gate on
			// its own: the one finding the control expects is the assignment's.
			"assign": func(name, lit string) string {
				return "package p\n\nvar " + name + " string\n\nfunc f() { " + name + " = " + lit + " }\n"
			},
		},
	},
	{
		phrase:  "a const or var spec",
		astType: "ValueSpec",
		srcs: map[string]func(name, lit string) string{
			"const": func(name, lit string) string { return "package p\n\nconst " + name + " = " + lit + "\n" },
			"var":   func(name, lit string) string { return "package p\n\nvar " + name + " = " + lit + "\n" },
		},
	},
	{
		phrase:  "a composite-literal element keyed by an identifier",
		astType: "KeyValueExpr",
		srcs: map[string]func(name, lit string) string{
			"struct field": func(name, lit string) string {
				return "package p\n\ntype Cfg struct{ " + name + " string }\n\n" +
					"func f() Cfg { return Cfg{" + name + ": " + lit + "} }\n"
			},
		},
	},
}

// valueVetoSiteList is the shape the Go prefix uses to enumerate its sites:
// "at every site SEC-001 reaches (a, b, c)".
var valueVetoSiteList = regexp.MustCompile(`at every site SEC-001 reaches \(([^()]+)\)`)

// TestCapability_SEC001ValueVetoPrefixNamesEverySiteTheVetoRunsAt pins the
// Go-specific prefix of the value-veto limit against the code, not against a
// copy of its text. Three things must agree:
//
//  1. the sites SEC-001 reaches, read from security.go's own dispatch (every
//     case of securityChecks' type switch that calls a checkSecret function);
//  2. the site list the served prefix declares, parsed as a set;
//  3. the veto observed at each of those sites, through the real provider:
//     the same name fires on a value that proves nothing and is silent on one
//     literal per valueshape.Form.
//
// A site added to the code and not to the text, a site dropped from the text,
// a site the text names where the veto is not shown to apply, or a prefix
// deleted outright each fail here.
func TestCapability_SEC001ValueVetoPrefixNamesEverySiteTheVetoRunsAt(t *testing.T) {
	var veto string
	for _, l := range golang.New().Capability().Security.Limits {
		if l.ID == "SEC-001" && strings.Contains(l.Limit, valueshape.DeclaredLimit) {
			veto = l.Limit
		}
	}
	if valueshape.DeclaredLimit == "" || veto == "" {
		t.Fatal("no SEC-001 limit carries valueshape.DeclaredLimit")
	}
	if !strings.HasSuffix(veto, valueshape.DeclaredLimit) {
		t.Fatalf("the value-veto limit does not END with valueshape.DeclaredLimit, so its Go prefix cannot be read:\n%s", veto)
	}
	prefix := strings.TrimSuffix(veto, valueshape.DeclaredLimit)

	// 1. The code's sites.
	dispatched := secretCheckDispatch(t)
	table := make(map[string]bool, len(valueVetoSites))
	for _, site := range valueVetoSites {
		table[site.astType] = true
		if !dispatched[site.astType] {
			t.Errorf("the test table maps %q to *ast.%s, which securityChecks no longer dispatches to a secret check", site.phrase, site.astType)
		}
	}
	for astType := range dispatched {
		if !table[astType] {
			t.Errorf("securityChecks dispatches *ast.%s to a secret check, and no site in the prefix's list stands for it", astType)
		}
	}

	// 2. The text's sites.
	lists := valueVetoSiteList.FindAllStringSubmatch(prefix, -1)
	if len(lists) != 1 {
		t.Fatalf("the Go prefix carries %d site lists (\"at every site SEC-001 reaches (a, b)\"), want exactly 1:\n%q", len(lists), prefix)
	}
	named := make(map[string]bool)
	for _, phrase := range strings.Split(lists[0][1], ", ") {
		named[phrase] = true
	}
	for _, site := range valueVetoSites {
		if !named[site.phrase] {
			t.Errorf("the Go prefix's site list does not name %q, a site the veto runs at: %q", site.phrase, lists[0][1])
		}
		delete(named, site.phrase)
	}
	for extra := range named {
		t.Errorf("the Go prefix's site list names %q, where no test shows the veto applies", extra)
	}

	// 3. The veto, observed at every spelling of every site.
	for _, site := range valueVetoSites {
		for spelling, src := range site.srcs {
			t.Run(site.astType+"/"+spelling, func(t *testing.T) {
				if got := sec001Of(t, src(gatedName, strconv.Quote("s3cr3t"))); len(got) != 1 {
					t.Fatalf("vacuum: %s with a plain short literal emitted %d SEC-001, want 1", spelling, len(got))
				}
				for _, form := range valueshape.Forms() {
					value := vetoedForms[form]
					if got, ok := valueshape.NotACredential(value); !ok || got != form {
						t.Fatalf("fixture %q is filed under %q but valueshape says (%q, %v)", value, form, got, ok)
					}
					if got := sec001Of(t, src(gatedName, strconv.Quote(value))); len(got) != 0 {
						t.Errorf("%s: %q proves it is a %s and SEC-001 still fired: %+v", spelling, value, form, got)
					}
				}
			})
		}
	}
}

// secretCheckDispatch reads security.go with go/ast and returns the node types
// securityChecks dispatches to a function whose name starts with checkSecret.
// An empty result fails: the comparison would otherwise be made against
// nothing.
func secretCheckDispatch(t *testing.T) map[string]bool {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), "security.go", nil, 0)
	if err != nil {
		t.Fatalf("parse security.go: %v", err)
	}
	types := make(map[string]bool)
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Name.Name != "securityChecks" {
			continue
		}
		ast.Inspect(fn, func(n ast.Node) bool {
			clause, ok := n.(*ast.CaseClause)
			if !ok {
				return true
			}
			callsSecretCheck := false
			for _, stmt := range clause.Body {
				ast.Inspect(stmt, func(m ast.Node) bool {
					if call, ok := m.(*ast.CallExpr); ok {
						if id, ok := call.Fun.(*ast.Ident); ok && strings.HasPrefix(id.Name, "checkSecret") {
							callsSecretCheck = true
						}
					}
					return true
				})
			}
			if !callsSecretCheck {
				return true
			}
			for _, expr := range clause.List {
				star, ok := expr.(*ast.StarExpr)
				if !ok {
					t.Fatalf("securityChecks has a secret-check case on a non-pointer type %T", expr)
				}
				sel, ok := star.X.(*ast.SelectorExpr)
				if !ok {
					t.Fatalf("securityChecks has a secret-check case on an unexpected type %T", star.X)
				}
				types[sel.Sel.Name] = true
			}
			return true
		})
	}
	if len(types) == 0 {
		t.Fatal("found no case of securityChecks that calls a checkSecret function; the probe reads nothing")
	}
	return types
}

// declaredFormList is the shape the value-veto limit uses to enumerate its
// forms: "forms a, b, c" — lowercase names joined by ", ".
var declaredFormList = regexp.MustCompile(`forms ([a-z-]+(?:, [a-z-]+)*)`)

// declaredForms parses the limit's form list into a set. A limit with no such
// list, or with more than one, fails: the comparison would otherwise be made
// against nothing, or against a list chosen at random.
func declaredForms(t *testing.T, limit string) map[string]bool {
	t.Helper()
	lists := declaredFormList.FindAllStringSubmatch(limit, -1)
	if len(lists) != 1 {
		t.Fatalf("the value-veto limit carries %d form lists (\"forms a, b, …\"), want exactly 1: %q", len(lists), limit)
	}
	set := make(map[string]bool)
	for _, name := range strings.Split(lists[0][1], ", ") {
		set[name] = true
	}
	return set
}
