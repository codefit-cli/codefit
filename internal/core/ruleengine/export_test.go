package ruleengine

// DecodeJSStringLiteral exposes the unexported decoder to the external test
// package, so its grammar is pinned row by row and not only through the rules
// that use it.
var DecodeJSStringLiteral = decodeJSStringLiteral
