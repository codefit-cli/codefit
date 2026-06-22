# ADR 0002 — TypeScript/TSX parser: gotreesitter (pure Go, no CGO)

**Status:** Accepted · **Date:** 2026-06-22 · **Phase:** 1 (TypeScript provider)

## Context

The Go provider parses with the stdlib `go/ast` (ADR 0001). The stdlib does not
parse TypeScript/TSX, so the TypeScript provider needs a parser — under codefit's
non-negotiable rule: `CGO_ENABLED=0 go build ./...` must work and cross-compile
to `windows/amd64`. Most tree-sitter Go bindings use CGO, which breaks this.

A throwaway spike (`/spike`, since deleted) parsed real `.ts` and `.tsx` with
[`github.com/odvcencio/gotreesitter`](https://github.com/odvcencio/gotreesitter)
and confirmed it builds and runs with `CGO_ENABLED=0`.

Because codefit is itself a security tool, **every dependency in its binary is
part of the user's trust surface.** Before adopting gotreesitter we ran the
[dependency adoption checklist](../../SECURITY.md#dependency-policy). This ADR
records that audit and its evidence.

## Decision

Adopt **`github.com/odvcencio/gotreesitter@v0.20.2`** as the TypeScript/TSX
parser. It is a pure-Go reimplementation of the tree-sitter runtime (116 external
scanners hand-ported to Go), so it preserves the single, CGO-free binary.

Backup if a blocker emerges later (see Caveats): the WASM-via-wazero approach
(`malivvan/tree-sitter`), where the official C scanner is compiled to WASM — at
the cost of maturity (pre-release) and wazero startup overhead.

## Audit evidence

**Provenance.** Maintainer Oscar Villavicencio (`odvcencio`): real identity
(Los Angeles, m31labs.dev, LinkedIn), GitHub account predating 2014, Arctic Code
Vault contributor, 32 repos. Primary single maintainer (1328 commits) but with
10+ contributors (rsnodgrass, vdergachev, alrs, …) — not a one-author repo, and
not an anonymous or recent account. Single-maintainer risk is mitigated by
checksum pinning (below) and the code being pure Go and auditable.

**Known vulnerabilities.** Clean.
- `govulncheck` over a build that includes gotreesitter: *"No vulnerabilities
  found. Your code is affected by 0 vulnerabilities."* (The 2+36 it lists in
  imported/required packages are stdlib advisories the code does not call, an
  artifact of the local Go patch level — not gotreesitter.)
- OSV.dev direct query for `github.com/odvcencio/gotreesitter@0.20.2` → `{}`
  (zero). Same for the transitive `golang.org/x/sync@0.11.0` → `{}`.

**Transitive tree.** Minimal. What enters codefit's binary (`go list -deps`) is
only `gotreesitter` + `gotreesitter/grammars` — **no new external packages** in
the binary beyond the runtime itself. Declared module requires: `golang.org/x/sync`
(official; not reached on our path), `gopkg.in/yaml.v3` (already a codefit dep),
and `kr/pretty` + `gopkg.in/check.v1` (test-only, never compiled into the binary).

**Integrity.** Pinned with checksum: both hashes present in `go.sum`, `go mod
verify` → *"all modules verified"*, and `GOSUMDB=sum.golang.org` is active with no
bypass — so the Go checksum database covers the module and protects against the
published version being swapped after this audit.

**Sensitive code review (it is pure Go, hence auditable).** A legitimate parser
reads bytes, builds a tree, returns nodes — it does not touch the network or run
commands. gotreesitter passes:
- **Network:** `net`/`net/http` appear only under `cmd/` (the repo's developer
  tools), never in the runtime. Confirmed: `net`/`net/http`/`os/exec` are **not**
  in `go list -deps` of the binary.
- **Command execution:** `os/exec` only under `cmd/`. The only direct `syscall`
  use in the runtime is an mmap path guarded by `//go:build grammar_blobs_external`
  — excluded from the default embedded build. The `syscall` that does enter the
  binary is the ordinary stdlib one (via `os`/`runtime`), ubiquitous in every Go
  program.
- **unsafe:** 15 runtime files (parser, GLR GSS/forest, arena allocator, string
  interning) — expected for a high-performance parsing runtime. Spot-checked
  `intern.go`: `uintptr(unsafe.Pointer(...))` for zero-copy string interning,
  benign.
- **init():** 541 functions, all pure registration (`Register(LangEntry{...})`,
  `registerSubsetEmbeddedBlob(...)`, scanner attachment maps). Nothing beyond
  grammar/scanner registration.

**License.** MIT (compatible with codefit's Apache 2.0).

**Maintenance.** v0.20.2 released 2026-06-06; 517 stars; 26 releases.

## Verdict

**Safe to adopt.** No supply-chain red flag found: clean vuln scans, minimal
transitive footprint, checksum-pinned, and the runtime touches neither the
network nor command execution. TS and TSX parse real input with `HasError()==false`
and a navigable AST.

## Caveats (provider-phase work, not security blockers)

1. **Binary size.** Importing all ~205 grammars yields a ~30 MB binary (vs 3.7 MB
   today). The Go checksum/runtime cost is fine; the size is not. It is reducible:
   `-tags grammar_blobs_external` → ~11 MB (but externalizes blobs, breaking the
   single binary). The right option for the single-binary rule is a
   **`grammar_subset`** build embedding only `typescript`/`tsx` — to configure when
   building the provider.
2. **Safety caps.** The pure-Go runtime documents iteration/stack/node caps that
   can yield `HasError()` on very large or deeply nested files. Detectable at
   runtime. The provider **must** test against large real TS files to confirm the
   caps don't bite typical project code; if they do, fall back to the WASM approach.

## Caveat resolution (Prompt 1.1)

Both caveats were resolved when the parsing layer was built:

1. **Binary size — accepted trade-off at ~5.4 MB.** The `grammar_subset`
   build tags (`grammar_subset grammar_subset_typescript grammar_subset_tsx`,
   wired into both the Makefile and goreleaser) bring a binary that imports the
   provider from **30 MB → 5.4 MB** (stripped). It does **not** reach the
   ~2.5 MB of today's binary, and that is understood: the `size`/`nm` breakdown
   shows the residual is almost entirely **`text` (code), not `data`** — i.e. the
   subset removed the other ~203 grammar blobs, and what remains is the
   gotreesitter **runtime itself** (the GLR parser plus 116 external scanners,
   all registered in a global `init()` map so dead-code elimination cannot drop
   them). ~5 MB is the irreducible cost of a full TypeScript parser in pure Go
   without forking gotreesitter. **Accepted**: the single, CGO-free,
   cross-compiling binary is preserved; the size is the price of TS support.

2. **Safety caps — passed on real code.** The exit-criterion stress tests parse
   real project files with `HasError()==false`: microsoft/vscode `strings.ts`
   (1411 lines), excalidraw `Actions.tsx` (1344 lines), and an ~80-level
   deeply-nested file (stack cap). The caps do not bite typical project code, so
   the WASM fallback is not needed.

## Consequences

- The TypeScript provider can use gotreesitter without CGO; the single, clean
  cross-compile guarantee holds, at ~5.4 MB once the provider is wired in.
- This audit is the first application of the project's
  [dependency policy](../../SECURITY.md#dependency-policy); future core
  dependencies follow the same checklist and get the same ADR treatment.
