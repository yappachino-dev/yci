package cli

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"

	"github.com/yappachino-dev/yci/internal/app"
	"github.com/yappachino-dev/yci/internal/library"
)

const (
	flagFromTrack = "from-track"
	flagFragment  = "fragment"
	flagAcceptAll = "accept-all"
	flagMatch     = "match"
	flagExclude   = "exclude"
	flagInclude   = "include"
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
func seedPlaylist(cmd *cobra.Command, name, seedInput, fragmentFlag string, acceptAll bool, mode library.MatchMode, excludeFlags []string) error {
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

	// The suggestion is derived for AND semantics. Under ANY the same
	// tokens mean something wider, so the quoted count below is
	// recomputed for the mode actually in force rather than reused.
	switch {
	case fragmentFlag != "":
		tokens = library.Tokenize(fragmentFlag)
		if len(tokens) == 0 {
			return fmt.Errorf("--%s %q has no usable tokens", flagFragment, fragmentFlag)
		}
	case acceptAll:
		tokens = suggested.Tokens
		if mode == library.MatchAny {
			cmd.Printf("Fragment: %s, matching ANY (wider than the derived selection)\n\n",
				library.FormatTokens(tokens, mode))
		} else {
			cmd.Printf("Fragment: %s (%d matches, %s)\n\n",
				suggested.FragmentText(), suggested.Recurrence, confidenceNote(suggested))
		}
	default:
		if !stdinIsTTY() {
			return fmt.Errorf("non-interactive session: pass --%s to use the derived fragment, or --%s to choose tokens", flagAcceptAll, flagFragment)
		}

		tokens, mode, err = pickFragmentTokens(cmd, seedTitleOf(tracks, seed), suggested, corpus, mode)
		if err != nil {
			return err
		}
	}

	// Exclusions named on the command line are resolved first, so a typo is
	// reported before anything is derived rather than after.
	excludes, err := resolveExclusions(ctx, c, excludeFlags)
	if err != nil {
		return err
	}

	// Derive goes through the service so seeding and reseeding cannot
	// disagree about what a token set means.
	matched, err := c.PlaylistService.Derive(ctx, tokens, mode, excludes)
	if err != nil {
		return err
	}

	if len(matched) == 0 {
		return fmt.Errorf("no track matches the fragment %s", describeTokens(tokens, mode))
	}

	// An interactive session gets a second pass in which individual
	// matches can be deselected. Lexical matching selects on spelling, not
	// on work identity, so a performer family also brings in collabs and
	// self-covers; one-time review of the whole set is the only place that
	// judgement can be applied.
	if !acceptAll && fragmentFlag == "" && !flagWasSet(cmd, flagExclude) && stdinIsTTY() {
		matched, excludes, err = pickCandidates(matched, excludes)
		if err != nil {
			return err
		}
		if len(matched) == 0 {
			return fmt.Errorf("every match was deselected; nothing to add to playlist %q", name)
		}
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

	ids := make([]library.TrackID, 0, len(matched))
	for _, t := range matched {
		ids = append(ids, t.ID)
	}

	// Membership and the recipe that explains it are written together, so a
	// playlist never exists holding rows that `playlist reseed` cannot
	// reproduce.
	playlist, err := c.PlaylistService.CreateFromSeed(ctx, name, seed, tokens, mode, excludes, ids)
	if err != nil {
		return mapSentinels(err, map[error]string{
			library.ErrPlaylistAlreadyExists: fmt.Sprintf("playlist %q already exists", name),
		})
	}

	cmd.Printf("\nCreated playlist %q with %d tracks (%s).\n", playlist.Name, len(ids), describeTokens(tokens, mode))
	if len(excludes) > 0 {
		cmd.Printf("Excluded %d track(s); they stay out when the recipe is re-derived.\n", len(excludes))
	}
	cmd.Printf("Re-derive it later with: yci playlist reseed %s\n", playlist.Name)
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
		fmt.Fprintf(w, "%s\t%s\n", shortTrackID(t.ID), truncateCell(t.Title, cellWidthTitleWide))
	}

	w.Flush()
}

// pickFragmentTokens shows the seed's tokens and lets the user choose which
// ones identify the family. The default is the derived selection; typing
// nothing accepts it. The match count is shown for every attempt, so a bad
// choice is visible immediately rather than after the playlist is written.
// pickFragmentTokens asks which tokens to match on and in which mode. The
// mode is passed in (from --match) and can be flipped at the prompt, so a
// caller who already chose ANY is not asked to re-assert it.
func pickFragmentTokens(cmd *cobra.Command, seedTitle string, suggested library.Fragment, corpus []string, mode library.MatchMode) ([]string, library.MatchMode, error) {
	tokens := library.Tokenize(seedTitle)

	if len(tokens) == 0 {
		return nil, mode, fmt.Errorf("seed title %q has no tokens to choose from", seedTitle)
	}

	cmd.Println("Title tokens:")
	for i, t := range tokens {
		marker := " "
		if containsString(suggested.Tokens, t) {
			marker = "*"
		}
		cmd.Printf("  %s %d) %s\n", marker, i+1, t)
	}
	cmd.Printf("\nSuggested: %s (%d matches, %s)\n", suggested.FragmentText(), suggested.Recurrence, mode)

	for {
		var answer string
		prompt := huh.NewInput().
			Title("Token numbers to match on (comma separated, empty to accept the suggestion)").
			Description(fmt.Sprintf("Suggested: %s   |   matching: %s (type AND or ANY to change)",
				formatIndices(suggestedIndices(suggested.Tokens, tokens)), mode)).
			CharLimit(64).
			Value(&answer)

		// The form needs an explicit width. Without one the input renders
		// at width zero and bubbles' placeholder view sizes a rune slice
		// from a negative length, panicking on first draw.
		form := huh.NewForm(huh.NewGroup(prompt)).WithTheme(huh.ThemeBase()).WithWidth(seedFormWidth)

		if err := form.Run(); err != nil {
			return nil, mode, fmt.Errorf("read token selection: %w", err)
		}

		answer = strings.TrimSpace(answer)

		// A bare mode word is not a selection; it changes the mode and
		// leaves the tokens to their suggested values. This is what makes
		// the mode reachable without restarting the command.
		switch strings.ToUpper(answer) {
		case "AND", "ALL":
			if mode != library.MatchAll {
				mode = library.MatchAll
				cmd.Printf("Matching mode: %s\n", mode)
			}
			answer = ""
		case "ANY", "OR":
			if mode != library.MatchAny {
				mode = library.MatchAny
				cmd.Printf("Matching mode: %s (this widens the selection)\n", mode)
			}
			answer = ""
		}

		if answer == "" {
			answer = formatIndices(suggestedIndices(suggested.Tokens, tokens))
		}
		if answer == "" {
			cmd.Println("No suggested tokens; type the numbers to match on.")
			continue
		}

		selected, err := parseTokenSelection(answer, tokens)
		if err != nil {
			cmd.Printf("%v\n", err)
			continue
		}

		matches := 0
		for _, title := range corpus {
			if library.MatchesTokens(title, selected, mode) {
				matches++
			}
		}

		cmd.Printf("Fragment %s matches %d tracks.\n", describeTokens(selected, mode), matches)
		if matches == 0 {
			cmd.Println("That selection matches nothing. Try again.")
			continue
		}

		return selected, mode, nil
	}
}

// describeTokens renders a token set with the mode spelled out, because
// `"Mori", "Calliope"` alone does not say whether the two must both appear.
func describeTokens(tokens []string, mode library.MatchMode) string {
	return fmt.Sprintf("%s %s", mode, library.FormatTokens(tokens, mode))
}

// suggestedIndices returns the 1-based positions of the tokens the derivation
// suggested, in title order.
//
// Positions, not token text: the prompt asks for numbers, so feeding it a
// token name produces "not a token number" the moment the user accepts the
// default. Title order is preserved so the echoed default matches the
// numbered list the user is looking at.
func suggestedIndices(suggested, all []string) []int {
	var out []int
	for i, t := range all {
		if containsString(suggested, t) {
			out = append(out, i+1)
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

// formatIndices renders positions for the prompt, or an empty string when
// there are none so the caller can tell "no suggestion" from "index 0".
func formatIndices(indices []int) string {
	parts := make([]string, 0, len(indices))
	for _, i := range indices {
		parts = append(parts, strconv.Itoa(i))
	}
	return strings.Join(parts, ",")
}

func containsString(haystack []string, needle string) bool {
	for _, h := range haystack {
		if h == needle {
			return true
		}
	}
	return false
}

// resolveExclusions turns --exclude values into track IDs, accepting the same
// unambiguous prefixes that every other command accepts.
//
// An ID that resolves to nothing is an error rather than a silent no-op: a
// mistyped prefix that quietly excluded nothing would leave the user
// believing a false positive had been dealt with.
func resolveExclusions(ctx context.Context, c *app.Container, values []string) ([]library.TrackID, error) {
	if len(values) == 0 {
		return nil, nil
	}

	out := make([]library.TrackID, 0, len(values))
	seen := make(map[library.TrackID]bool, len(values))

	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}

		id, err := resolveTrackID(ctx, c, value)
		if err != nil {
			return nil, fmt.Errorf("resolving --%s %q: %w", flagExclude, value, err)
		}

		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}

	return out, nil
}

// candidateSelectKey names the multiselect so its result can be read back
// from the form.
const candidateSelectKey = "candidates"

// candidateOption is one row in the multiselect. It is comparable so huh can
// use it as its value type, which is why the title is carried alongside the
// ID rather than looked up later.
type candidateOption struct {
	title string
	id    library.TrackID
}

// pickCandidates lets the user deselect matches that do not belong, and
// returns the kept set together with the exclusion list to record.
//
// Everything starts selected, because the derivation is the proposal and the
// user's job is to find the exceptions rather than to build the set. The
// multiselect filters, so a 60-track performer family is still navigable.
//
// The form needs an explicit width for the same reason the token input does:
// huh does not default it, and a zero-width field panics in bubbles'
// placeholder view.
func pickCandidates(matched []library.Track, alreadyExcluded []library.TrackID) ([]library.Track, []library.TrackID, error) {
	excluded := make(map[library.TrackID]bool, len(alreadyExcluded))
	for _, id := range alreadyExcluded {
		excluded[id] = true
	}

	options := make([]huh.Option[candidateOption], 0, len(matched))
	seeded := make([]candidateOption, 0, len(matched))

	for _, t := range matched {
		opt := candidateOption{title: t.Title, id: t.ID}
		options = append(options, huh.NewOption(
			fmt.Sprintf("%s  %s", shortTrackID(t.ID), truncateCell(t.Title, cellWidthTitleWide)),
			opt,
		))
		if !excluded[t.ID] {
			seeded = append(seeded, opt)
		}
	}

	// Order matters: Options must be set before Value, because Value is what
	// walks the options and marks the ones already in the initial value as
	// selected. Called the other way round, every row would come up
	// unselected and the user would have to select the whole family by hand.
	multi := huh.NewMultiSelect[candidateOption]().
		Key(candidateSelectKey).
		Title("Which of these belong in the playlist?").
		Description("Space toggles, / filters, enter confirms. All start selected — deselect the false positives.").
		Options(options...).
		Value(&seeded).
		Height(12).
		Filtering(true)

	form := huh.NewForm(huh.NewGroup(multi)).WithTheme(huh.ThemeBase()).WithWidth(seedFormWidth)
	if err := form.Run(); err != nil {
		return nil, alreadyExcluded, fmt.Errorf("read candidate selection: %w", err)
	}

	chosen, ok := form.Get(candidateSelectKey).([]candidateOption)
	if !ok {
		// No result is a huh behaviour change, not an empty selection:
		// falling back to the seeded set keeps the derived playlist rather
		// than silently creating an empty one.
		chosen = seeded
	}

	keep := make(map[library.TrackID]bool, len(chosen))
	for _, o := range chosen {
		keep[o.id] = true
	}

	final := make([]library.Track, 0, len(matched))
	exclusions := make([]library.TrackID, 0, len(alreadyExcluded))

	for _, t := range matched {
		if keep[t.ID] {
			final = append(final, t)
			continue
		}
		exclusions = append(exclusions, t.ID)
	}

	return final, exclusions, nil
}

// flagWasSet reports whether the user named a flag explicitly, as opposed to
// it holding its default. Used to decide whether an interactive session
// should still offer the candidate picker: passing --exclude is a statement
// that the exclusions have been decided.
func flagWasSet(cmd *cobra.Command, name string) bool {
	return cmd.Flags().Changed(name)
}
