package cli

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/yappachino-dev/yci/internal/app"
	"github.com/yappachino-dev/yci/internal/library"
)

const flagYes = "yes"

// stdinIsTTY reports whether standard input is an interactive terminal.
// Note that a plain ModeCharDevice check is not enough: /dev/null is a
// character device too.
func stdinIsTTY() bool {
	return term.IsTerminal(int(os.Stdin.Fd()))
}

// confirmDestructive gates irreversible commands: --yes bypasses, an
// interactive session gets a prompt, anything else fails closed.
func confirmDestructive(cmd *cobra.Command, prompt string) (bool, error) {
	if yes, _ := cmd.Flags().GetBool(flagYes); yes {
		return true, nil
	}

	if !stdinIsTTY() {
		return false, fmt.Errorf("non-interactive session: re-run with --%s to confirm", flagYes)
	}

	var confirmed bool
	form := huh.NewForm(huh.NewGroup(
		huh.NewConfirm().Title(prompt).Value(&confirmed),
	)).WithTheme(huh.ThemeBase())

	if err := form.Run(); err != nil {
		return false, fmt.Errorf("read confirmation: %w", err)
	}

	return confirmed, nil
}

// idFromArgsOrFlag returns the trimmed positional argument if present,
// otherwise the trimmed value of the named flag. Supplying both is an error.
func idFromArgsOrFlag(cmd *cobra.Command, flagName string, args []string) (string, error) {
	switch len(args) {
	case 0:
		v, _ := cmd.Flags().GetString(flagName)
		return strings.TrimSpace(v), nil
	case 1:
		if cmd.Flags().Changed(flagName) {
			return "", fmt.Errorf("specify the ID either as an argument or via --%s, not both", flagName)
		}

		return strings.TrimSpace(args[0]), nil
	default:
		return "", fmt.Errorf("expected at most one ID argument")
	}
}

// resolveTrackID expands an ID prefix to the unique matching track. Full
// IDs pass through; ambiguous prefixes are rejected with the candidate list.
func resolveTrackID(ctx context.Context, c *app.Container, input string) (library.TrackID, error) {
	tracks, err := c.TrackService.All(ctx)
	if err != nil {
		return "", fmt.Errorf("search tracks: %w", err)
	}

	hasExact := false
	var matches []library.Track
	for _, t := range tracks {
		if string(t.ID) == input {
			hasExact = true
		}

		if strings.HasPrefix(string(t.ID), input) {
			matches = append(matches, t)
		}
	}

	if hasExact {
		return library.TrackID(input), nil
	}

	switch len(matches) {
	case 0:
		return "", fmt.Errorf("no track matches ID %q", input)
	case 1:
		return matches[0].ID, nil
	default:
		ids := make([]string, 0, len(matches))
		for _, m := range matches {
			ids = append(ids, string(m.ID))
		}

		return "", fmt.Errorf("ambiguous ID prefix %q matches %d tracks: %s", input, len(matches), strings.Join(ids, ", "))
	}
}
