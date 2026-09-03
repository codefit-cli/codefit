# The tools, scoping, and the cache

> The complete MCP tool reference: what each tool does, how to narrow a scan to
> the files you changed, and the opt-in finding cache.

## The tools

codefit exposes its capabilities as MCP tools in three roles:

**The engine** — run the analysis and read the result.

| Tool | What it does |
| --- | --- |
| `codefit-scan-all` | The per-endpoint synthesis: three buckets (`actionable` / `resolved_clean` / `frontier_pending`) + the baseline delta (including `reasoned_by_agent` / `reasoned_items` / `in_conflict` — what agents already concluded, so nothing is reasoned twice), plus a parallel `db` section (database-structure findings/surface) and a per-dimension `score`. Every bucket **names** its endpoints with what it takes to rank them; the concern text is fetched on demand (deterministic findings come back in full). Carries a declared byte `budget` and says how many endpoints it withheld, if any. The main entry point. Optional `changed_files` narrows the audit — see [Scoping a scan](#scoping-a-scan-to-the-files-you-changed). |
| `codefit-scan-endpoint` | Full detail of one file on demand — the concerns `scan-all` named but did not spell out, for **any** bucket. |
| `codefit-scan-security` | The deterministic findings + mapped surface over a project (the flat result). Also takes the optional `changed_files`. |
| `codefit-scan-db` | The database-structure audit over the configured schema (`database.schema_paths` — a Prisma `schema.prisma` or SQL-DDL migrations in PostgreSQL, MySQL, or SQL Server dialect per `database.type`): affirmations (e.g. a table with no primary key) + surface (un-indexed FKs, duplicate indexes, …). Returns `measured: false` with a note when there is no schema or parser — and equally when every configured schema source was found but none of them could be read, so an unreadable schema is never reported as a clean one. |
| `codefit-surface-idor` / `-authz` / `-overfetch` | Enumerate one surface category for the agent to reason. |
| `codefit-surface-nplus1` | Enumerate the N+1 surface: query call sites sitting inside a loop, ordered by structural certainty (the cross-function frontier last, never dropped). |
| `codefit-check-cves` | Check the project's dependencies against OSV.dev (free, no API key). Reads exact versions from lockfiles / `go.mod`; reports the vulnerable deps with id, severity and fixed version. |

**Baseline** — the project's audit memory (see below).

| Tool | What it does |
| --- | --- |
| `codefit-baseline-list` | List tracked items (fingerprint, file, category, state, and any agent verdicts with their reasoning) — `filter: known` for what's still pending. |
| `codefit-baseline-record-verdict` | Persist what an agent concluded about a surface item, so the answer survives the conversation. Re-validates each verdict against a fresh re-analysis first: one whose item is no longer there is **refused and named**, never silently dropped. Recording never *accepts* an item — only a human does, with `-accept`. Two agents disagreeing keeps **both** verdicts and flags the item in conflict. |
| `codefit-baseline-accept` | Record a human's decision to accept an item (false positive / accepted debt) with a reason. |
| `codefit-baseline-prune` | Drop items a refactor resolved (re-scans to confirm they're gone first). The re-scan is **always full** — it accepts no `changed_files`. |
| `codefit-baseline-register-authz-helper` | Register a project-specific authorization helper so later scans recognize it (`known_authz_detected` becomes true where it is called). Clears the **authz** gap only — an IDOR/ownership item stays actionable. Requires a human decision and a reason. |
| `codefit-baseline-unregister-authz-helper` | Reverse the above: the next scan stops recognizing that helper. |

**Auxiliary** — feed results back and introspect.

| Tool | What it does |
| --- | --- |
| `codefit-confirm-surface` | Integrate the agent's verdicts for one call: a confirmed item becomes a probabilistic finding anchored to it. **Stateless** — it persists nothing and takes no `root`. To make a verdict outlive the conversation use `codefit-baseline-record-verdict` instead. |
| `codefit-coverage` | The coverage manifest for a language — what codefit audits vs. reasons over vs. does not cover. |

## Scoping a scan to the files you changed

`codefit-scan-all` and `codefit-scan-security` take an optional `changed_files`: a list of
project-relative paths. Only those files are analysed. **codefit never asks git** which
files changed — it has no power over your git, and the agent calling it already knows what
it touched. Omitting `changed_files` (or passing an empty list) is a **full** audit; an
empty list is never read as "audit nothing".

A partial audit that looks like a full one would be a lying auditor, so the narrowing is
declared in the response. Every scan — full or partial — carries a `scope` block:

```json
"scope": {
  "mode": "partial",
  "requested": 3,
  "audited": 2,
  "auditable_total": 412,
  "unmatched": ["src/deleted.ts"],
  "note": "Partial audit: 2 of 412 auditable files were in scope. …"
}
```

What a partial scan does **not** claim:

- **`blocked: false` means *no critical in the audited slice*, not *no critical*** — and the
  same goes for `score` and `by_dimension`. `blocked: true` needs no caveat. The blocking
  rule itself is unchanged and stays non-configurable.
- **`unmatched` is not "clean".** A requested path the audit never reached — deleted, not an
  auditable extension, inside a skipped directory — is listed there. It is the difference
  between *audited and clean* and *never looked*.
- **The database dimension reports `null` (not measured), never `100`,** when no configured
  `database.schema_paths` entry is in scope. When it does run it reads all of them.
- **A partial scan cannot prune the baseline.** An item is a `gone` candidate only if its
  category ran **and** its file was in scope, so a narrowed pass never proposes dropping the
  memory of a file it did not open — and `codefit-baseline-prune` accepts no scope at all.
  Scanning may be partial; forgetting may not.

This decides **which files get audited**, not which results get reused — that is the
[finding cache](#the-finding-cache-opt-in) below, and the two are independent.

## The finding cache (opt-in)

codefit can remember what it computed for a file and skip re-analysing it when nothing that
matters has changed. **It is off unless you ask for it** — a project with no `cache:` section
in `.codefit.yaml` has no cache, and `codefit init` does not write one. To turn it on:

```yaml
cache:
  enabled: true
  # dir: .codefit/cache   # the default; a relative dir resolves against the project root
```

`.codefit/cache` is gitignored and skipped by the scan — the cache is local scratch, never
shared knowledge like `.codefit.yaml` or the baseline.

**It never changes what codefit reports.** A warm scan and a cold scan are *byte-identical*,
not merely equivalent — that is the property the implementation is tested against, and a
cache that could change the output would be a blind spot, not an optimization. Every file is
still opened, still counted and still reported; the cache decides only what is **recomputed**.

Four things worth knowing:

- **It exists so the full scan stays affordable, not for speed as such.** The full scan is
  the honest one: it is the only scan that can prune the baseline and the only one whose
  `blocked: false` means what it appears to mean. If the full scan were expensive and the
  narrowed one cheap, everyone would narrow forever.
- **A new codefit binary invalidates everything, on purpose.** An entry is keyed on the
  analyzer's own bytes as well as the file's path and content, so a codefit that ships new
  rules can never serve you a verdict computed under the old ones. The cost of that
  guarantee is that **every new binary orphans the previous generation of entries** — which
  is why the store cleans up after itself.
- **The store bounds itself, and only touches what it wrote.** Entries are grouped by the
  binary that produced them, and opening the cache keeps **three groups** — the current one
  always, plus the two most recently *written* others — drops entries in the current group
  **not written in 30 days**, and clears the layout its predecessor left behind. A hit does
  not rewrite an entry, so a file you have not edited in a month is re-analysed once and
  re-cached. That is a **retention** bound, not a size limit: codefit does not measure the
  directory or evict by size. The cleanup only ever recognises the two filename shapes
  codefit writes itself, so **anything else you keep in `.codefit/cache` is never touched, at
  any age**, and it is best effort — it can never be the reason a scan fails. One residue
  follows from that rule and is worth knowing rather than finding: a `.entry-*.tmp` left by a
  crashed write is not entry-shaped either, so a stray one inside the *current* group stays
  until that group is superseded. `rm -rf .codefit/cache` is still always safe — it costs
  only time, which is exactly what makes it different from the baseline — you just should not
  need it routinely.
- **A cache failure is never an audit failure.** A missing, unreadable or corrupt entry just
  means the file gets analysed; a failed write is logged and the scan reports normally.
- **An entry has to prove it is the answer to the key that was asked for.** Each entry
  records its own key, and a read that does not match it is a miss. This matters because
  `.codefit/cache` is an ordinary directory in your project: valid JSON that simply is not a
  codefit entry — a stray `{}`, an editor or sync artifact, a half-restored backup, another
  tool's file at an entry's path — would otherwise parse into an *empty* entry and be served
  as "analysed, nothing found". Entries written before codefit started stamping the key are
  re-analysed once and rewritten.
- **The cache barely warms under concurrent tool calls on Windows.** Windows will not let the
  atomic write replace an entry file another reader is holding open, so with two codefit
  tools running over one project at once the write fails and logs a warning per file. The
  direction is safe — a failed write is just a miss and the audit is unaffected — but the
  cache does not fill up the way it does elsewhere. Not yet addressed.

Not cached: the database dimension. Its inputs are the configured `database.schema_paths`
rather than a repository walk, and a schema reconstructed from an ordered set of migrations
does not obviously invalidate per file.

