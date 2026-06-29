package typescript

import (
	"strings"

	"github.com/codefit-cli/codefit/internal/core/syntax"
)

// expressVerbs are the Express/Fastify router methods that register a route
// handler. Detection is by SHAPE (ADR 0005): the method name alone is not enough
// — a `map.get(...)` or `arr.filter(...)` shares the name — so a handler also
// requires a string path as the first argument and a function as the last.
var expressVerbs = map[string]bool{
	"get": true, "post": true, "put": true, "patch": true,
	"delete": true, "options": true, "head": true, "all": true,
}

// expressHandlersIn discovers Express route handlers anywhere in a file by shape:
// a `<router>.<verb>('/path', ...middleware, handler)` call whose FIRST argument
// is a string-literal path and whose LAST argument is an inline function. The
// shape discriminator rejects same-named non-route calls (map.get('/k', v) — last
// arg is not a function; arr.get(0, cb) — first arg is not a string), so codefit
// does not over-enumerate by method name. A handler passed by reference (a named
// identifier, not an inline function) is a cross-scope case codefit does not
// resolve here — it is not enumerated, a known limit declared in coverage.
func expressHandlersIn(root syntax.Node) []tsHandler {
	var out []tsHandler
	walkTS(root, func(n syntax.Node) {
		if n.Type() != "call_expression" {
			return
		}
		fn := field(n, "function", 0)
		if fn == nil || fn.Type() != "member_expression" {
			return
		}
		verb := field(fn, "property", 1)
		if verb == nil || !expressVerbs[string(verb.Text())] {
			return
		}
		args := field(n, "arguments", 1)
		if args == nil || args.NamedChildCount() < 2 {
			return
		}
		first := args.NamedChild(0)
		last := args.NamedChild(args.NamedChildCount() - 1)
		if first.Type() != "string" {
			return
		}
		if last.Type() != "arrow_function" && last.Type() != "function_expression" {
			return
		}
		body := bodyBlock(last)
		if body == nil {
			return
		}
		// Arguments between the path (first) and the handler (last) are route
		// middleware — where the auth guard lives in Express/Fastify.
		var middleware []syntax.Node
		for i := 1; i < args.NamedChildCount()-1; i++ {
			middleware = append(middleware, args.NamedChild(i))
		}
		out = append(out, tsHandler{
			kind:       "route",
			framework:  "express",
			method:     strings.ToUpper(string(verb.Text())),
			params:     field(last, "parameters", 0),
			body:       body,
			middleware: middleware,
			line:       n.StartLine(),
			snippet:    firstLine(string(n.Text())),
		})
	})
	return out
}

// fastifyHandlersIn discovers Fastify routes in the OPTIONS-OBJECT form:
// <fastify>.<verb>('/path', { ...opts, handler: async (request, reply) => ... }).
// The simple positional-function form (fastify.get(path, fn)) is already covered
// by expressHandlersIn (same shape; the framework label does not change behaviour,
// since inputs/sink/authz-scope are shared across the two). Fastify's guard lives
// in the options object as preHandler/onRequest (a reference or an array), not as
// a positional argument, so those are collected as middleware for the authz signal.
func fastifyHandlersIn(root syntax.Node) []tsHandler {
	var out []tsHandler
	walkTS(root, func(n syntax.Node) {
		if n.Type() != "call_expression" {
			return
		}
		fn := field(n, "function", 0)
		if fn == nil || fn.Type() != "member_expression" {
			return
		}
		verb := field(fn, "property", 1)
		if verb == nil || !expressVerbs[string(verb.Text())] {
			return
		}
		args := field(n, "arguments", 1)
		if args == nil || args.NamedChildCount() < 2 || args.NamedChild(0).Type() != "string" {
			return
		}
		opts := args.NamedChild(args.NamedChildCount() - 1)
		if opts.Type() != "object" {
			return
		}
		handler := objectPropFunction(opts, "handler")
		if handler == nil {
			return
		}
		body := bodyBlock(handler)
		if body == nil {
			return
		}
		out = append(out, tsHandler{
			kind:       "route",
			framework:  "fastify",
			method:     strings.ToUpper(string(verb.Text())),
			params:     field(handler, "parameters", 0),
			body:       body,
			middleware: fastifyGuards(opts),
			line:       n.StartLine(),
			snippet:    firstLine(string(n.Text())),
		})
	})
	return out
}

