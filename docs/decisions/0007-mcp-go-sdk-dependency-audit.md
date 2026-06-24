# ADR 0007 — Adopting the official MCP Go SDK (dependency audit)

**Status:** Accepted · **Date:** 2026-06-23 · **Phase:** 1 (MCP stdio server)

## Context

codefit is MCP-first: it must speak the Model Context Protocol so agents consume
its tools. The protocol (JSON-RPC framing, handshake, capability negotiation,
tool schemas) is non-trivial and evolving. We adopt the **official Go SDK**
(`github.com/modelcontextprotocol/go-sdk`) rather than hand-rolling JSON-RPC.

Every dependency compiled into codefit becomes part of the trust surface of every
user (SECURITY.md). The SDK is **transport** — it speaks the network/stdio by
design, so the "a parser must not touch the network" rule does not apply; but the
rest of the checklist does, and the `CGO_ENABLED=0` + clean-cross-compile
guarantee is non-negotiable. This ADR records the audit, with evidence.

**Version chosen: v1.6.1** (latest stable). The v1.6.x series implements the
**stable 2025-11-25** MCP spec; v1.7.0+ targets the 2026-07-28 release candidate,
which we do **not** adopt yet. Only the **stdio** transport is used in this slice;
SSE/HTTP is deferred (the SDK abstracts the transport, so it is added later
without a refactor).

## Audit against the SECURITY.md checklist

### 1. Provenance — PASS

Official SDK of the `modelcontextprotocol` organization, **maintained in
collaboration with Google**. Active: ~4.7k stars, 699 commits, 25 releases;
v1.6.1 is recent. Multiple contributors and an organizational backer — not a
single anonymous point of failure. License: **Apache 2.0** (new contributions) /
MIT (existing). Compatible with codefit's Apache 2.0.

### 2. Known vulnerabilities — PASS (with a toolchain note)

`govulncheck -mode binary` over a codefit binary that includes the SDK:

- Built with **go1.25.11**: **`No vulnerabilities found.`**
- The SDK and its transitive modules contribute **0 vulnerabilities that the code
  calls** (govulncheck reports latent ones in required modules, but the call graph
  does not reach them).
- A build with **go1.25.0** flagged **3 standard-library** vulnerabilities
  (`net/url` GO-2025-4010, fixed in go1.25.2). These are a **Go toolchain** issue,
  not the SDK's — they affect any binary built with go < 1.25.2. **Mitigation:**
  pin the build toolchain to **≥ go1.25.2** (the SDK requires go ≥ 1.25.0 anyway).

### 3. Transitive tree — PASS (footprint grows, reported honestly)

codefit went from **3 direct dependencies to 4** (added the SDK). New **external
modules** entering the binary:

| Module | Why |
| --- | --- |
| `github.com/modelcontextprotocol/go-sdk` | the SDK (direct) |
| `github.com/google/jsonschema-go` | tool input/output schemas (Google) |
| `github.com/segmentio/encoding` + `github.com/segmentio/asm` | fast JSON (Go assembly, **not** CGO) |
| `github.com/yosida95/uritemplate/v3` | URI templates (resources) |
| `golang.org/x/oauth2` | OAuth (only exercised if OAuth is configured — not in stdio) |
| `golang.org/x/sys` | syscall helpers |

`github.com/kr/text` is **test-only** (does not compile into the binary).

### 4. Integrity / pinning — PASS

`v1.6.1` pinned with checksums in `go.sum`; `go mod verify` → *all modules
verified*; `GOSUMDB=sum.golang.org` active, no bypass.

### 5. Sensitive-code review — PASS

- **No telemetry / phone-home.** The only hardcoded URLs are OAuth provider
  endpoints (accounts.google.com, Okta), IETF/RFC and JSON-schema spec references,
  and **test fixtures** — no analytics/beacon endpoint. OAuth URLs are reached
  only if OAuth is configured (not by the stdio server).
- **`os/exec`** appears in `mcp/cmd.go` — the **CommandTransport**, the documented
  MCP feature where a *client* launches a server as a subprocess. codefit uses the
  **server** side (`StdioTransport`: read stdin / write stdout); it does not invoke
  the command transport. Legitimate, bounded, not phone-home.
- **`net/http`** is confined to the SSE/HTTP transport (deferred) and OAuth; the
  stdio transport does not use it.
- **`init()`** functions are pure registration (a reverse log-level map; a default
  reconnect delay) — no network, no exec.

### 6. CGO / cross-compile — PASS (the critical gate)

- **`CGO_ENABLED=0 go build` succeeds** with the SDK compiled in. The SDK is pure
  Go plus **Go assembly** (segmentio/asm) — Go assembly is built by the Go
  toolchain, it is not CGO.
- **Cross-compile (CGO=0) verified:** linux/amd64, linux/arm64, windows/amd64,
  darwin/arm64 all build. The single-binary, clean-cross-compile guarantee holds.

### Size — reported

Release-tagged binary (`grammar_subset`): **~3.9 MB → ~8.3 MB** with the SDK
(+~4.4 MB). A real, notable increase (roughly doubles), accepted as the cost of a
correct, maintained protocol implementation versus a hand-rolled one.

## Decision

**Adopt `github.com/modelcontextprotocol/go-sdk@v1.6.1`** as a core dependency for
the MCP transport. It passes the checklist: official provenance, 0 called
vulnerabilities (with the required toolchain), an audited transitive footprint, no
telemetry/unexpected behavior, and — the non-negotiable — **`CGO_ENABLED=0` and a
clean cross-compile**.

Required follow-ups when wiring the server: pin the build toolchain to ≥ go1.25.2
(`toolchain` directive), and update the documented minimum Go version (README /
CONTRIBUTING say 1.24+, the SDK requires 1.25+). SSE/HTTP and the 2026-07-28 spec
are revisited when the case is post-v1.0 and the SDK supports them as stable.
