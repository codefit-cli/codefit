// Package surfaceindex projects a findings.SurfaceItem list into the light
// shape both codefit-scan-all's DBSection and the standalone codefit-scan-db
// serve by default (design D1). It is a pure leaf: it imports only
// internal/core/findings, and nothing outside internal/mcp imports it — the
// adapter is the single consumer, mirroring internal/core/coverage's
// Index()/Resolve() shape for the same reason (one projection, two callers,
// no response-shaping logic drifting into the thin MCP layer).
//
// Every item is indexed; nothing is withheld here (design D4 — there is no
// ranking axis across 18 disjoint db surface categories with no severity
// field, so there is nothing to withhold BY). Full detail is served only on
// request, by id, through Resolve — the same relation codefit-scan-endpoint
// has to codefit-scan-all (ADR 0008/0054), reused rather than reinvented.
package surfaceindex
