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

	writeSchemaGap(&b, info)

	fmt.Fprintln(&b, "\nNext: connect codefit over MCP (see README → \"Connect codefit\").")
	return b.String()
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
// and says so. That is honest, and it is precisely why detecting SQL migration
// directories is the follow-up work.
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
	fmt.Fprintln(b, "  codefit detects a schema only from a Prisma schema.prisma. SQL migration")
	fmt.Fprintln(b, "  directories (Flyway, golang-migrate, plain .sql DDL) are NOT detected, so no")
	fmt.Fprintln(b, "  database.schema_paths was written and the DB dimension audits nothing here.")
	fmt.Fprintln(b, "  If this project has a schema, add it by hand — the commented block in")
	fmt.Fprintln(b, "  .codefit.yaml shows how — and the DB dimension runs on the next scan.")
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
