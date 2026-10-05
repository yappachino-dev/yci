package cli

import (
	"context"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// completionRule maps a command path to the completion it should carry, and
// states why. A command whose identifying argument names something the user
// chooses rather than selects is absent on purpose.
var completionRule = map[string]cobra.CompletionFunc{
	"playlist edit":   playlistNameCompletion,
	"playlist show":   playlistNameCompletion,
	"playlist add":    playlistOrTrackCompletion,
	"playlist remove": playlistOrTrackCompletion,
	"playlist delete": playlistNameCompletion,
	"playlist export": playlistNameCompletion,
	"playlist reseed": playlistNameCompletion,
	"source update":   sourceNameCompletion,
	"source remove":   sourceNameCompletion,
	"source rename":   sourceNameCompletion,
	"download retry":  queueIDCompletion,
	"folder edit":     folderIDCompletion,
	"folder delete":   folderIDCompletion,
	"artist edit":     artistIDCompletion,
	"artist delete":   artistIDCompletion,
	"track delete":    trackIDCompletion,
	"config get":      configKeyCompletion,
	"config set":      configKeyCompletion,
}

// TestCommandsThatNameAnIdentifierOfferCompletion: a command whose argument is
// something the user selects from the library should complete it.
//
// Without this, every completion call in the tree could be deleted and the
// suite would stay green, and a new command could ship taking an identifier
// with nothing to suggest it.
//
// `artist create` and `folder create` are deliberately excluded: their
// positional is a slug being chosen, so completing to existing values would
// be wrong rather than merely unhelpful.
func TestCommandsThatNameAnIdentifierOfferCompletion(t *testing.T) {
	rootCmd.InitDefaultHelpCmd()
	rootCmd.InitDefaultCompletionCmd()

	for path, want := range completionRule {
		cmd := resolveCommand(t, path)
		if cmd.ValidArgsFunction == nil {
			t.Errorf("%s takes an identifier but offers no completion", path)
			continue
		}

		if fnName(cmd.ValidArgsFunction) != fnName(want) {
			t.Errorf("%s completes with %s, want %s",
				path, fnName(cmd.ValidArgsFunction), fnName(want))
		}
	}
}

// TestChooserCommandsOfferNoCompletion pins the exception, because it is a
// deliberate omission and looks like a gap to the next reader.
func TestChooserCommandsOfferNoCompletion(t *testing.T) {
	for _, path := range []string{"artist create", "folder create", "playlist create"} {
		if cmd := resolveCommand(t, path); cmd.ValidArgsFunction != nil {
			t.Errorf("%s names something the user chooses, but completes it", path)
		}
	}
}

// TestPlaylistAddCompletesBothArguments: add takes a playlist name and then
// track IDs, so completing one list for both positions would be wrong.
func TestPlaylistAddCompletesBothArguments(t *testing.T) {
	cmd := resolveCommand(t, "playlist add")
	// Completion runs without the boot sequence, so the command carries no
	// container; the helpers must offer nothing rather than reach for one.
	cmd.SetContext(context.Background())

	if _, directive := cmd.ValidArgsFunction(cmd, nil, ""); directive != cobra.ShellCompDirectiveNoFileComp {
		t.Errorf("first argument directive = %v, want NoFileComp", directive)
	}

	if _, directive := cmd.ValidArgsFunction(cmd, []string{"Identity"}, ""); directive != cobra.ShellCompDirectiveNoFileComp {
		t.Errorf("second argument directive = %v, want NoFileComp", directive)
	}
}

// fnName identifies a completion function by name, so the rule can compare
// them without depending on declaration order in a table.
func fnName(fn cobra.CompletionFunc) string {
	name := runtime.FuncForPC(reflect.ValueOf(fn).Pointer()).Name()
	return name[strings.LastIndex(name, ".")+1:]
}

func resolveCommand(t *testing.T, path string) *cobra.Command {
	t.Helper()

	cmd, _, err := rootCmd.Find(strings.Fields(path))
	if err != nil {
		t.Fatalf("resolve %q: %v", path, err)
	}

	if cmd == rootCmd {
		t.Fatalf("%q did not resolve to a command", path)
	}

	return cmd
}
