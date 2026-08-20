package mcp

import (
	"context"
	"fmt"

	"github.com/google/jsonschema-go/jsonschema"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/codefit-cli/codefit/internal/version"
)

// recognizedAuthzHelpersDescNote is the agent-facing declaration shared by
// codefit-scan-security's and codefit-scan-all's tool descriptions (spec
// "Tool descriptions declare the fields exist"): it must be read BEFORE the
// agent concludes "no authorization" from a run of known_authz_detected:
// false across many endpoints, so it is worded to be found there first, not
// buried after the fact.
const recognizedAuthzHelpersDescNote = " `recognized_authz_helpers` names the project-registered authz " +
	"helper(s) codefit recognized for this language, and `recognized_authz_helpers_note` explains the " +
	"count; a low or zero count there reflects codefit's KNOWLEDGE, not the project's actual guarding — " +
	"read it before concluding known_authz_detected: false means unauthorized."

// NewServer builds the codefit MCP server with its tools registered. Each tool is
// a THIN adapter: it hands the SDK's typed request to the core handler that
// already exists and is tested, and returns the core's result as structured
// output. No audit logic lives here — the MCP layer only connects the protocol
// to the engine (PRD §15). The server is stateless: every tool call is
// independent and carries everything it needs.
func NewServer() *mcpsdk.Server {
	s := mcpsdk.NewServer(&mcpsdk.Implementation{Name: "codefit", Version: version.Version}, nil)

	addTool(s, string(ToolScanSecurity),
		"Run the deterministic security rules and the mapped surface over a project. Input: {root, language, changed_files?}. Pass changed_files to audit only those paths; the response's scope block then declares the narrowing, and score/blocked describe only the audited slice. Returns findings + surface + score + blocked + scope."+
			recognizedAuthzHelpersDescNote,
		HandleScanSecurity)
	addTool(s, string(ToolScanAll),
		"The actionable summary per endpoint. Every endpoint is NAMED, not inlined: `actionable` gives file, line, method, how many concerns and of which categories, which kinds of gap, the highest certainty reached and whether an affirmation is present — enough to rank and choose. The per-concern signals and reason_to_review text is NOT here; fetch any endpoint's full concerns with codefit-scan-endpoint, which re-runs the same analysis. Deterministic findings are the exception: they are facts codefit already concluded, so they come back IN FULL in actionable.endpoints[].deterministic_concerns and never need a second call. resolved_clean and frontier_pending are named the same way. Every response declares a byte `budget`; if the endpoint list did not fit, budget.withheld says how many endpoints are missing and budget.ordering says what ranking the ones you have are a prefix of — never a silent cut. Manages the committed .codefit-baseline: returns a `baseline` delta (new/changed/known/gone) and shows only what is not yet tracked — act on baseline.new and baseline.changed. Input: {root, language, changed_files?}. "+
			"Pass changed_files (project-relative paths) to audit ONLY the files you touched — codefit never asks git, you already know what you changed; omit it (or pass an empty list) for a full audit. "+
			"A partial run declares itself in the `scope` block (mode/audited/auditable_total/unmatched, and findings, score and `blocked` then describe only those files), never proposes pruning a baseline "+
			"item in a file it did not open, and leaves the db dimension NOT MEASURED (by_dimension.db null) unless a configured schema path is in scope. "+
			"When no security provider resolves for `language`, `by_dimension.security` is also null and the `security` section (Measured/note) explains why — the db dimension may still be measured on its own. "+
			"`summary` is PER DIMENSION and every count declares which dimension it counted: `summary.security` (endpoints, deterministic_findings, surface_items, certain_concerns) and `summary.db` "+
			"(schema_sources, deterministic_findings, surface_items), plus `summary.totals` — the cross-dimension roll-up of only the units that mean the same thing in both (deterministic_findings, "+
			"surface_items; never endpoints, a table has no route). A sub-block is `null` when that dimension was NOT MEASURED — null is not zero: zero means codefit looked and found nothing, null "+
			"means nobody looked. Never read `summary.security` as the state of the project: a schema-heavy project can show `summary.security.surface_items: 0` with dozens of open questions under "+
			"`summary.db`. The counts are the RAW population, taken before the baseline filter, so they can exceed what the buckets and `db.surface` list; `summary.note` says so. "+
			"`db.surface` is a light INDEX (id/category/file/line/fingerprint/structural_facts per item, not the full question text) — every item is named, `db.count` is the complete number and `db.withheld` "+
			"is always 0 (there is no ranking axis across db.surface's disjoint categories to withhold by; `db.withheld_note` says so). To read a NAMED item's full detail (the snippet and the actual "+
			"reason_to_review question), call codefit-scan-db with `{root, language, detail: [ids]}` — scan-all's db bucket carries no detail of its own. "+
			"`security.recognized_authz_helpers` and `security.recognized_authz_helpers_note` (present only when `security.measured` is true) are the same declaration as codefit-scan-security's, scoped under `security`:"+
			recognizedAuthzHelpersDescNote,
		HandleScanAll)
	addTool(s, string(ToolScanEndpoint),
		"Re-analyse ONE file on demand and return its endpoints' full concerns (signals, reason_to_review, certainty). Stateless: it re-runs the static analysis, it stores nothing, so what it returns is EXACTLY what codefit-scan-all left out for that endpoint. This is how you read the detail of ANY endpoint scan-all named — actionable, resolved_clean or frontier_pending. Input: {root, language, file}.",
		HandleScanEndpoint)
	addTool(s, string(ToolSurfaceIDOR),
		"Enumerate the IDOR surface (id→resource endpoints) for the agent to reason about ownership checks. "+helperScopeNote+" Input: {files:[{path, content}]}.",
		HandleSurfaceIDOR)
	addTool(s, string(ToolSurfaceAuthz),
		"Enumerate the broken-authorization surface (handlers doing something sensitive), ordered unchecked-first. "+helperScopeNote+" Input: {files:[{path, content}]}.",
		HandleSurfaceAuthz)
	addTool(s, string(ToolSurfaceOverfetch),
		"Enumerate the over-fetching surface (domain-object serializations), ordered by structural certainty. Input: {files:[{path, content}]}.",
		HandleSurfaceOverfetch)
	addTool(s, string(ToolSurfaceNPlus1),
		"Enumerate the N+1 surface (query calls inside a loop), ordered by structural certainty (frontier last, never dropped). Dimension: db. Input: {files:[{path, content}]}.",
		HandleSurfaceNPlus1)
	addTool(s, string(ToolConfirmSurface),
		"Integrate the agent's verdicts on surface items: vulnerable ones become probabilistic findings (confidence < 1.0) anchored to the item. Stateless: codefit recomputes the id to validate.",
		func(in ConfirmSurfaceRequest) (ConfirmSurfaceResponse, error) { return HandleConfirmSurface(in), nil })
	addTool(s, string(ToolBaselineList),
		"List the baseline's tracked items so you can reference them in accept/prune WITHOUT reading the .codefit-baseline file. Returns per item: fingerprint, file, category, state (known|acknowledged), and the reason+date if acknowledged. Filter with {filter:\"known\"} for items still pending (not yet accepted) or {filter:\"acknowledged\"}. Read-only. Input: {root, filter?}.",
		HandleBaselineList)
	addTool(s, string(ToolBaselineAccept),
		"Mark baseline item(s) as acknowledged by a human (false positive or accepted debt): they stop appearing as actionable but stay counted with the reason. SAFETY: call ONLY when the human decided so in the conversation — NEVER on your own. A reason is required. Input: {root, fingerprints, reason}.",
		HandleBaselineAccept)
	addTool(s, string(ToolBaselinePrune),
		"Remove baseline item(s) that no longer exist in the code (resolved by a refactor). Re-scans to confirm they are gone before removing. Input: {root, language, fingerprints?}. Without fingerprints, prunes all confirmed-gone items.",
		HandleBaselinePrune)
	addTool(s, string(ToolBaselineRegisterAuthzHelper),
		"Register a PROJECT-SPECIFIC authorization helper (e.g. requirePermission, getAuthenticatedUserSalonId) so codefit recognizes it on later scans: known_authz_detected becomes true for handlers that call it, clearing the AUTHZ gap WITHOUT the agent re-reasoning. SAFETY: this silences the authz gap on EVERY item calling the helper (far more reach than accepting one) — call ONLY when the HUMAN approved registering it; NEVER on your own. It does NOT clear the IDOR/ownership gap: an IDOR endpoint stays actionable. A reason is required. Input: {root, language, helper_name, reason}.",
		HandleBaselineRegisterAuthzHelper)
	addTool(s, string(ToolBaselineUnregisterAuthzHelper),
		"Remove a previously registered project authz helper (reverses register-authz-helper). The next scan stops recognizing it. Input: {root, language, helper_name}.",
		HandleBaselineUnregisterAuthzHelper)
	addTool(s, string(ToolBaselineRecordVerdict),
		"Persist your reasoning about a surface item (from codefit-surface-* or codefit-scan-*) so it accumulates across audits instead of dying in this conversation. Re-validates each verdict against a FRESH re-analysis before persisting — a verdict whose item no longer exists at that file:line:category is REFUSED, named, and reported (reasons: surface_id_mismatch, no_surface_item_at_anchor, unknown_verdict, analysis_failed), never silently dropped; a batch is per-entry, not all-or-nothing. SAFETY: recording a verdict NEVER silences the item — 'vulnerable' may be recorded freely (it only adds alarm), but 'not_vulnerable' does NOT remove the item from view; only a HUMAN can silence it, via codefit-baseline-accept, same as any other item. Two agents disagreeing on the same item is not an error: both verdicts are kept and the item is flagged in_conflict for a human to resolve. `reasoning` is committed to a shared file — write PROSE about the item, never a quoted source excerpt, a secret, or another project's schema identifier. Input: {root, language, verdicts:[{surface_id, category, file, line, verdict, reasoning, confidence, severity?}]}.",
		HandleBaselineRecordVerdict)
	addTool(s, string(ToolCoverage),
		"Return the coverage INDEX for a language: every entry codefit declares, as an id, a one-line claim, an answer class (deterministic / reasoning / not_covered / delivered_elsewhere) and whether it has more prose to give. The index is always complete — nothing is ever withheld from it, and the response says so and states its own size. Read the delivered_elsewhere entries before concluding a rule id is not covered: they are capabilities the PRD promises under one rule id that codefit ships under another (N+1 is promised as DB-201 and delivered as the nplus1 surface category). To read an entry's full prose, call again with its id: {language, detail: [id, ...]} takes MANY ids in one call, returns each entry's prose byte for byte, and NAMES any id it does not recognize rather than returning an empty success. The response declares the size of what it is actually returning: bytes covers the index PLUS any detail asked for, index_bytes is the index's share, and a detail request large enough to cross the response budget says so and still comes back complete. Input: {language, detail?}.",
		HandleCoverage)
	addTool(s, string(ToolScanDB),
		"Audit the database STRUCTURE from the schema — a Prisma schema.prisma OR a directory of SQL-DDL/Flyway migrations in PostgreSQL, MySQL or SQL Server (T-SQL) dialect, selected by database.type. Affirms tables without a primary key (deterministic, and only where the parser could prove the table's structure complete); everything else is surface to reason about — foreign keys with no covering index, exact duplicate and prefix-redundant indexes, multivalued (array) columns, name-heuristic smells (text FK, missing audit timestamps, sensitive column in the clear, repeating groups), view sensitive-column exposure, and routine-body risks in procedures/functions/triggers. It also classifies the schema's paradigm (OLTP vs OLAP), and on a warehouse adds the star-schema/SCD family plus the columnar-index and partitioning checks. Schema-only: the code x schema index-vs-query cross runs in codefit-scan-all, not here. Reads database.schema_paths from .codefit.yaml; returns measured=false with a note when there is no schema/parser, or when every configured source was found but none could be read (never a false 'clean'). "+
			"`surface` is a light INDEX (id/category/file/line/fingerprint/structural_facts, not the full question text) — always complete: `count` is the total classified and `withheld` is always 0 "+
			"(`withheld_note` says why: there is no ranking axis to withhold by). Read a NAMED item's full detail — the snippet and the actual reason_to_review question — by calling again with "+
			"`{root, language, detail: [ids]}`: many ids in one call, each returned byte-identical to the full item; an id that matches nothing is NAMED in `unrecognized` with a note (codefit is stateless "+
			"and cannot tell whether the id never existed or the schema changed between calls) rather than an empty success. The response declares its own size once detail is involved: `bytes` covers the "+
			"index PLUS any detail asked for, `index_bytes` is the index's own share, and a request large enough to cross the response budget says `over_budget: true` and still comes back complete. "+
			"Input: {root, language, detail?}.",
		HandleScanDB)
	addTool(s, string(ToolCheckCVEs),
		"Check the project's dependencies for known vulnerabilities via OSV.dev (free, no API key). Reads EXACT versions from lockfiles (package-lock.json) and go.mod — it does NOT resolve package.json ranges; a manifest present without its lockfile is reported as a note, never guessed. Input: {root}. Returns the vulnerable dependencies with each vulnerability's CVE/GHSA id, severity, fixed version and references.",
		HandleCheckCVEs)

	return s
}

