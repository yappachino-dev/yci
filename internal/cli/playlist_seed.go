package cli

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"

	"github.com/yappachino-dev/yci/internal/library"
)

const (
	flagFromTrack = "from-track"
	flagFragment  = "fragment"
	flagAcceptAll = "accept-all"
)

// seedMatchPreviewCap bounds how many matched tracks are listed. A
// performer family is routinely 60 tracks, and printing all of them before a
// confirmation buries the question being asked.
const seedMatchPreviewCap = 15

// seedFormWidth is the render width of the token picker. huh does not default
// it, and a zero-width input panics in bubbles' placeholder view.
const seedFormWidth = 72

// seedPlaylist derives a fragment from a seed track, shows what it matches,
// and materializes the result as a static playlist.
//
// The fragment is a token set with AND semantics rather than a joined
// substring, so "Mori Calliope" also reaches the run-together
// "MoriCalliope" spelling. Nothing is written until the user confirms, and
// the whole membership is added in one transaction.
func seedPlaylist(cmd *cobra.Command, name, seedInput, fragmentFlag string, acceptAll bool) error {
	ctx := cmd.Context()
	c := Container(ctx)

	seed, err := resolveTrackID(ctx, c, seedInput)
	if err != nil {
		return err
	}

	tracks, err := c.TrackService.All(ctx)
	if err != nil {
		return fmt.Errorf("list tracks: %w", err)
	}

	corpus := make([]string, 0, len(tracks))
	for _, t := range tracks {
		corpus = append(corpus, t.Title)
	}

	cmd.Printf("Seed track: %s\n\n", seedTitleOf(tracks, seed))

	var tokens []string
	suggested := library.DeriveFragment(seedTitleOf(tracks, seed), corpus)

	switch {
	case fragmentFlag != "":
		tokens = library.Tokenize(fragmentFlag)
		if len(tokens) == 0 {
			return fmt.Errorf("--%s %q has no usable tokens", flagFragment, fragmentFlag)
		}
	case acceptAll:
		tokens = suggested.Tokens
		cmd.Printf("Fragment: %s (%d matches, %s)\n\n",
			suggested.FragmentText(), suggested.Recurrence, confidenceNote(suggested))
	default:
		if !stdinIsTTY() {
			return fmt.Errorf("non-interactive session: pass --%s to use the derived fragment, or --%s to choose tokens", flagAcceptAll, flagFragment)
		}

		tokens, err = pickFragmentTokens(cmd, seedTitleOf(tracks, seed), suggested, corpus)
		if err != nil {
			return err
		}
	}

	matched := make([]library.Track, 0, len(tracks))
	for _, t := range tracks {
		if library.MatchesFragment(t.Title, tokens) {
			matched = append(matched, t)
		}
	}

	if len(matched) == 0 {
		return fmt.Errorf("no track matches the fragment %q", strings.Join(tokens, " "))
	}

	printSeedMatches(cmd, matched)

	ok, err := confirmDestructive(cmd, fmt.Sprintf("Add these %d tracks to playlist %q?", len(matched), name))
	if err != nil {
		return err
	}
	if !ok {
		cmd.Println("Cancelled. No playlist was created.")
		return nil
	}

	playlist, err := c.PlaylistService.Create(ctx, name)
	if err != nil {
		return mapSentinels(err, map[error]string{
			library.ErrPlaylistAlreadyExists: fmt.Sprintf("playlist %q already exists", name),
		})
	}

	ids := make([]library.TrackID, 0, len(matched))
	for _, t := range matched {
		ids = append(ids, t.ID)
	}

	if err := c.PlaylistService.AddTracks(ctx, playlist.Name, ids); err != nil {
		return fmt.Errorf("add tracks to playlist %q: %w", name, err)
	}

	cmd.Printf("\nCreated playlist %q with %d tracks.\n", playlist.Name, len(ids))
	cmd.Printf("Export it with: yci playlist export %s\n", playlist.Name)

	return nil
}

func seedTitleOf(tracks []library.Track, id library.TrackID) string {
	for _, t := range tracks {
		if t.ID == id {
			return t.Title
		}
	}
	return ""
}

func confidenceNote(f library.Fragment) string {
	if f.Certain {
		return "high confidence"
	}
	return "low confidence: matches only the seed, check the list before accepting"
}

