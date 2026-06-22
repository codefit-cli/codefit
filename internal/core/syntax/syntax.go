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
	// StartByte and EndByte are the node's [start,end) byte range in the source.
	// Byte ranges power pattern-inside via containment (A is inside B when
	// B.StartByte <= A.StartByte && A.EndByte <= B.EndByte) and give findings a
	// precise location. They were chosen over a Parent() pointer because both
	// parsers expose them natively (gotreesitter directly; go/ast via Pos/End +
	// FileSet) whereas go/ast has no native parent — so this extension converges
	// cleanly across both parsers (ADR 0003).
	StartByte() int
	EndByte() int
	// HasError reports whether this node (or its subtree, for the root) contains
	// a syntax error — the runtime-level check for parse failures.
	HasError() bool
}
