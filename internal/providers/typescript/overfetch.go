package typescript

import (
	"github.com/codefit-cli/codefit/internal/core/findings"
	"github.com/codefit-cli/codefit/internal/core/syntax"
)

// overfetchQuery enumerates the over-fetching surface of a Next.js App Router
// file: every point where a domain object is serialized to the response. Unlike
// IDOR/authz (which ask "is access verified?"), this asks "are the exposed fields
// limited?" — it is about data EXPOSURE. codefit reports the structural fact (a
// Prisma find serialized with or without a select/omit clause); it does NOT judge
// whether the exposed fields are sensitive — that requires the domain/schema,
// which is the agent's. It implements surface.Query and reuses the IDOR/authz
// mold (Prisma by shape, the frontier to the agent, the queryable fact).
type overfetchQuery struct{}

func (overfetchQuery) Enumerate(root syntax.Node, file string) []findings.SurfaceItem {
	if root == nil {
		return nil
	}
	var out []findings.SurfaceItem
	for _, h := range auditTargets(root, file) {
		out = append(out, overfetchItems(h, file)...)
	}
	return out
}

// prismaVar is a local variable bound to a Prisma find: its model and whether the
// find limited fields with select/omit.
type prismaVar struct {
	model   string
	limited bool
}

// overfetchItems finds the serializations in one handler body and maps each to a
// surface item. Variable tracking is scoped to the handler body (each handler is
// its own function scope), so a name in one handler is not linked to another.
func overfetchItems(h tsHandler, file string) []findings.SurfaceItem {
	prismaVars := collectPrismaVars(h.body)
	serviceVars := collectServiceVars(h.body)
	var out []findings.SurfaceItem
	walkTS(h.body, func(n syntax.Node) {
		x, ok := serializationSink(h, n)
		if !ok {
			return
		}
		if item, ok := overfetchItem(n, x, prismaVars, serviceVars, file); ok {
			out = append(out, item)
		}
	})
	return out
}

// serializationSink reports whether n is a point where a value is serialized to
// the client, returning the serialized value. For a route handler that is an
// explicit Response.json / NextResponse.json / JSON.stringify call. A Server
// Action has no such call: its RETURN value is serialized by the framework and
// sent to the client, so the return statement's argument is the sink (the
// explicit-call forms still count, e.g. an action that builds a Response).
func serializationSink(h tsHandler, n syntax.Node) (syntax.Node, bool) {
	if x, ok := serializationArg(n); ok {
		return x, true
	}
	if h.framework == "express" || h.framework == "fastify" {
		if x, ok := responseSinkArg(h, n); ok {
			return x, true
		}
	}
	// A Server Action and a NestJS handler have no explicit response call: they
	// RETURN the value and the framework serializes it to the client, so the return
	// statement's argument is the sink.
	if (h.kind == "action" || h.framework == "nestjs") && n.Type() == "return_statement" && n.NamedChildCount() > 0 {
		return n.NamedChild(0), true
	}
	return nil, false
}

// responseSinkArg reports whether call serializes a value to the client through
// the handler's response object — <res>.json(X) or <res>.send(X), where <res> is
// the handler's second formal parameter (Express `res`, Fastify `reply`).
// Returns X. Keyed off the actual parameter name, so a renamed response object is
// not a blind spot; .json and .send are the field-returning sinks (.sendStatus,
// .end and friends carry no domain object).
func responseSinkArg(h tsHandler, call syntax.Node) (syntax.Node, bool) {
	if call.Type() != "call_expression" {
		return nil, false
	}
	res := paramName(h.params, 1)
	if res == "" {
		return nil, false
	}
	fn := field(call, "function", 0)
	if fn == nil || fn.Type() != "member_expression" {
		return nil, false
	}
	obj := field(fn, "object", 0)
	prop := field(fn, "property", 1)
	if obj == nil || prop == nil || obj.Type() != "identifier" || string(obj.Text()) != res {
		return nil, false
	}
	if p := string(prop.Text()); p != "json" && p != "send" {
		return nil, false
	}
	args := field(call, "arguments", 1)
	if args == nil || args.NamedChildCount() == 0 {
		return nil, false
	}
	return args.NamedChild(0), true
}