func printSeedMatches(cmd *cobra.Command, matched []library.Track) {
	w := newTabWriter(cmd.OutOrStdout())
	fmt.Fprintln(w, "ID\tTITLE")

	for i, t := range matched {
		if i == seedMatchPreviewCap {
			fmt.Fprintf(w, "...\tand %d more\n", len(matched)-seedMatchPreviewCap)
			break
		}
		fmt.Fprintf(w, "%s\t%s\n", shortTrackID(t.ID), truncateCell(t.Title, 60))
	}

	w.Flush()
}

// pickFragmentTokens shows the seed's tokens and lets the user choose which
// ones identify the family. The default is the derived selection; typing
// nothing accepts it. The match count is shown for every attempt, so a bad
// choice is visible immediately rather than after the playlist is written.
func pickFragmentTokens(cmd *cobra.Command, seedTitle string, suggested library.Fragment, corpus []string) ([]string, error) {
	tokens := library.Tokenize(seedTitle)

	if len(tokens) == 0 {
		return nil, fmt.Errorf("seed title %q has no tokens to choose from", seedTitle)
	}

	cmd.Println("Title tokens:")
	for i, t := range tokens {
		marker := " "
		if containsString(suggested.Tokens, t) {
			marker = "*"
		}
		cmd.Printf("  %s %d) %s\n", marker, i+1, t)
	}
	cmd.Printf("\nSuggested: %s (%d matches)\n", suggested.FragmentText(), suggested.Recurrence)

	for {
		var answer string
		prompt := huh.NewInput().
			Title("Token numbers to match on (comma separated, empty to accept the suggestion)").
			Description(fmt.Sprintf("Suggested: %s", strings.Join(defaultSelection(suggested.Tokens, tokens), ", "))).
			CharLimit(64).
			Value(&answer)

		// The form needs an explicit width. Without one the input renders
		// at width zero and bubbles' placeholder view sizes a rune slice
		// from a negative length, panicking on first draw.
		form := huh.NewForm(huh.NewGroup(prompt)).WithTheme(huh.ThemeBase()).WithWidth(seedFormWidth)

		if err := form.Run(); err != nil {
			return nil, fmt.Errorf("read token selection: %w", err)
		}

		answer = strings.TrimSpace(answer)
		if answer == "" {
			answer = strings.Join(defaultSelection(suggested.Tokens, tokens), ",")
		}

		selected, err := parseTokenSelection(answer, tokens)
		if err != nil {
			cmd.Printf("%v\n", err)
			continue
		}

		matches := 0
		for _, title := range corpus {
			if library.MatchesFragment(title, selected) {
				matches++
			}
		}

		cmd.Printf("Fragment %q matches %d tracks.\n", strings.Join(selected, " "), matches)
		if matches == 0 {
			cmd.Println("That selection matches nothing. Try again.")
			continue
		}

		return selected, nil
	}
}

func defaultSelection(suggested, all []string) []string {
	var out []string
	for _, t := range all {
		if containsString(suggested, t) {
			out = append(out, t)
		}
	}
	return out
}

// parseTokenSelection turns "1,3-5" into the matching tokens, preserving
// title order regardless of the order they were typed in.
func parseTokenSelection(input string, tokens []string) ([]string, error) {
	seen := map[int]bool{}

	for _, part := range strings.Split(input, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		lo, hi, isRange := strings.Cut(part, "-")
		start, err := strconv.Atoi(strings.TrimSpace(lo))
		if err != nil {
			return nil, fmt.Errorf("%q is not a token number", part)
		}

		end := start
		if isRange {
			if end, err = strconv.Atoi(strings.TrimSpace(hi)); err != nil {
				return nil, fmt.Errorf("%q is not a token range", part)
			}
		}

		if start < 1 || end > len(tokens) || start > end {
			return nil, fmt.Errorf("token %q is out of range: choose between 1 and %d", part, len(tokens))
		}

		for i := start; i <= end; i++ {
			seen[i] = true
		}
	}

	if len(seen) == 0 {
		return nil, fmt.Errorf("select at least one token")
	}

	selected := make([]string, 0, len(seen))
	for i, t := range tokens {
		if seen[i+1] {
			selected = append(selected, t)
		}
	}

	return selected, nil
}

func containsString(haystack []string, needle string) bool {
	for _, h := range haystack {
		if h == needle {
			return true
		}
	}
	return false
}
