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

// resolveIDByPrefix expands an ID prefix to the unique matching entity of
// any ID type. Full IDs pass through unchanged; ambiguous prefixes are
// rejected with the candidate list. An empty input never matches.
func resolveIDByPrefix[T ~string](input string, all []T, kind string) (T, error) {
	if input == "" {
		return "", fmt.Errorf("no %s matches empty ID", kind)
	}

	hasExact := false
	var matches []T
	for _, id := range all {
		if string(id) == input {
			hasExact = true
		}

		if strings.HasPrefix(string(id), input) {
			matches = append(matches, id)
		}
	}

	if hasExact {
		return T(input), nil
	}

	switch len(matches) {
	case 0:
		return "", fmt.Errorf("no %s matches ID %q", kind, input)
	case 1:
		return matches[0], nil
	default:
		ids := make([]string, 0, len(matches))
		for _, m := range matches {
			ids = append(ids, string(m))
		}

		return "", fmt.Errorf("ambiguous ID prefix %q matches %d %ss: %s", input, len(matches), kind, strings.Join(ids, ", "))
	}
}

// resolveTrackID expands an ID prefix to the unique matching track.
func resolveTrackID(ctx context.Context, c *app.Container, input string) (library.TrackID, error) {
	tracks, err := c.TrackService.All(ctx)
	if err != nil {
		return "", fmt.Errorf("search tracks: %w", err)
	}

	ids := make([]library.TrackID, 0, len(tracks))
	for _, t := range tracks {
		ids = append(ids, t.ID)
	}

	return resolveIDByPrefix(input, ids, "track")
}

// resolveArtistID expands an ID prefix to the unique matching artist.
func resolveArtistID(ctx context.Context, c *app.Container, input string) (library.ArtistID, error) {
	artists, err := c.ArtistService.All(ctx)
	if err != nil {
		return "", fmt.Errorf("search artists: %w", err)
	}

	ids := make([]library.ArtistID, 0, len(artists))
	for _, a := range artists {
		ids = append(ids, a.ID)
	}

	return resolveIDByPrefix(input, ids, "artist")
}

// resolveFolderID expands an ID prefix to the unique matching folder.
func resolveFolderID(ctx context.Context, c *app.Container, input string) (library.FolderID, error) {
	folders, err := c.FolderService.All(ctx)
	if err != nil {
		return "", fmt.Errorf("search folders: %w", err)
	}

	ids := make([]library.FolderID, 0, len(folders))
	for _, f := range folders {
		ids = append(ids, f.ID)
	}

	return resolveIDByPrefix(input, ids, "folder")
}
