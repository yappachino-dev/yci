package cli

import (
	"bytes"
	"errors"
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