// objectPropValue returns the value node of the first pair in obj whose key is
// name (handling string-quoted keys); nil if absent.
func objectPropValue(obj syntax.Node, name string) syntax.Node {
	for i := 0; i < obj.NamedChildCount(); i++ {
		pair := obj.NamedChild(i)
		if pair.Type() != "pair" {
			continue
		}
		if k := field(pair, "key", 0); k != nil && strings.Trim(string(k.Text()), `"'`+"`") == name {
			return field(pair, "value", 1)
		}
	}
	return nil
}

// objectPropFunction returns the value of obj's named property when it is an
// inline function; nil otherwise (a handler passed by reference is a cross-scope
// case codefit does not resolve here — declared in coverage).
func objectPropFunction(obj syntax.Node, name string) syntax.Node {
	v := objectPropValue(obj, name)
	if v != nil && (v.Type() == "arrow_function" || v.Type() == "function_expression") {
		return v
	}
	return nil
}

// fastifyGuards collects a route's preHandler/onRequest hook references (a single
// reference or an array of them) as middleware nodes for the authz signal.
func fastifyGuards(opts syntax.Node) []syntax.Node {
	var out []syntax.Node
	for _, key := range []string{"preHandler", "onRequest"} {
		v := objectPropValue(opts, key)
		if v == nil {
			continue
		}
		if v.Type() == "array" {
			for i := 0; i < v.NamedChildCount(); i++ {
				out = append(out, v.NamedChild(i))
			}
			continue
		}
		out = append(out, v)
	}
	return out
}

// nestHTTPVerbs maps a NestJS method decorator name to its HTTP verb. A class
// method carrying one of these decorators IS a route handler (detection by shape,
// never by @Controller — ADR 0005).
var nestHTTPVerbs = map[string]string{
	"Get": "GET", "Post": "POST", "Put": "PUT", "Patch": "PATCH",
	"Delete": "DELETE", "Options": "OPTIONS", "Head": "HEAD", "All": "ALL",
}

// nestHandlersIn discovers NestJS route handlers: class methods decorated with an
// HTTP-verb decorator (@Get/@Post/...). NestJS handlers are class methods, not
// call_expressions, so the decorators precede their target as siblings — class
// decorators (@Controller, @UseGuards) before the class_declaration, method
// decorators before the method_definition. It scans each container's children in
// order so a class's own @UseGuards (inherited by every method) is captured and
// passed down. The body/params come straight off the method, so the shared
// tsHandler mold and all downstream logic apply unchanged.
func nestHandlersIn(root syntax.Node) []tsHandler {
	var out []tsHandler
	var visit func(n syntax.Node)
	visit = func(n syntax.Node) {
		var pending []syntax.Node
		for i := 0; i < n.NamedChildCount(); i++ {
			c := n.NamedChild(i)
			switch c.Type() {
			case "decorator":
				pending = append(pending, c)
			case "class_declaration":
				out = append(out, nestClassHandlers(c, useGuardsArgs(pending))...)
				pending = nil
			default:
				pending = nil
			}
			visit(c)
		}
	}
	visit(root)
	return out
}

// nestClassHandlers emits a handler for each HTTP-verb-decorated method in a class
// body. Method decorators precede their method_definition as siblings, so it
// accumulates the pending decorators and attaches them to the next method.
// classGuards are the class-level @UseGuards args, inherited by every method, and
// combined with the method's own guards into tsHandler.middleware.
func nestClassHandlers(class syntax.Node, classGuards []syntax.Node) []tsHandler {
	body := childOfType(class, "class_body")
	if body == nil {
		return nil
	}
	var out []tsHandler
	var pending []syntax.Node
	for i := 0; i < body.NamedChildCount(); i++ {
		c := body.NamedChild(i)
		switch c.Type() {
		case "decorator":
			pending = append(pending, c)
		case "method_definition":
			if verb := nestHTTPVerb(pending); verb != "" {
				if stmt := childOfType(c, "statement_block"); stmt != nil {
					guards := append(append([]syntax.Node{}, classGuards...), useGuardsArgs(pending)...)
					out = append(out, tsHandler{
						kind:       "route",
						framework:  "nestjs",
						method:     verb,
						params:     childOfType(c, "formal_parameters"),
						body:       stmt,
						middleware: guards,
						line:       c.StartLine(),
						snippet:    nestSnippet(pending, c),
					})
				}
			}
			pending = nil
		default:
			pending = nil
		}
	}
	return out
}

