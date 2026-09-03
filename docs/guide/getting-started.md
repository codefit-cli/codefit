# Getting started

> Install codefit, verify the binary, connect it to your agent, and run the first
> audit. Everything on this page is the complete, current procedure — if a step
> here disagrees with the tool, that is a bug, and we treat it as one.

## Install

Two ways to install. **Option A (the release binary) is recommended** — it reports the
correct version and needs no Go toolchain.

### Option A — download the release binary (recommended)

> **Take `v0.3.0-alpha.1` or newer, and read this before clicking "latest".**
> GitHub's `/releases/latest` link **hides pre-releases**, so today it still points at
> **`v0.2.9`** — and `v0.2.9` was built with Go `1.25.12`, a toolchain carrying **four
> standard-library vulnerabilities that codefit's own code calls** (`net/url`,
> `crypto/tls`, `encoding/asn1`, `net/http`). They are closed in `v0.3.0-alpha.1`,
> along with `GO-2026-5024` in `golang.org/x/sys` that shipped inside the Windows
> binary. Pick the newest tag from the full
> [Releases](https://github.com/codefit-cli/codefit/releases) list, not the "latest"
> shortcut.
>
> `alpha` marks the phase, not the stability: it means Phase 3 is underway and `0.3.0`
> has not closed. What ships is gated the same way every release is — build, vet, race
> tests, lint, `govulncheck` and a four-target cross-compile.

Grab the archive for your platform from the
[Releases](https://github.com/codefit-cli/codefit/releases) page. This is the
tagged build, so `codefit version` reports the real version, and it needs no Go.

**Linux / macOS** (pick your target: `linux_amd64`, `linux_arm64`, `darwin_arm64`):

```bash
tar -xzf codefit_<version>_linux_amd64.tar.gz
sudo mv codefit /usr/local/bin/        # a directory on your PATH
chmod +x /usr/local/bin/codefit
```

**Windows** (`windows_amd64`): download `codefit_<version>_windows_amd64.zip`, extract it,
and put `codefit.exe` in a stable folder (e.g. `C:\tools\codefit\`). Optionally add
that folder to your `PATH`.

Verify the download against `checksums.txt` from the release —
`sha256sum -c checksums.txt` (Linux), `shasum -a 256 -c checksums.txt` (macOS), or
`Get-FileHash codefit_<version>_windows_amd64.zip -Algorithm SHA256` (Windows PowerShell).

### Option B — `go install` (needs Go 1.25+)

```bash
go install github.com/codefit-cli/codefit/cmd/codefit@latest
```

Heads up: this reports its version as `0.1.0-dev (commit none, built unknown)` because
`go install` does not embed release metadata — the version is injected by ldflags only
in the release build (GoReleaser) and in `make build`. The binary is **functionally
identical** to the release. For the tagged version, download from
[Releases](https://github.com/codefit-cli/codefit/releases) — the full list, since the
"latest" shortcut hides pre-releases — or build from a checkout with `make build`.

### Verify the install

```bash
codefit version
# example output — your version, commit, and date will differ:
# release binary →  codefit <version> (commit <commit>, built <date>)
# go install     →  codefit v0.1.0-dev (commit none, built unknown)
```

A single static binary, no runtime dependencies (`CGO_ENABLED=0`), cross-compiling
to linux/amd64, linux/arm64, windows/amd64, darwin/arm64. There is no LLM or auth
to configure — codefit manages no models and no credentials.

## Quickstart

```bash
# 1. In your project, generate config + install codefit's skill for your agent(s)
codefit init

# 2. Register codefit as an MCP server for your agent (see "Connect codefit" below)

# 3. From your agent, in plain language:
#    "audit the endpoints in this project for IDOR and broken authorization"
```

`codefit init` writes a config for **any** root — it exits 0 whether or not it
recognizes the language. If it recognizes none, read the report: it names the
markers it looked for, says that no code is scanned in this project, and points
at the one dimension that still reaches it. If your project's schema lives in SQL
migrations rather than a Prisma `schema.prisma`, add the path yourself — codefit
does not detect migration directories yet, and the generated config says so where
the `database:` section would be:

```yaml
database:
  type: "postgresql" # postgresql | mysql | sqlserver
  schema_paths:
    - "db/migrations"
```

`type:` is optional and leaving it out has a consequence worth knowing: codefit
then parses the DDL as PostgreSQL without announcing the choice, so a MySQL or
SQL Server schema is silently mis-parsed and every DB finding afterwards reasons
over a schema you do not have. `sqlite` is the one value codefit refuses outright
rather than guessing at.

The agent loads codefit's skill, calls `codefit-scan-all`, reads the three buckets
(every endpoint named with what it takes to rank it), pulls the full concerns of the
ones worth pursuing with `codefit-scan-endpoint`, reasons the surface with your
project's context, and reports back. It then **records what it concluded** with
`codefit-baseline-record-verdict`, so the reasoning survives the conversation
instead of being redone from zero next time — and so the next scan can hand it
back with `baseline.reasoned_items`. When you decide an item is a false positive
it calls `codefit-baseline-accept` with your reason; after a fix it calls
`codefit-baseline-prune`. You never leave the agent, and codefit never touches
your code.

**Recording is not deciding.** An agent's verdict is a recommendation on the
record: it is stamped `by: agent`, it never acknowledges an item, and two agents
disagreeing keeps both verdicts and raises the disagreement to you rather than
picking a winner. Only a human accepts. What a confirmed `vulnerable` verdict
*does* do is count toward the score — see [The baseline model](../../README.md#the-memory-the-project-keeps).

## Connect codefit

Register codefit as a local (stdio) MCP server. The config blocks need the **absolute
path** to the binary unless it is on the agent process's `PATH`. codefit is stateless —
the project root is passed per call as the `root` tool argument, so the server needs no
`cwd`.

### Finding the binary path

**Linux / macOS:**

```bash
which codefit                        # if it's on your PATH
echo "$(go env GOPATH)/bin/codefit"  # if you used `go install`
```

**Windows (PowerShell):**

```powershell
where.exe codefit                    # if it's on your PATH
Write-Output "$(go env GOPATH)\bin\codefit.exe"   # if you used `go install`
```

With the release binary, the path is wherever you placed it (e.g.
`C:\tools\codefit\codefit.exe`).

> **Windows path gotcha (this is the one that bites):** in JSON (`.mcp.json`,
> `opencode.json`) a Windows path must use **double backslashes**
> (`"C:\\Users\\you\\go\\bin\\codefit.exe"`) or **forward slashes**
> (`"C:/Users/you/go/bin/codefit.exe"`). A single backslash is an invalid JSON escape
> and silently breaks the config. The same applies to TOML basic strings (Codex), or
> use a single-quoted literal string there: `'C:\Users\you\go\bin\codefit.exe'`.

In every Windows example below, **replace `you` with your Windows username** and point
the path at wherever codefit actually lives.

**Claude Code** — `.mcp.json` (project) or `claude mcp add`:

Linux / macOS:

```json
{
  "mcpServers": {
    "codefit": { "type": "stdio", "command": "/usr/local/bin/codefit", "args": ["mcp", "serve"] }
  }
}
```

Windows:

```json
{
  "mcpServers": {
    "codefit": { "type": "stdio", "command": "C:\\Users\\you\\go\\bin\\codefit.exe", "args": ["mcp", "serve"] }
  }
}
```

**OpenCode** — `opencode.json`:

Linux / macOS:

```json
{
  "mcp": {
    "codefit": { "type": "local", "command": ["/usr/local/bin/codefit", "mcp", "serve"], "enabled": true }
  }
}
```

Windows:

```json
{
  "mcp": {
    "codefit": { "type": "local", "command": ["C:\\Users\\you\\go\\bin\\codefit.exe", "mcp", "serve"], "enabled": true }
  }
}
```

**Codex** — `~/.codex/config.toml`:

Linux / macOS:

```toml
[mcp_servers.codefit]
command = "/usr/local/bin/codefit"
args = ["mcp", "serve"]
```

Windows (single-quoted literal string — no backslash escaping needed):

```toml
[mcp_servers.codefit]
command = 'C:\Users\you\go\bin\codefit.exe'
args = ["mcp", "serve"]
```

Then run `codefit init` in the project. It detects Codex by a **project-local
`.codex/`** dir (not the global config); if Codex is only configured globally,
`init` writes the skill to the standard `.agents/skills/codefit/` location and
tells you so.

