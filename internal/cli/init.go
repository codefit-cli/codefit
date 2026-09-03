package cli

import (
	"bufio"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/codefit-cli/codefit/internal/scaffold"
	"github.com/spf13/cobra"
)

func newInitCmd() *cobra.Command {
	var (
		nonInteractive bool
		force          bool
	)
	cmd := &cobra.Command{
		Use:   "init [path]",
		Short: "Analyze the project and generate an initial .codefit.yaml",
		Long: "Analyze the project and generate codefit's setup: a committed .codefit.yaml,\n" +
			"codefit's own thin skill, and that skill placed where each detected agent\n" +
			"(Claude Code, OpenCode, Codex) discovers it. Nothing is written silently.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			root := "."
			if len(args) == 1 {
				root = args[0]
			}
			return runInitCmd(cmd, root, force, nonInteractive)
		},
	}
	f := cmd.Flags()
	f.BoolVar(&nonInteractive, "non-interactive", false, "skip the wizard and write sensible defaults")
	f.BoolVar(&force, "force", false, "overwrite an existing .codefit.yaml")
	return cmd
}

// runInitCmd resolves the overwrite decision (the dev always decides), runs the
// scaffold, and prints the full report. The .codefit.yaml is shared project
// config, so an existing one is never replaced without explicit consent.
func runInitCmd(cmd *cobra.Command, root string, force, nonInteractive bool) error {
	out := cmd.OutOrStdout()

	overwrite := force
	if !force && scaffold.ConfigExists(root) {
		if nonInteractive {
			overwrite = false
		} else {
			ok, err := confirmOverwrite(out, cmd.InOrStdin())
			if err != nil {
				return err
			}
			overwrite = ok
		}
	}

	res, err := scaffold.Generate(scaffold.Options{Root: root, OverwriteConfig: overwrite})
	if err != nil {
		return fmt.Errorf("codefit init: %w", err)
	}
	fmt.Fprint(out, formatReport(res))
	return nil
}

// confirmOverwrite asks the dev whether to regenerate an existing config and
// returns their decision, defaulting to NO on an empty answer.
func confirmOverwrite(out io.Writer, in io.Reader) (bool, error) {
	fmt.Fprint(out, ".codefit.yaml already exists. Regenerate it? [y/N]: ")
	line, err := bufio.NewReader(in).ReadString('\n')
	if err != nil && err != io.EOF {
		return false, fmt.Errorf("reading confirmation: %w", err)
	}
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes":
		return true, nil
	default:
		return false, nil
	}
}

// formatReport renders the human-facing account of what init did. It declares
// every detected fact and every file written — codefit never acts silently.
func formatReport(res scaffold.Result) string {
	var b strings.Builder
	info := res.Info

	fmt.Fprintf(&b, "codefit init — %s\n\n", info.Name)

	fmt.Fprintln(&b, "Detected:")
	fmt.Fprintf(&b, "  language    %s\n", info.Language)
	fmt.Fprintf(&b, "  capability  %s\n", scaffold.CapabilityStatement(info.Language))
	if info.Framework != "" {
		fmt.Fprintf(&b, "  framework   %s\n", info.Framework)
	}
	if info.ORM != "" {
		db := info.ORM
		if len(info.SchemaPaths) > 0 && info.DBType != "" {
			db = fmt.Sprintf("%s  (%s → %s)", info.ORM, reportPath(info.SchemaPaths[0]), info.DBType)
		}
		fmt.Fprintf(&b, "  orm         %s\n", db)
	}
	if c, ok := provenSchema(info); ok {
		fmt.Fprintf(&b, "  schema      %s → %d table(s) reconstructed\n", reportPath(c.Path), c.Tables)
	}
	if info.RouteHandlers > 0 {
		fmt.Fprintf(&b, "  endpoints   %d route handler(s)\n", info.RouteHandlers)
	}

	fmt.Fprintln(&b, "\nConfig:")
	switch res.ConfigAction {
	case scaffold.ConfigCreated:
		fmt.Fprintf(&b, "  %s  (project config — commit this)\n", reportPath(res.ConfigPath))
	case scaffold.ConfigOverwritten:
		fmt.Fprintf(&b, "  %s  (regenerated — commit this)\n", reportPath(res.ConfigPath))
	case scaffold.ConfigSkipped:
		fmt.Fprintf(&b, "  %s already exists; left unchanged (use --force to regenerate)\n", reportPath(res.ConfigPath))
	}

	if res.UsedFallback {
		fmt.Fprintln(&b, "\nNo known agents detected. Skill written to the standard location:")
		for _, s := range res.Skills {
			fmt.Fprintf(&b, "  %s\n", reportPath(s.Path))
		}
		fmt.Fprintln(&b, "Install it manually for your agent, or with `npx skills`.")
	} else {
		fmt.Fprintf(&b, "\nAgents detected: %s\n", strings.Join(agentNames(res.Skills), ", "))
		fmt.Fprintln(&b, "Skill written to:")
		for _, s := range res.Skills {
			fmt.Fprintf(&b, "  %s\n", reportPath(s.Path))
		}
	}

	writeSchemaSection(&b, res)

	fmt.Fprintln(&b, "\nNext: connect codefit over MCP (see docs/guide/getting-started.md → \"Connect codefit\").")
	return b.String()
}

