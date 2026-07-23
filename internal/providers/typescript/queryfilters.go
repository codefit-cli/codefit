package typescript

import (
	"strings"
	"unicode"

	"github.com/codefit-cli/codefit/internal/core/db"
	"github.com/codefit-cli/codefit/internal/core/query"
	"github.com/codefit-cli/codefit/internal/core/syntax"
	"github.com/codefit-cli/codefit/internal/providers"
)

// compile-time check: the TS provider implements the query-extraction capability
// (resolved by type-assertion, ADR 0014/0029), alongside SchemaParser.
var _ providers.QueryExtractor = (*Provider)(nil)

// ExtractQueryFilters extracts the neutral QueryFilter of every Prisma query
// call-site in src that FILTERS by one or more columns (has a WHERE). It is a pure
// AST fact — the code side of the code↔schema cross — reusing the same call-site
// machinery as N+1 and over-fetching (walkTS/isPrismaCall/prismaCallInfo). A call
// with no WHERE (a create, a bare findMany, an aggregate without filter) yields no
// filter: there is nothing to cross against an index.
func (p *Provider) ExtractQueryFilters(src providers.SourceFile) ([]query.QueryFilter, error) {
	root, err := p.Parse(src)
	if err != nil {
		return nil, err
	}
	var out []query.QueryFilter
	walkTS(root, func(n syntax.Node) {
		if n.Type() != "call_expression" || !isPrismaCall(n) {
			return
		}
		cols, composite := prismaWhereColumns(n)
		if len(cols) == 0 {
			return
		}
		model, _ := prismaCallInfo(n)
		out = append(out, query.QueryFilter{
			Model:     modelToSchemaName(model),
			Columns:   cols,
			Composite: composite,
			Pos:       db.Pos{File: src.Path, Line: n.StartLine()},
		})
	})
	return out, nil
}

// modelToSchemaName maps a Prisma Client accessor (the model with a lower-cased
// first letter — "user", "userProfile") back to the schema model name ("User",
// "UserProfile") by re-capitalising the first letter, the exact inverse of
// Prisma's accessor derivation. This convention is PRISMA knowledge and lives HERE
// in the provider; the core cross-runner matches the result EXACTLY against
// db.Table.Name and never re-normalises (ADR 0029). A name whose casing this
// inverse does not recover is an honest abstain at reconcile, never a forced match.
func modelToSchemaName(accessor string) string {
	if accessor == "" {
		return ""
	}
	r := []rune(accessor)
	r[0] = unicode.ToUpper(r[0])
	return string(r)
}

// prismaWhereColumns returns the columns a Prisma call's WHERE constrains, and
// whether it constrains more than one together (the composite signal for DB-013).
//
// THE TRAP this locks: it reads args[0].where SPECIFICALLY — the same options-
// object walk hasSelectOrOmit uses — so a data:{…} (create/update), select:{…},
// include:{…} or orderBy:{…} object whose keys LOOK like columns is NEVER mistaken
// for a filter. Only WHERE filters. A top-level AND (object or array) is recursed
// and merges into the same column group; OR and NOT are a DECLARED LIMIT this
// slice — skipped, never guessed (their columns do not reduce to a single
// indexable set). A key naming a relation filter (profile:{…}) is emitted as a
// candidate and dropped at reconcile, which alone knows it is not a real column.
func prismaWhereColumns(call syntax.Node) (cols []string, composite bool) {
	where := prismaWhereObject(call)
	if where == nil {
		return nil, false
	}
	seen := map[string]bool{}
	collectWhereKeys(where, seen, &cols)
	return cols, len(cols) > 1
}

// prismaWhereObject returns the object value of the call's top-level `where` key,
// or nil when the call has no options object or no where clause. It inspects only
// the options object's DIRECT keys, so a nested field named "where" never counts.
func prismaWhereObject(call syntax.Node) syntax.Node {
	args := field(call, "arguments", 1)
	if args == nil || args.NamedChildCount() == 0 {
		return nil
	}
	opts := args.NamedChild(0)
	if opts.Type() != "object" {
		return nil
	}
	for i := 0; i < opts.NamedChildCount(); i++ {
		pair := opts.NamedChild(i)
		if pair.Type() != "pair" {
			continue
		}
		if k := field(pair, "key", 0); k == nil || pairKeyName(k) != "where" {
			continue
		}
		if v := field(pair, "value", 1); v != nil && v.Type() == "object" {
			return v
		}
		return nil // a where that is not an object literal (a variable) is not walked
	}
	return nil
}

// collectWhereKeys gathers the constrained column names from a where object into
// cols (deduped via seen, in encounter order). AND (object or array) is recursed
// and merged; OR/NOT are skipped (declared limit). Every other key — bare
// (email:), quoted ("email":) or shorthand ({id}) — is a candidate column.
func collectWhereKeys(obj syntax.Node, seen map[string]bool, cols *[]string) {
	add := func(name string) {
		if name != "" && !seen[name] {
			seen[name] = true
			*cols = append(*cols, name)
		}
	}
	for i := 0; i < obj.NamedChildCount(); i++ {
		child := obj.NamedChild(i)
		switch child.Type() {
		case "shorthand_property_identifier":
			// `where: { id }` — shorthand for id: id, a filter by id.
			add(string(child.Text()))
		case "pair":
			k := field(child, "key", 0)
			if k == nil {
				continue
			}
			switch name := pairKeyName(k); name {
			case "AND":
				recurseLogical(field(child, "value", 1), seen, cols)
			case "OR", "NOT":
				// Declared limit this slice: logical grouping that does not reduce to a
				// single indexable column set (ADR 0029). Skipped, never guessed.
			default:
				add(name)
			}
		}
	}
}

// recurseLogical merges the constrained columns of an AND clause, which Prisma
// writes as a single object (AND: {…}) or an array of objects (AND: [{…}, {…}]).
func recurseLogical(v syntax.Node, seen map[string]bool, cols *[]string) {
	if v == nil {
		return
	}
	switch v.Type() {
	case "object":
		collectWhereKeys(v, seen, cols)
	case "array":
		for j := 0; j < v.NamedChildCount(); j++ {
			if el := v.NamedChild(j); el.Type() == "object" {
				collectWhereKeys(el, seen, cols)
			}
		}
	}
}

// pairKeyName returns the identifier text of an object-pair key, whether written
// bare (email:) or quoted ("email":). Empty for a computed or other key shape.
func pairKeyName(k syntax.Node) string {
	switch k.Type() {
	case "property_identifier", "identifier":
		return string(k.Text())
	case "string":
		return strings.Trim(string(k.Text()), `"'`+"`")
	}
	return ""
}