// Serve runs the codefit MCP server over the stdio transport until ctx is
// cancelled. (HTTP/SSE is deferred; the SDK abstracts the transport, so it is
// added later without a refactor.)
func Serve(ctx context.Context) error {
	return NewServer().Run(ctx, &mcpsdk.StdioTransport{})
}

// addTool registers a codefit core handler (func(In) (Out, error)) as an MCP
// tool, wrapping it in the SDK's typed handler signature. The adapter only
// forwards. The input schema is derived from In and then normalized for the
// strict subset Google/Gemini enforces (see geminiInputSchema).
func addTool[In, Out any](s *mcpsdk.Server, name, desc string, h func(In) (Out, error)) {
	mcpsdk.AddTool(s, &mcpsdk.Tool{Name: name, Description: desc, InputSchema: geminiInputSchema[In](name)},
		func(_ context.Context, _ *mcpsdk.CallToolRequest, in In) (*mcpsdk.CallToolResult, Out, error) {
			out, err := h(in)
			return nil, out, err
		})
}

// geminiInputSchema derives the JSON schema for In (exactly as the SDK would) and
// normalizes it for Google/Gemini, which enforces a stricter schema subset than
// other models: a node's `type` must be a single string, never a union like
// ["null","array"]. Go slices/pointers infer to a nullable union by default,
// which Gemini rejects ("$type == Type.ARRAY" / "items: missing field" on the
// array). collapseNullable drops the "null" so every array is a plain `type:
// array` with its `items`. Pre-setting Tool.InputSchema makes the SDK use this
// instead of re-inferring.
func geminiInputSchema[In any](tool string) *jsonschema.Schema {
	schema, err := jsonschema.For[In](nil)
	if err != nil {
		// Our tool input types are plain structs that always infer cleanly; a
		// failure here is a programming error, surfaced loudly at startup.
		panic(fmt.Errorf("mcp: deriving input schema for tool %q: %w", tool, err))
	}
	collapseNullable(schema)
	return schema
}

