package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/yappachino-dev/yci/internal/app"
	"github.com/yappachino-dev/yci/internal/library"
)

// cmdWithOutput builds a bare command wired to the container, with a captured
// output buffer, the way the shell wires a real one. c may be nil for
// commands that never read it, such as the console reporter.
func cmdWithOutput(t *testing.T, c *app.Container) (*cobra.Command, *bytes.Buffer) {
	t.Helper()

	var out bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetContext(WithContainer(context.Background(), c))

	return cmd, &out
}

// seededPlaylist creates a container holding a playlist seeded from tokens.
func seededPlaylist(t *testing.T, name string, titles, tokens []string) (*app.Container, []library.TrackID) {
	t.Helper()

	c := newTestContainer(t)
	ids := seedTracks(t, c, titles...)

	if _, err := c.PlaylistService.CreateFromSeed(t.Context(), name, ids[0], tokens,
		library.MatchAll, nil, ids); err != nil {
		t.Fatalf("seed playlist %q: %v", name, err)
	}

	return c, ids
}

// TestRenamePlaylistFollowsTheExportedFile is the reason a rename reports
// anything at all.
//
// Renaming only the database row would leave a playlist on the phone under a
// name that no future export can update, and nothing would say so.
func TestRenamePlaylistFollowsTheExportedFile(t *testing.T) {
	c, _ := seededPlaylist(t, "Old Name", []string{"a / b", "c / d"}, []string{"b"})

	exported, err := c.PlaylistService.Export(t.Context(), "Old Name")
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	if _, err := os.Stat(exported); err != nil {
		t.Fatalf("export did not produce a file: %v", err)
	}

	cmd, out := cmdWithOutput(t, c)
	if err := renamePlaylist(cmd, c, "Old Name", "New Name"); err != nil {
		t.Fatalf("rename: %v", err)
	}

	if !strings.Contains(out.String(), "followed the rename") {
		t.Errorf("rename did not report moving the export:\n%s", out)
	}
	if _, err := os.Stat(exported); !os.IsNotExist(err) {
		t.Error("the old export still exists after a rename")
	}

	want := filepath.Join(c.Paths.LibraryDir(), library.ExportDir, "New Name.m3u")
	if _, err := os.Stat(want); err != nil {
		t.Errorf("export did not follow the rename to %q: %v", want, err)
	}

	// A no-op rename says so rather than claiming a move.
	cmd, out = cmdWithOutput(t, c)
	if err := renamePlaylist(cmd, c, "New Name", "New Name"); err != nil {
		t.Fatalf("no-op rename: %v", err)
	}
	if !strings.Contains(out.String(), "nothing changed") {
		t.Errorf("a no-op rename did not say so:\n%s", out)
	}
}

func TestRenamePlaylistReportsAnOrphanedExport(t *testing.T) {
	c, _ := seededPlaylist(t, "Stale", []string{"a / b", "c / d"}, []string{"b"})

	exported, err := c.PlaylistService.Export(t.Context(), "Stale")
	if err != nil {
		t.Fatalf("export: %v", err)
	}

	// Rename in the database only, leaving the file behind. This is exactly
	// the state a failed move produces.
	if _, err := c.PlaylistService.Rename(t.Context(), "Stale", "Fresh"); err != nil {
		t.Fatalf("rename: %v", err)
	}

	cmd, out := cmdWithOutput(t, c)
	// Renaming "Fresh" to "Newer" will try to move a file that does not
	// exist under "Fresh", which is the orphan case: the previous file is
	// still sitting under "Stale".
	if err := renamePlaylist(cmd, c, "Fresh", "Newer"); err != nil {
		t.Fatalf("rename: %v", err)
	}

	_ = exported

	// The point is that nothing is silently dropped: either the move happened
	// or the leftover is named.
	rendered := out.String()
	if !strings.Contains(rendered, "followed the rename") && !strings.Contains(rendered, "Warning") {
		t.Errorf("rename reported neither a move nor a leftover:\n%s", rendered)
	}
}

// TestEditExclusionsMergeIntoTheRecipe is the behaviour behind
// `playlist edit --exclude/--include`.
//
// A replace semantic would be a trap: naming one track to exclude would
// silently clear the exclusions an earlier wizard session recorded, and the
// user would only find out from a later reseed.
func TestEditExclusionsMergeIntoTheRecipe(t *testing.T) {
	c, ids := seededPlaylist(t, "P",
		[]string{"comet / 星街すいせい", "other / 星街すいせい", "third / 星街すいせい", "x / y"},
		[]string{"星街すいせい"})

	if _, err := editExclusions(t.Context(), c, "P", []string{string(ids[1])}, nil); err != nil {
		t.Fatalf("exclude: %v", err)
	}

	p, err := c.PlaylistService.ByName(t.Context(), "P")
	if err != nil {
		t.Fatal(err)
	}
	if len(p.FragmentExcludes) != 1 || p.FragmentExcludes[0] != ids[1] {
		t.Fatalf("after one exclude, list = %v", p.FragmentExcludes)
	}

	// Excluding a second track must not clear the first.
	if _, err := editExclusions(t.Context(), c, "P", []string{string(ids[2])}, nil); err != nil {
		t.Fatalf("exclude again: %v", err)
	}

	p, err = c.PlaylistService.ByName(t.Context(), "P")
	if err != nil {
		t.Fatal(err)
	}
	if len(p.FragmentExcludes) != 2 {
		t.Errorf("exclusions = %v, want both ids to survive", p.FragmentExcludes)
	}

	// Including one back out leaves the other alone.
	if _, err := editExclusions(t.Context(), c, "P", nil, []string{string(ids[1])}); err != nil {
		t.Fatalf("include: %v", err)
	}

	p, err = c.PlaylistService.ByName(t.Context(), "P")
	if err != nil {
		t.Fatal(err)
	}
	if len(p.FragmentExcludes) != 1 || p.FragmentExcludes[0] != ids[2] {
		t.Errorf("exclusions = %v, want only %s", p.FragmentExcludes, ids[2])
	}
}