// provenSchema returns the candidate that was written into schema_paths, when
// this run wrote one it MEASURED.
//
// It matches on the candidate list rather than reading SchemaPaths alone,
// because a Prisma project also has a schema_paths and codefit never counted
// tables for it. Printing "→ 0 table(s) reconstructed" there would state a
// measurement that was never taken.
func provenSchema(info scaffold.ProjectInfo) (scaffold.SchemaCandidate, bool) {
	for _, p := range info.SchemaPaths {
		for _, c := range info.SchemaCandidates {
			if c.Path == filepath.ToSlash(p) && c.Tables > 0 {
				return c, true
			}
		}
	}
	return scaffold.SchemaCandidate{}, false
}

// writeSchemaSection reports what init did about the database schema: what it
// proved, what it found and left out, and what it dropped.
//
// It is one function rather than two because the developer reads one report:
// splitting "we wrote this" from "we did not write that" across two sections
// hides the fact that both can be true of one run.
func writeSchemaSection(b *strings.Builder, res scaffold.Result) {
	info := res.Info
	writeDroppedSchemaPaths(b, res.DroppedSchemaPaths)
	if c, ok := provenSchema(info); ok {
		writeSchemaProof(b, info, c)
		return
	}
	writeSchemaGap(b, info)
}

// writeDroppedSchemaPaths announces R9's demotion. codefit informs the
// consequence; the developer decides what to do about it.
func writeDroppedSchemaPaths(b *strings.Builder, dropped []scaffold.SchemaCandidate) {
	if len(dropped) == 0 {
		return
	}
	fmt.Fprintln(b, "\nDropped — database schema:")
	for _, d := range dropped {
		fmt.Fprintf(b, "  %s\n", reportPath(d.Path))
		fmt.Fprintf(b, "    %s\n", d.Reason)
	}
	fmt.Fprintln(b, "  This run re-derived database.schema_paths from disk, and the path above no")
	fmt.Fprintln(b, "  longer proves, so it was not written again. Nothing else removed it. If it")
	fmt.Fprintln(b, "  is still your schema, put it back by hand — codefit will read whatever the")
	fmt.Fprintln(b, "  config names, whether or not it could prove it itself.")
}

// writeSchemaProof states what codefit MEASURED, and — just as important — what
// it did not.
//
// The dialect sentence is not decoration. codefit does not sniff the SQL
// dialect, so a proof run with no `database.type` configured ran under the
// default binding, and "proved" would otherwise read as a claim about the
// dialect too. The residual is declared where the developer can act on it.
func writeSchemaProof(b *strings.Builder, info scaffold.ProjectInfo, proven scaffold.SchemaCandidate) {
	fmt.Fprintln(b, "\nConfigured — database schema:")
	fmt.Fprintf(b, "  %s → %d table(s) reconstructed, written as database.schema_paths.\n",
		reportPath(proven.Path), proven.Tables)
	fmt.Fprintln(b, "  codefit wrote it because it PROVED it: it read that directory exactly as an")
	fmt.Fprintln(b, "  audit would and reconstructed a schema. It writes nothing it cannot prove.")

	if info.DBType == "" {
		fmt.Fprintln(b, "  It proved the path by parsing it as PostgreSQL, because `database.type` is")
		fmt.Fprintln(b, "  unset and codefit does not sniff the SQL dialect. The proof says the DDL")
		fmt.Fprintln(b, "  reconstructs — it does NOT say the dialect is right. If this schema is MySQL")
		fmt.Fprintln(b, "  or SQL Server, set `database.type`; the commented line in .codefit.yaml sits")
		fmt.Fprintln(b, "  directly above the key.")
	}

	var others []scaffold.SchemaCandidate
	for _, c := range info.SchemaCandidates {
		if c.Path != proven.Path {
			others = append(others, c)
		}
	}
	if len(others) > 0 {
		fmt.Fprintln(b, "  codefit also found SQL it did not write, and says so rather than letting the")
		fmt.Fprintln(b, "  omission look like an absence:")
		for _, c := range others {
			fmt.Fprintf(b, "    %s — %s\n", reportPath(c.Path), c.Reason)
		}
	}
}

