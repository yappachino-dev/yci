package cli

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// newUsageTestRoot builds a command tree shaped like the real one: a root
// PersistentPreRunE that calls the same hook rootCmd does, plus a child that
// fails while running.
func newUsageTestRoot(runErr error, out, errOut *bytes.Buffer) *cobra.Command {
	root := &cobra.Command{
		Use: "test",
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			silenceUsageOnRuntimeError(cmd)
			return nil
		},
	}

	root.AddCommand(&cobra.Command{
		Use:  "boom",
		RunE: func(*cobra.Command, []string) error { return runErr },
	})

	root.SetOut(out)
	root.SetErr(errOut)
	root.SetArgs([]string{"boom"})

	return root
}

// TestSilenceUsageOnRuntimeError pins the distinction the hook exists for: a
// failure during real work reports only the error, while a usage mistake
// still gets the help text that explains the mistake.
func TestSilenceUsageOnRuntimeError(t *testing.T) {
	t.Run("runtime error omits usage", func(t *testing.T) {
		var out, errOut bytes.Buffer

		root := newUsageTestRoot(errors.New("library root unavailable"), &out, &errOut)
		if err := root.Execute(); err == nil {
			t.Fatal("Execute() = nil, want the command error")
		}

		combined := out.String() + errOut.String()
		if !strings.Contains(combined, "library root unavailable") {
			t.Errorf("output = %q, want the error surfaced", combined)
		}
		if strings.Contains(combined, "Usage:") {
			t.Errorf("output = %q, want no usage dump for a runtime error", combined)
		}
	})

	t.Run("unknown flag keeps usage", func(t *testing.T) {
		var out, errOut bytes.Buffer

		root := newUsageTestRoot(nil, &out, &errOut)
		root.SetArgs([]string{"boom", "--nonsense"})

		if err := root.Execute(); err == nil {
			t.Fatal("Execute() = nil, want a flag parsing error")
		}

		combined := out.String() + errOut.String()
		if !strings.Contains(combined, "Usage:") {
			t.Errorf("output = %q, want usage retained for a flag mistake", combined)
		}
	})

	t.Run("bad argument count keeps usage", func(t *testing.T) {
		var out, errOut bytes.Buffer

		root := newUsageTestRoot(nil, &out, &errOut)
		child, _, err := root.Find([]string{"boom"})
		if err != nil {
			t.Fatal(err)
		}
		child.Args = cobra.ExactArgs(1)
		root.SetArgs([]string{"boom", "a", "b"})

		if err := root.Execute(); err == nil {
			t.Fatal("Execute() = nil, want an argument validation error")
		}

		combined := out.String() + errOut.String()
		if !strings.Contains(combined, "Usage:") {
			t.Errorf("output = %q, want usage retained for wrong arguments", combined)
		}
	})
}

// TestEveryNounGroupRejectsUnknownSubcommand pins the shape of every noun
// group in the real command tree.
//
// The failure this guards against is silent: a non-runnable command prints
// help and exits 0, so a typo looks like success to `set -e` and help text
// lands where a --json consumer expects data. Enumerating the real groups
// rather than a synthetic tree is the point, because the bug is a property of
// how these commands are declared.
func TestEveryNounGroupRejectsUnknownSubcommand(t *testing.T) {
	groups := []*cobra.Command{
		folderCmd, artistCmd, channelCmd, trackCmd, playlistCmd,
		configCmd, importCmd, debugCmd,
	}

	for _, g := range groups {
		if g.RunE == nil {
			t.Errorf("group %q has no RunE: Cobra will print help and exit 0 for an unknown subcommand. Wrap it in asGroup.", g.Name())
		}
		if g.Args == nil {
			t.Errorf("group %q has no Args: an unknown subcommand would be accepted", g.Name())
		}
	}
}

// TestAsGroupRejectsUnknownSubcommand executes one to prove the shape check
// above means what it says: the error is returned, not swallowed.
func TestAsGroupRejectsUnknownSubcommand(t *testing.T) {
	root := &cobra.Command{Use: "test"}
	group := asGroup(&cobra.Command{Use: "thing"})
	group.AddCommand(&cobra.Command{
		Use:   "list",
		RunE:  func(*cobra.Command, []string) error { return nil },
		Short: "List things",
	})
	root.AddCommand(group)

	root.SetOut(io.Discard)
	root.SetErr(io.Discard)
	root.SetArgs([]string{"thing", "nope"})

	err := root.Execute()
	if err == nil {
		t.Fatal("Execute() = nil, want an unknown-command error")
	}
	if !strings.Contains(err.Error(), `unknown command "nope"`) {
		t.Errorf("error = %q, want it to name the unknown subcommand", err)
	}
}

// TestAsGroupShowsHelpOnNoArgs is the other half: making the group runnable
// must not turn `yci thing` into an error.
func TestAsGroupShowsHelpOnNoArgs(t *testing.T) {
	out := &bytes.Buffer{}
	root := &cobra.Command{Use: "test"}
	group := asGroup(&cobra.Command{Use: "thing", Short: "Manage things"})
	group.AddCommand(&cobra.Command{
		Use:   "list",
		RunE:  func(*cobra.Command, []string) error { return nil },
		Short: "List things",
	})
	root.AddCommand(group)

	root.SetOut(out)
	root.SetErr(io.Discard)
	root.SetArgs([]string{"thing"})

	if err := root.Execute(); err != nil {
		t.Fatalf("Execute() = %v, want help with no error", err)
	}
	if !strings.Contains(out.String(), "Manage things") {
		t.Errorf("output = %q, want the group help", out.String())
	}
}