// useGuardsArgs returns the guard arguments of every @UseGuards decorator in the
// set (the guard class references) — what codefit reports as the applied guards.
func useGuardsArgs(decorators []syntax.Node) []syntax.Node {
	var out []syntax.Node
	for _, d := range decorators {
		if decoratorCallee(d) != "UseGuards" || d.NamedChildCount() == 0 {
			continue
		}
		call := d.NamedChild(0)
		if call.Type() != "call_expression" {
			continue
		}
		if args := field(call, "arguments", 1); args != nil {
			for i := 0; i < args.NamedChildCount(); i++ {
				out = append(out, args.NamedChild(i))
			}
		}
	}
	return out
}

// nestHTTPVerb returns the HTTP verb if any decorator in the set is an HTTP-verb
// decorator, else "".
func nestHTTPVerb(decorators []syntax.Node) string {
	for _, d := range decorators {
		if v, ok := nestHTTPVerbs[decoratorCallee(d)]; ok {
			return v
		}
	}
	return ""
}

// nestSnippet builds a readable label for a NestJS handler: the verb decorator
// plus the method name (e.g. "@Get(':id') findOne").
func nestSnippet(decorators []syntax.Node, method syntax.Node) string {
	name := ""
	if id := childOfType(method, "property_identifier"); id != nil {
		name = string(id.Text())
	}
	for _, d := range decorators {
		if _, ok := nestHTTPVerbs[decoratorCallee(d)]; ok {
			return strings.TrimSpace(firstLine(string(d.Text())) + " " + name)
		}
	}
	return name
}

// decoratorCallee returns the name a decorator invokes: the identifier of its call
// (@Get(':id') → "Get") or a bare identifier (@Injectable → "Injectable").
func decoratorCallee(dec syntax.Node) string {
	if dec.NamedChildCount() == 0 {
		return ""
	}
	c := dec.NamedChild(0)
	switch c.Type() {
	case "identifier":
		return string(c.Text())
	case "call_expression":
		if fn := field(c, "function", 0); fn != nil && fn.Type() == "identifier" {
			return string(fn.Text())
		}
	}
	return ""
}

// decoratorStringArg returns the first string-literal argument of a decorator's
// call (@Param('id') → "id"), or "" when there is none.
func decoratorStringArg(dec syntax.Node) string {
	if dec.NamedChildCount() == 0 || dec.NamedChild(0).Type() != "call_expression" {
		return ""
	}
	args := field(dec.NamedChild(0), "arguments", 1)
	if args == nil || args.NamedChildCount() == 0 || args.NamedChild(0).Type() != "string" {
		return ""
	}
	return strings.Trim(string(args.NamedChild(0).Text()), `"'`+"`")
}

// childOfType returns the first direct named child of the given type, or nil.
func childOfType(n syntax.Node, t string) syntax.Node {
	for i := 0; i < n.NamedChildCount(); i++ {
		if c := n.NamedChild(i); c.Type() == t {
			return c
		}
	}
	return nil
}

// isRequestIDAccess reports whether node is a client-controlled request access of
// the form <req>.params.<x> / <req>.query.<x> / <req>.body.<x> — the Express and
// Fastify idiom for reading path params, query strings, and the body. Route
// params and query strings are client-supplied by definition, so every such
// access is treated as an id-input (matched by structure, not by the param's
// name, so a non-standard id like `slug` is not a blind spot — ADR 0005).
func isRequestIDAccess(node syntax.Node) bool {
	if node.Type() != "member_expression" {
		return false
	}
	obj := field(node, "object", 0)
	if obj == nil || obj.Type() != "member_expression" {
		return false
	}
	base := field(obj, "object", 0)
	mid := field(obj, "property", 1)
	if base == nil || mid == nil || base.Type() != "identifier" {
		return false
	}
	return requestSources[string(mid.Text())]
}

// requestSources are the request members that carry client-controlled input.
var requestSources = map[string]bool{"params": true, "query": true, "body": true}
