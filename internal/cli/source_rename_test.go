package cli

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/yappachino-dev/yci/internal/app"
	"github.com/yappachino-dev/yci/internal/library"
)

func seedSource(t *testing.T, c *app.Container, name string) {
	t.Helper()

	err := c.SourceService.Add(t.Context(), name, "https://example.com/"+name,
		[]library.SourceChannel{{ID: "UCa", Name: "A"}})
	if err != nil {
		t.Fatalf("seed source %q: %v", name, err)
	}
}

// TestSourceRenameRelabelsWithoutRefetching drives the command the way a user
// does. It works offline on purpose: a rename that needed the network or a
// prompt could not be used to fix a label without re-picking channels.
func TestSourceRenameRelabelsWithoutRefetching(t *testing.T) {
	c := newTestContainer(t)
	seedSource(t, c, "before")

	cmd, out, _ := splitCmd(t, c)
	cmd.SetArgs([]string{"before", "after"})

	if err := sourceRenameCmd.RunE(cmd, []string{"before", "after"}); err != nil {
		t.Fatalf("rename: %v", err)
	}

	if _, err := c.SourceService.ByName(t.Context(), "after"); err != nil {
		t.Errorf("the new name does not resolve: %v", err)
	}
	if _, err := c.SourceService.ByName(t.Context(), "before"); err == nil {
		t.Error("the old name still resolves")
	}

	// The confirmation is the requested result, so it belongs on stdout.
	if !strings.Contains(out.String(), `"after"`) {
		t.Errorf("stdout is missing the confirmation: %q", out.String())
	}
}

func TestSourceRenameCompletionStopsAtTheSecondArgument(t *testing.T) {
	c := newTestContainer(t)
	seedSource(t, c, "keepme")

	cmd, _, _ := splitCmd(t, c)

	first, _ := sourceNameCompletion(cmd, nil, "ke")
	if len(first) == 0 {
		t.Fatal("the existing source is not offered as the first argument")
	}

	// `source rename <current> <new>`: the second argument is a name the
	// user is choosing, so offering existing names would be wrong.
	second, directive := sourceNameCompletion(cmd, []string{"keepme"}, "ke")
	if len(second) != 0 {
		t.Errorf("the second argument offered %v, want nothing", second)
	}
	if directive != cobra.ShellCompDirectiveNoFileComp {
		t.Errorf("the second argument falls back to filenames: %v", directive)
	}
}