// overfetchItem classifies one serialized value and builds the item, or reports
// ok=false when the value is not a domain serialization (a plain object, a
// field-limited map, a primitive).
func overfetchItem(call, x syntax.Node, prismaVars map[string]prismaVar, serviceVars map[string]string, file string) (findings.SurfaceItem, bool) {
	xu := unwrapAwait(x)

	var signals []string
	var limited, local bool
	var reason string

	switch {
	case xu.Type() == "call_expression" && isPrismaCall(xu):
		model, method := prismaCallInfo(xu)
		limited, local = hasSelectOrOmit(xu), true
		signals = localOverfetchSignals(model, method, limited)
		reason = overfetchModelReason(model)
	case xu.Type() == "identifier" && inPrismaVars(xu, prismaVars):
		pv := prismaVars[string(xu.Text())]
		limited, local = pv.limited, true
		signals = localOverfetchSignals(pv.model, "find", limited)
		reason = overfetchModelReason(pv.model)
	case xu.Type() == "call_expression" && isServiceCall(xu):
		signals = frontierOverfetchSignals(calleeName(xu))
		reason = overfetchFrontierReason()
	case xu.Type() == "identifier" && inServiceVars(xu, serviceVars):
		signals = frontierOverfetchSignals(serviceVars[string(xu.Text())])
		reason = overfetchFrontierReason()
	default:
		return findings.SurfaceItem{}, false
	}

	return findings.SurfaceItem{
		Category:          "overfetch",
		File:              file,
		Line:              call.StartLine(),
		Snippet:           firstLine(string(call.Text())),
		StructuralSignals: signals,
		StructuralFacts: map[string]bool{
			"local_access_detected":   local,
			"field_limiting_detected": limited,
		},
		ReasonToReview: reason,
	}, true
}

func localOverfetchSignals(model, method string, limited bool) []string {
	limit := "The query has no select/omit clause, so all columns of the model are returned"
	if limited {
		limit = "The query limits the returned fields with a select/omit clause"
	}
	return []string{
		"Serializes the result of prisma." + model + "." + method,
		limit,
		"The serialized model is '" + model + "'",
	}
}

// frontierOverfetchSignals phrases an over-fetch whose serialized value is not a
// local Prisma find. The limit is stated as an AFFIRMATION (codefit does not
// follow the call, so the field selection is an UNRESOLVED candidate, not an
// absence) and following the data is its own instruction, so the agent pursues it
// instead of discarding it.
func frontierOverfetchSignals(callee string) []string {
	return []string{
		"Serializes the result of " + callee,
		"This is not a local Prisma find, so codefit does not follow it into the function that produced it — " +
			"whether a select/omit limits the returned fields is NOT verified here; the selection lives in a " +
			"service/repository layer. Follow the data to its source and check the Prisma schema to determine " +
			"whether sensitive fields reach the client.",
	}
}

func overfetchModelReason(model string) string {
	return "Does the model '" + model + "' have sensitive fields (password hashes, tokens, personal " +
		"data) that should not be returned to the client, and should this serialization use a " +
		"select/omit to limit them per the Prisma schema?"
}

func overfetchFrontierReason() string {
	return "Does the serialized data include sensitive fields that should not be returned to the " +
		"client — following the data to its source and the Prisma schema to confirm?"
}

// serializationArg reports whether call is a response serialization
// (Response.json(X) / NextResponse.json(X) / JSON.stringify(X)) and returns X.
func serializationArg(call syntax.Node) (syntax.Node, bool) {
	if call.Type() != "call_expression" {
		return nil, false
	}
	fn := field(call, "function", 0)
	if fn == nil || fn.Type() != "member_expression" {
		return nil, false
	}
	obj := field(fn, "object", 0)
	prop := field(fn, "property", 1)
	if obj == nil || prop == nil || obj.Type() != "identifier" {
		return nil, false
	}
	o, p := string(obj.Text()), string(prop.Text())
	isJSON := (o == "Response" || o == "NextResponse") && p == "json"
	isStringify := o == "JSON" && p == "stringify"
	if !isJSON && !isStringify {
		return nil, false
	}
	args := field(call, "arguments", 1)
	if args == nil || args.NamedChildCount() == 0 {
		return nil, false
	}
	return args.NamedChild(0), true
}

