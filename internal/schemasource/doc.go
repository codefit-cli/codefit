// Package schemasource maps a schema INPUT to the concrete parser that can read
// it. It is the single production site of that mapping (spec R11): the site may
// move, it must not multiply, because a second mapping site is how one caller
// starts reading a schema under a different parser than the one the audit uses.
//
// It lives outside internal/core deliberately. The mapping names concrete
// providers (sqlddl, typescript), and "el núcleo NUNCA importa un provider
// concreto" — so a package that must name them cannot be a core package. It
// also lives outside internal/mcp, where it used to live, because the mapping is
// not a transport concern: `codefit init` needs the same binding the MCP scan
// uses, and a transport adapter handing a parser to a scaffolder was the seam
// showing.
//
// The mapping resolves by the shape of the INPUT, never by the app language: a
// schema is orthogonal to the backend that talks to it (ADR 0018).
package schemasource