// collapseNullable recursively rewrites nullable union types ("null" + one real
// type) into that single type, throughout the schema. Required fields are not
// meaningfully nullable, and codefit's tool inputs are required.
func collapseNullable(s *jsonschema.Schema) {
	if s == nil {
		return
	}
	if len(s.Types) > 0 {
		nonNull := make([]string, 0, len(s.Types))
		for _, t := range s.Types {
			if t != "null" {
				nonNull = append(nonNull, t)
			}
		}
		switch len(nonNull) {
		case 1:
			s.Type, s.Types = nonNull[0], nil
		case 0:
			// degenerate (only "null"): leave as-is.
		default:
			s.Types = nonNull
		}
	}
	for _, sub := range []*jsonschema.Schema{
		s.Items, s.AdditionalItems, s.Contains, s.UnevaluatedItems,
		s.AdditionalProperties, s.UnevaluatedProperties, s.Not,
	} {
		collapseNullable(sub)
	}
	for _, list := range [][]*jsonschema.Schema{s.PrefixItems, s.ItemsArray, s.AllOf, s.AnyOf, s.OneOf} {
		for _, sub := range list {
			collapseNullable(sub)
		}
	}
	for _, m := range []map[string]*jsonschema.Schema{s.Properties, s.PatternProperties, s.Defs, s.Definitions} {
		for _, sub := range m {
			collapseNullable(sub)
		}
	}
}
