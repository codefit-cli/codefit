// Package syntax is codefit's parser-agnostic AST boundary. It defines [Node],
// the minimal tree interface the core navigates, so the engine (ruleengine,
// sensors) can walk a parsed file without importing any concrete parser
// (go/ast, gotreesitter, ...). Each language provider adapts its parser's nodes
// to this interface.
//
// The interface is deliberately minimal: it carries only what parsing and tree
// navigation need today. Operators that need more (e.g. pattern-inside wanting a
// parent pointer) extend it when they are implemented, with a real caller —
// not speculatively (see ADR 0003).
package syntax
