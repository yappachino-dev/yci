package cli

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/yappachino-dev/yci/internal/app"
	"github.com/yappachino-dev/yci/internal/library"
)

func TestParseTokenSelection(t *testing.T) {
	tokens := []string{"Cry", "Baby", "星街すいせい", "Cover"}

	tests := []struct {
		name  string
		input string
		want  []string
	}{
		{"single number", "3", []string{"星街すいせい"}},
		{"several numbers", "1,3", []string{"Cry", "星街すいせい"}},
		{"spaces are tolerated", " 1 , 3 ", []string{"Cry", "星街すいせい"}},
		{"range", "1-2", []string{"Cry", "Baby"}},
		{"range with a number", "1-2,4", []string{"Cry", "Baby", "Cover"}},
		{"out of order keeps title order", "4,1", []string{"Cry", "Cover"}},
		{"repeats collapse", "1,1,1", []string{"Cry"}},
		{"trailing comma is ignored", "1,", []string{"Cry"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseTokenSelection(tt.input, tokens)
			if err != nil {
				t.Fatalf("parseTokenSelection(%q): %v", tt.input, err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("parseTokenSelection(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestParseTokenSelectionRejects(t *testing.T) {
	tokens := []string{"Cry", "Baby", "星街すいせい", "Cover"}

	tests := []struct {
		name  string
		input string
	}{
		{"zero is not a token", "0"},
		{"past the end", "5"},
		{"backwards range", "3-1"},
		{"not a number", "abc"},
		{"not a number in a range", "1-x"},
		{"nothing selected", ",,,"},
		{"empty input", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseTokenSelection(tt.input, tokens)
			if err == nil {
				t.Fatalf("parseTokenSelection(%q) = %q, want an error", tt.input, got)
			}
		})
	}
}

func TestSuggestedIndicesArePromptPositions(t *testing.T) {
	all := []string{"Cry", "Baby", "星街すいせい", "Cover"}

	tests := []struct {
		name      string
		suggested []string
		want      []int
	}{
		{"one suggestion", []string{"星街すいせい"}, []int{3}},
		{"two suggestions keep title order", []string{"Baby", "Cry"}, []int{1, 2}},
		{"suggestion not in the list", []string{"Ghost"}, nil},
		{"no suggestion", nil, nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := suggestedIndices(tt.suggested, all)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("suggestedIndices(%q) = %v, want %v", tt.suggested, got, tt.want)
			}
		})
	}
}

// TestAcceptingTheSuggestionParses is the regression test for a bug that only
// showed up in the terminal: the default answer was built from token *text*
// and fed to a parser expecting *positions*, so accepting the suggestion
// errored with `星街すいせい is not a token number`.
//
// It walks the same path the wizard takes on empty input, because the earlier
// test asserted the helper's return type rather than the behaviour the user
// depends on.
func TestAcceptingTheSuggestionParses(t *testing.T) {
	tokens := []string{"Cry", "Baby", "星街すいせい", "Cover"}

	for _, suggested := range [][]string{
		{"星街すいせい"},
		{"Cry", "星街すいせい"},
		{"Cry", "Baby", "星街すいせい", "Cover"},
	} {
		answer := formatIndices(suggestedIndices(suggested, tokens))

		selected, err := parseTokenSelection(answer, tokens)
		if err != nil {
			t.Fatalf("accepting suggestion %v produced answer %q, which does not parse: %v", suggested, answer, err)
		}
		if !reflect.DeepEqual(selected, suggested) {
			t.Errorf("accepting %v selected %q, want %q", suggested, selected, suggested)
		}
	}
}

func TestFormatIndicesIsEmptyWhenNoSuggestion(t *testing.T) {
	if got := formatIndices(suggestedIndices(nil, []string{"a"})); got != "" {
		t.Errorf("formatIndices = %q, want empty so the caller can detect it", got)
	}
	if got := formatIndices([]int{1, 3}); got != "1,3" {
		t.Errorf("formatIndices([1 3]) = %q, want \"1,3\"", got)
	}
}

// seedCmd builds the create command with its flags and a container in
// context, so the seeding path runs exactly as it does from the shell.
func seedCmd(t *testing.T, c *app.Container) (*cobra.Command, *strings.Builder) {
	return seedCmdWith(t, c, false)
}

func seedCmdWith(t *testing.T, c *app.Container, yes bool) (*cobra.Command, *strings.Builder) {
	t.Helper()

	var out strings.Builder
	cmd := &cobra.Command{}
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetContext(WithContainer(context.Background(), c))

	cmd.Flags().String(flagFromTrack, "", "")
	cmd.Flags().String(flagFragment, "", "")
	cmd.Flags().Bool(flagAcceptAll, false, "")
	cmd.Flags().Bool(flagYes, yes, "")

	return cmd, &out
}

func seedTracks(t *testing.T, c *app.Container, titles ...string) []library.TrackID {
	t.Helper()
	ctx := context.Background()

	folder, err := c.FolderService.Create(ctx, "", library.FolderID(library.RootID), library.FolderName("VT"))
	if err != nil {
		t.Fatalf("create folder: %v", err)
	}
	artist, err := c.ArtistService.Create(ctx, "", folder.ID, library.ArtistName("Someone"))
	if err != nil {
		t.Fatalf("create artist: %v", err)
	}

	ids := make([]library.TrackID, 0, len(titles))
	for i, title := range titles {
		id := library.TrackID(fmt.Sprintf("track-%02d", i))
		if _, err := c.TrackService.Create(ctx, library.TrackCreateParams{
			ID:           id,
			Title:        title,
			ArtistID:     artist.ID,
			RelativePath: library.TrackPath(fmt.Sprintf("VT/%02d.opus", i)),
			SourceID:     fmt.Sprintf("src%02d", i),
		}); err != nil {
			t.Fatalf("create track %q: %v", title, err)
		}
		ids = append(ids, id)
	}

	return ids
}

func TestSeedPlaylistAcceptAll(t *testing.T) {
	c := newTestContainer(t)
	ids := seedTracks(t, c,
		"Cry Baby / 星街すいせい(Cover)",
		"KING / 星街すいせい(Cover)",
		"KICK BACK / 鷹嶺ルイ(cover)",
		"BRAIN - Mori Calliope (cover)",
	)

	cmd, out := seedCmdWith(t, c, true)
	if err := seedPlaylist(cmd, "Suisei", string(ids[0]), "", true, library.MatchAll); err != nil {
		t.Fatalf("seedPlaylist: %v", err)
	}

	entries, err := c.PlaylistService.Entries(context.Background(), "Suisei")
	if err != nil {
		t.Fatal(err)
	}

	// The seed and its one sibling, and nothing from another performer.
	if len(entries) != 2 {
		t.Errorf("playlist has %d tracks, want 2:\n%s", len(entries), out.String())
	}
	for _, e := range entries {
		if !strings.Contains(e.Track.Title, "星街すいせい") {
			t.Errorf("unexpected track in playlist: %q", e.Track.Title)
		}
	}
	if !strings.Contains(out.String(), "Fragment: 星街すいせい") {
		t.Errorf("output does not state the fragment:\n%s", out.String())
	}
}

func TestSeedPlaylistExplicitFragment(t *testing.T) {
	c := newTestContainer(t)
	ids := seedTracks(t, c,
		"BRAIN - Mori Calliope (cover)",
		"【MV】Orpheus - Mori Calliope",
		"【#RequiemforLove3D】 Bocca della Verità feat @MoriCalliope",
		"GETCHA! / calliope × suisei(cover)",
		"KICK BACK / 鷹嶺ルイ(cover)",
	)

	cmd, out := seedCmdWith(t, c, true)
	if err := seedPlaylist(cmd, "Calliope", string(ids[0]), "Mori Calliope", true, library.MatchAll); err != nil {
		t.Fatalf("seedPlaylist: %v", err)
	}

	entries, err := c.PlaylistService.Entries(context.Background(), "Calliope")
	if err != nil {
		t.Fatal(err)
	}

	// The run-together "@MoriCalliope" spelling is only reachable because
	// matching is a token set rather than a joined substring.
	if len(entries) != 3 {
		t.Errorf("playlist has %d tracks, want 3:\n%s", len(entries), out.String())
	}
}

func TestSeedPlaylistNoMatchIsRefused(t *testing.T) {
	c := newTestContainer(t)
	ids := seedTracks(t, c, "Espresso", "From The Start")

	cmd, _ := seedCmd(t, c)
	if err := seedPlaylist(cmd, "Nothing", string(ids[0]), "zzzznotpresent", true, library.MatchAll); err == nil {
		t.Fatal("seedPlaylist accepted a fragment that matches nothing")
	}

	// Nothing may be written when the fragment is unusable.
	all, err := c.PlaylistService.All(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 0 {
		t.Errorf("playlists were created despite no match: %v", all)
	}
}

func TestSeedPlaylistRequiresConfirmationFlag(t *testing.T) {
	c := newTestContainer(t)
	ids := seedTracks(t, c, "Cry Baby / 星街すいせい(Cover)", "KING / 星街すいせい(Cover)")

	// Tests are non-interactive, so without --yes the command must refuse
	// rather than assume approval.
	cmd, _ := seedCmd(t, c)
	if err := seedPlaylist(cmd, "Suisei", string(ids[0]), "", true, library.MatchAll); err == nil {
		t.Fatal("seedPlaylist succeeded without confirmation in a non-interactive session")
	}

	all, err := c.PlaylistService.All(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 0 {
		t.Errorf("a playlist was created despite the refusal: %v", all)
	}
}
