// Package cli wires the codefit command tree with cobra: the root command and
// its global flags, plus its subcommands. codefit is MCP-first — there is no
// CLI audit mode, so this tree carries only PLUMBING: init, version, status,
// update, and mcp serve. Auditing is reached exclusively through the MCP tools.
//
// Status: BUILT except update. init, version, status and mcp serve run for
// real; update is a Fase 4 item and is still the notImplemented stub, which
// says so on stdout rather than pretending to succeed.
package cli