// writeSchemaGap declares, in the report the developer actually reads, that the
// config just written audits no database schema.
//
// The condition is len(SchemaPaths) == 0 — the field the audit actually reads.
// schema_paths is what the DB sensor consumes; `orm` is consumed by nothing, so
// keying this declaration on an ORM made it answer a question nobody asked. It
// went silent for a drizzle or typeorm project, which has the full gap AND a
// `database:` block that configured nothing, and it would have spoken for a
// Flyway project the moment detection fills schema_paths with no ORM to go with
// it. Wrong in both directions, out of one predicate.
//
// It is not keyed on "the language was undetected" either. A TypeScript project
// without a schema has exactly the same gap as a Java one, and declaring it only
// in the case that happens to be new would be the over-promise this project
// exists to prevent.
//
// ORM is still read here — for WORDING, never to gate. A report printing "orm
// drizzle" under Detected and "Not configured — database schema" below it states
// two true facts that read as a contradiction; the clarifier explains how both
// hold at once rather than leaving the developer to reconcile them.
//
// The consequence is spelled out rather than implied: with no schema configured
// AND no security provider, the next codefit-scan-all has nothing to measure
// and says so.
//
// What this section says has CHANGED, and the change is the point. It used to
// declare a capability gap — "SQL migration directories are NOT detected" — a
// sentence true of every project at once and therefore silent about this one.
// codefit now searches, so the report states its SEARCH RESULT: either the
// directories it found and why none could be proven, or that it looked and found
// none, with the depth it walked. A report that says nothing about having
// searched cannot be told apart from a codefit that never searched.
func writeSchemaGap(b *strings.Builder, info scaffold.ProjectInfo) {
	if len(info.SchemaPaths) > 0 {
		return
	}
	fmt.Fprintln(b, "\nNot configured — database schema:")
	if info.ORM != "" {
		fmt.Fprintf(b, "  codefit did detect %s, and that fact stands — but an ORM name is not a schema.\n", info.ORM)
		fmt.Fprintln(b, "  No sensor reads it, so it was not written on its own: a `database:` block")
		fmt.Fprintln(b, "  holding only an ORM configures nothing while looking configured. What the DB")
		fmt.Fprintln(b, "  dimension audits is whatever database.schema_paths names, and no schema source")
		fmt.Fprintln(b, "  resolved here.")
	}
	if len(info.SchemaCandidates) > 0 {
		fmt.Fprintln(b, "  codefit DID find SQL migration directories here, and could not prove any")
		fmt.Fprintln(b, "  single one into a schema it may write for you. What it found, and why each")
		fmt.Fprintln(b, "  was left out:")
		for _, c := range info.SchemaCandidates {
			fmt.Fprintf(b, "    %s — %s", reportPath(c.Path), c.Reason)
			if c.Tables > 0 {
				fmt.Fprintf(b, " (%d table(s) reconstructed)", c.Tables)
			}
			fmt.Fprintln(b)
		}
		fmt.Fprintln(b, "  Nothing was written rather than a guess: database.schema_paths entries merge")
		fmt.Fprintln(b, "  into ONE reconstructed model, so a wrong entry does not add noise, it poisons")
		fmt.Fprintln(b, "  the model — and every DB finding after it is stated at full confidence about")
		fmt.Fprintln(b, "  a schema you do not have. The choice is yours: write the one you mean, and")
		fmt.Fprintln(b, "  the DB dimension runs on the next scan.")
	} else {
		fmt.Fprintln(b, "  codefit LOOKED for one and found none. It searched this project for SQL")
		fmt.Fprintf(b, "  migration directories, walking down to %d directory levels below the root and\n", scaffold.DiscoveryDepth)
		fmt.Fprintln(b, "  skipping vendored trees, build output and test fixtures, for a directory whose .sql")
		fmt.Fprintln(b, "  filenames prove their apply order — and it detects a Prisma schema.prisma")
		fmt.Fprintln(b, "  directly. That is codefit's search RESULT, not a claim about your project: a")
		fmt.Fprintln(b, "  schema deeper than the bound is outside it, and a monorepo should run")
		fmt.Fprintln(b, "  `codefit init` per sub-project root.")
		fmt.Fprintln(b, "  If this project has a schema, add it by hand — the commented block in")
		fmt.Fprintln(b, "  .codefit.yaml shows how — and the DB dimension runs on the next scan.")
	}
	if !info.Detected() {
		fmt.Fprintln(b, "  Until then codefit-scan-all has nothing to measure on this project and will")
		fmt.Fprintln(b, "  say so rather than report it clean: no security provider resolves either.")
	}
}

// reportPath renders a path the way codefit spells the paths it emits: with
// forward slashes, on every OS.
//
// The scaffold works in host paths — findPrismaSchema returns
// filepath.FromSlash(...), and the skill writers join with the OS separator — so
// on Windows these arrive here as "prisma\schema.prisma" and
// ".claude\skills\codefit\SKILL.md". Printing them that way makes init
// contradict itself: RenderConfig slash-normalizes the same schema path into the
// .codefit.yaml written by the same run ("paths are slash-normalized so the
// committed file is portable across operating systems"), and every path codefit
// puts in a finding is filepath.ToSlash'd before it reaches the agent. The
// report is the same product speaking about the same files, so it uses the same
// spelling — and stays copy-pasteable into the config and into docs.
func reportPath(p string) string { return filepath.ToSlash(p) }

func agentNames(skills []scaffold.SkillWrite) []string {
	out := make([]string, len(skills))
	for i, s := range skills {
		out[i] = s.Agent
	}
	return out
}