func TestEditExclusionsRefusesUnseededPlaylist(t *testing.T) {
	c := newTestContainer(t)
	if _, err := c.PlaylistService.Create(t.Context(), "Handmade"); err != nil {
		t.Fatal(err)
	}

	_, err := editExclusions(t.Context(), c, "Handmade", []string{"whatever"}, nil)
	if err == nil {
		t.Fatal("expected an error for a playlist with no recipe")
	}
	if !strings.Contains(err.Error(), "not created from a seed track") {
		t.Errorf("error = %v, want a no-recipe message", err)
	}
}

// TestResolveExclusionsCoversItsEdges pins the resolution rules, including the
// decision to fail loudly on an unknown prefix: a mistyped ID that silently
// excluded nothing would leave a user believing a false positive had been
// dealt with.
func TestResolveExclusionsCoversItsEdges(t *testing.T) {
	c := newTestContainer(t)
	ids := seedTracks(t, c, "a / b", "c / d")

	got, err := resolveExclusions(t.Context(), c, []string{string(ids[0])})
	if err != nil {
		t.Fatalf("resolving a known id: %v", err)
	}
	if len(got) != 1 || got[0] != ids[0] {
		t.Errorf("resolved %v, want [%s]", got, ids[0])
	}

	got, err = resolveExclusions(t.Context(), c, []string{string(ids[0]), string(ids[0])})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Errorf("a duplicate resolved to %v, want one entry", got)
	}

	got, err = resolveExclusions(t.Context(), c, []string{"", "  "})
	if err != nil {
		t.Fatalf("blank values: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("blank values resolved to %v", got)
	}

	if _, err := resolveExclusions(t.Context(), c, []string{"nope-nope-nope"}); err == nil {
		t.Error("an unresolvable --exclude was accepted silently")
	}
}

// TestCompletionsWithoutAContainerDoNotPanic covers the path shell completion
// actually takes: it runs without the boot sequence, so no container exists.
// Panicking there breaks tab completion in a user's shell.
func TestCompletionsWithoutAContainerDoNotPanic(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())

	// These read from the database, so with no container they must offer
	// nothing. configKeyCompletion is deliberately absent: the key set is
	// static and needs no boot, which is covered separately below.
	completions := map[string]func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective){
		"artist":          artistIDCompletion,
		"folder":          folderIDCompletion,
		"track":           trackIDCompletion,
		"channel":         channelNameCompletion,
		"playlist":        playlistNameCompletion,
		"playlistOrTrack": playlistOrTrackCompletion,
		"source":          sourceNameCompletion,
		"queue":           queueIDCompletion,
	}

	for name, fn := range completions {
		got, directive := fn(cmd, nil, "")

		if got != nil {
			t.Errorf("%s completion returned %v with no container", name, got)
		}
		if directive != cobra.ShellCompDirectiveNoFileComp {
			t.Errorf("%s directive = %v, want NoFileComp", name, directive)
		}
	}
}

// TestConfigKeyCompletionNeedsNoContainer records why config completion is a
// separate case: the key registry is static, so `config set <TAB>` has to work
// even on the paths that deliberately skip database boot.
func TestConfigKeyCompletionNeedsNoContainer(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())

	got, directive := configKeyCompletion(cmd, nil, "")
	if directive != cobra.ShellCompDirectiveNoFileComp {
		t.Errorf("directive = %v, want NoFileComp", directive)
	}
	if len(got) == 0 {
		t.Fatal("config key completion returned nothing without a container")
	}
	for _, want := range []string{"library_dir", "staging_dir"} {
		if !slicesContains(got, want) {
			t.Errorf("completion list is missing %q: %v", want, got)
		}
	}
}

// slicesContains reports membership, which is all the test needs and avoids
// importing slices for one call.
func slicesContains(haystack []string, needle string) bool {
	for _, h := range haystack {
		if h == needle {
			return true
		}
	}

	return false
}

// TestTrackIDCompletionNarrowsByPrefix is the usefulness of completion: it
// should narrow as the user types rather than offering everything.
func TestTrackIDCompletionNarrowsByPrefix(t *testing.T) {
	c := newTestContainer(t)
	seedTracks(t, c, "a / b", "c / d")

	cmd := &cobra.Command{}
	cmd.SetContext(WithContainer(context.Background(), c))

	got, directive := trackIDCompletion(cmd, nil, "track-0")
	if directive != cobra.ShellCompDirectiveNoFileComp {
		t.Errorf("directive = %v", directive)
	}
	if len(got) == 0 {
		t.Fatal("no completions for a matching prefix")
	}
	for _, comp := range got {
		value, _, _ := strings.Cut(comp, "\t")
		if !strings.HasPrefix(value, "track-0") {
			t.Errorf("completion %q does not match the prefix", comp)
		}
	}

	// A prefix matching nothing returns nothing rather than everything.
	none, _ := trackIDCompletion(cmd, nil, "zzz")
	if len(none) != 0 {
		t.Errorf("prefix zzz returned %v", none)
	}
}