// collectPrismaVars maps local variables bound to a Prisma find to that find's
// model and whether it limits fields.
func collectPrismaVars(body syntax.Node) map[string]prismaVar {
	out := map[string]prismaVar{}
	walkTS(body, func(n syntax.Node) {
		if n.Type() != "variable_declarator" {
			return
		}
		name := field(n, "name", 0)
		val := field(n, "value", 1)
		if name == nil || val == nil || name.Type() != "identifier" {
			return
		}
		v := unwrapAwait(val)
		if v.Type() == "call_expression" && isPrismaCall(v) {
			model, _ := prismaCallInfo(v)
			out[string(name.Text())] = prismaVar{model: model, limited: hasSelectOrOmit(v)}
		}
	})
	return out
}

// collectServiceVars maps local variables bound to a non-Prisma application call
// (a service/repository) to that call's callee — the frontier source.
func collectServiceVars(body syntax.Node) map[string]string {
	out := map[string]string{}
	walkTS(body, func(n syntax.Node) {
		if n.Type() != "variable_declarator" {
			return
		}
		name := field(n, "name", 0)
		val := field(n, "value", 1)
		if name == nil || val == nil || name.Type() != "identifier" {
			return
		}
		v := unwrapAwait(val)
		if v.Type() == "call_expression" && isServiceCall(v) {
			out[string(name.Text())] = calleeName(v)
		}
	})
	return out
}

// isServiceCall reports whether a call is into application code (a possible
// service/repository) — not Prisma, not a validator, not a framework/utility.
func isServiceCall(call syntax.Node) bool {
	return !isPrismaCall(call) && !isValidationCall(call) && !isFrameworkCall(call) && calleeName(call) != ""
}

// prismaCallInfo returns the model and method of a <client>.<model>.<method>
// Prisma call.
func prismaCallInfo(call syntax.Node) (model, method string) {
	fn := field(call, "function", 0)
	if fn == nil {
		return "", ""
	}
	m := field(fn, "property", 1)
	clientModel := field(fn, "object", 0)
	if m != nil {
		method = string(m.Text())
	}
	if clientModel != nil && clientModel.Type() == "member_expression" {
		if mod := field(clientModel, "property", 1); mod != nil {
			model = string(mod.Text())
		}
	}
	return model, method
}

// hasSelectOrOmit reports whether a Prisma find's argument object carries a
// top-level select or omit clause (field limiting). Only the find options
// object's direct keys are inspected, so a nested field named "select" does not
// falsely count.
func hasSelectOrOmit(call syntax.Node) bool {
	args := field(call, "arguments", 1)
	if args == nil || args.NamedChildCount() == 0 {
		return false
	}
	opts := args.NamedChild(0)
	if opts.Type() != "object" {
		return false
	}
	for i := 0; i < opts.NamedChildCount(); i++ {
		pair := opts.NamedChild(i)
		if pair.Type() != "pair" {
			continue
		}
		if k := field(pair, "key", 0); k != nil {
			if t := string(k.Text()); t == "select" || t == "omit" {
				return true
			}
		}
	}
	return false
}

func unwrapAwait(n syntax.Node) syntax.Node {
	if n != nil && n.Type() == "await_expression" && n.NamedChildCount() == 1 {
		return n.NamedChild(0)
	}
	return n
}

func inPrismaVars(id syntax.Node, m map[string]prismaVar) bool {
	_, ok := m[string(id.Text())]
	return ok
}

func inServiceVars(id syntax.Node, m map[string]string) bool {
	_, ok := m[string(id.Text())]
	return ok
}
