package syntax

// Node is one node of a parsed file, exposed to the core in a parser-agnostic
// way. A language provider returns the root Node from its parse; the core walks
// the tree only through this interface.
//
// Minimal by design (ADR 0003): only what tree navigation needs now. No
// Parent() until the ruleengine's pattern-inside operator needs it (Prompt 1.2),
// so the interface is not shaped for a caller that does not yet exist.
type Node interface {
	// Type is the node's grammar type, e.g. "function_declaration",
	// "jsx_element", "import_statement".
	Type() string
	// Text is the source slice this node spans.
	Text() []byte
	// NamedChildCount / NamedChild iterate the named children (anonymous tokens
	// like punctuation are skipped).
	NamedChildCount() int
	NamedChild(i int) Node
	// ChildByField returns the child under a grammar field name (e.g. "name",
	// "parameters", "body"), or nil when absent.
	ChildByField(name string) Node
	// StartLine is the 1-based line where the node begins, for findings.
	StartLine() int
	// HasError reports whether this node (or its subtree, for the root) contains
	// a syntax error — the runtime-level check for parse failures.
	HasError() bool
}
