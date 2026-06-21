package cli

import (
	"slices"
	"testing"

	"github.com/spf13/cobra"
)

func subcommandNames(cmd *cobra.Command) []string {
	names := make([]string, 0, len(cmd.Commands()))
	for _, c := range cmd.Commands() {
		names = append(names, c.Name())
	}
	return names
}

func child(t *testing.T, parent *cobra.Command, name string) *cobra.Command {
	t.Helper()
	for _, c := range parent.Commands() {
		if c.Name() == name {
			return c
		}
	}
	t.Fatalf("command %q has no subcommand %q (has: %v)", parent.Name(), name, subcommandNames(parent))
	return nil
}

func TestRootHasAllSubcommands(t *testing.T) {
	root := newRootCmd()
	want := []string{
		"init", "mcp", "auth", "set", "status",
	}
	got := subcommandNames(root)
	for _, name := range want {
		if !slices.Contains(got, name) {
			t.Errorf("root is missing subcommand %q (has: %v)", name, got)
		}
	}
}

func TestRootHasGlobalFlags(t *testing.T) {
	pf := newRootCmd().PersistentFlags()
	for _, name := range []string{"config", "output", "out-file", "fail-on", "quiet", "no-llm", "verbose", "tui", "no-tui"} {
		if pf.Lookup(name) == nil {
			t.Errorf("root is missing global flag --%s", name)
		}
	}
}

func TestNestedCommands(t *testing.T) {
	root := newRootCmd()
	child(t, child(t, root, "mcp"), "serve")
	auth := child(t, root, "auth")
	for _, name := range []string{"login", "logout", "status"} {
		child(t, auth, name)
	}
	child(t, child(t, root, "set"), "model")
}

// TestEverySubcommandHasShortHelp guards against shipping a command with no
// help text — the command tree must be self-documenting.
func TestEverySubcommandHasShortHelp(t *testing.T) {
	var walk func(cmd *cobra.Command)
	walk = func(cmd *cobra.Command) {
		for _, c := range cmd.Commands() {
			if c.Short == "" {
				t.Errorf("command %q has no Short help", c.CommandPath())
			}
			walk(c)
		}
	}
	walk(newRootCmd())
}
