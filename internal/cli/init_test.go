package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/codefit-cli/codefit/internal/scaffold"
)

func TestInitCmdHasFlags(t *testing.T) {
	cmd := newInitCmd()
	for _, name := range []string{"force", "non-interactive"} {
		if cmd.Flags().Lookup(name) == nil {
			t.Errorf("init is missing flag --%s", name)
		}
	}
}

func TestFormatReportCreatedWithAgents(t *testing.T) {
	res := scaffold.Result{
		Info: scaffold.ProjectInfo{
			Name: "bitacora", Language: "typescript", Framework: "next",
			ORM: "prisma", DBType: "postgresql",
			SchemaPaths: []string{"prisma/schema.prisma"}, RouteHandlers: 34,
		},
		ConfigPath:   ".codefit.yaml",
		ConfigAction: scaffold.ConfigCreated,
		Skills: []scaffold.SkillWrite{
			{Agent: "OpenCode", Path: filepath.FromSlash(".opencode/skills/codefit/SKILL.md")},
			{Agent: "Claude Code", Path: filepath.FromSlash(".claude/skills/codefit/SKILL.md")},
		},
	}
	out := formatReport(res)

	for _, want := range []string{
		"typescript", "next", "prisma", "postgresql", "34",
		".codefit.yaml", "OpenCode", "Claude Code",
		".opencode/skills/codefit/SKILL.md", ".claude/skills/codefit/SKILL.md",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("report missing %q\n---\n%s", want, out)
		}
	}
}

func TestFormatReportSkippedAndFallback(t *testing.T) {
	skipped := formatReport(scaffold.Result{
		Info:         scaffold.ProjectInfo{Name: "x", Language: "go"},
		ConfigPath:   ".codefit.yaml",
		ConfigAction: scaffold.ConfigSkipped,
		UsedFallback: true,
		Skills:       []scaffold.SkillWrite{{Agent: "standard location", Path: filepath.FromSlash(".agents/skills/codefit/SKILL.md")}},
	})
	if !strings.Contains(skipped, "--force") {
		t.Errorf("skipped report must mention --force to regenerate:\n%s", skipped)
	}
	if !strings.Contains(strings.ToLower(skipped), "no known agents") {
		t.Errorf("fallback report must say no known agents were detected:\n%s", skipped)
	}
}

// End-to-end: init on a real project tree (non-interactive) writes the config and
// the skill, and reports what it did.
func TestInitCommandEndToEnd(t *testing.T) {
	root := t.TempDir()
	writeTSProject(t, root)
	if err := os.MkdirAll(filepath.Join(root, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}

	out, err := runInit(t, root, "", "--non-interactive")
	if err != nil {
		t.Fatalf("init: %v\n%s", err, out)
	}
	if !fileThere(filepath.Join(root, ".codefit.yaml")) {
		t.Errorf(".codefit.yaml not created")
	}
	if !fileThere(filepath.Join(root, ".claude/skills/codefit/SKILL.md")) {
		t.Errorf("skill not placed for Claude Code")
	}
	if !strings.Contains(out, "typescript") {
		t.Errorf("output should report the detected language:\n%s", out)
	}
}

// runInit executes the root command with `init <root> <extra...>` and returns its
// combined output. stdin is wired to the given reader for the interactive prompt.
func runInit(t *testing.T, root string, stdin string, extra ...string) (string, error) {
	t.Helper()
	cmd := newRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetIn(strings.NewReader(stdin))
	cmd.SetArgs(append([]string{"init", root}, extra...))
	err := cmd.Execute()
	return buf.String(), err
}

func TestConfirmOverwrite(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"y\n", true},
		{"yes\n", true},
		{"Y\n", true},
		{"\n", false},  // empty → default NO
		{"n\n", false}, // explicit no
		{"", false},    // EOF → default NO
		{"nope\n", false},
	}
	for _, c := range cases {
		got, err := confirmOverwrite(&bytes.Buffer{}, strings.NewReader(c.in))
		if err != nil {
			t.Errorf("confirmOverwrite(%q): unexpected error %v", c.in, err)
		}
		if got != c.want {
			t.Errorf("confirmOverwrite(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

// The interactive flow: an existing config is regenerated on "y" and left alone
// on an empty answer — the dev always decides.
func TestInitInteractiveOverwriteDecision(t *testing.T) {
	setup := func(t *testing.T) string {
		root := t.TempDir()
		writeTSProject(t, root)
		if err := os.WriteFile(filepath.Join(root, ".codefit.yaml"), []byte("version: \"old\"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		return root
	}

	t.Run("yes regenerates", func(t *testing.T) {
		root := setup(t)
		out, err := runInit(t, root, "y\n")
		if err != nil {
			t.Fatalf("init: %v\n%s", err, out)
		}
		data, _ := os.ReadFile(filepath.Join(root, ".codefit.yaml"))
		if strings.Contains(string(data), `"old"`) {
			t.Errorf("config was not regenerated on 'y':\n%s", data)
		}
	})

	t.Run("empty keeps existing", func(t *testing.T) {
		root := setup(t)
		out, err := runInit(t, root, "\n")
		if err != nil {
			t.Fatalf("init: %v\n%s", err, out)
		}
		data, _ := os.ReadFile(filepath.Join(root, ".codefit.yaml"))
		if !strings.Contains(string(data), `"old"`) {
			t.Errorf("existing config was modified despite empty answer:\n%s", data)
		}
		if !strings.Contains(out, "left unchanged") {
			t.Errorf("output should report the config was left unchanged:\n%s", out)
		}
	})
}

func writeTSProject(t *testing.T, root string) {
	t.Helper()
	write := func(rel, content string) {
		full := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("package.json", `{"name":"demo","dependencies":{"next":"14.0.0","react":"18.0.0"}}`)
	write("tsconfig.json", `{"compilerOptions":{"strict":true}}`)
	write("app/users/route.ts", "export async function GET() { return Response.json(await prisma.user.findMany()); }\n")
}

func fileThere(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
