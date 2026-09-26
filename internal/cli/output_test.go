package cli

import (
	"context"
	"regexp"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/yappachino-dev/yci/internal/app"
	"github.com/yappachino-dev/yci/internal/download"
	"github.com/yappachino-dev/yci/internal/library"
)

// runList executes a list helper against a real container and returns
// everything it printed.
func runList(t *testing.T, c *app.Container, fn func(context.Context, *cobra.Command) error) string {
	t.Helper()

	var out strings.Builder
	cmd := &cobra.Command{}
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	if err := fn(WithContainer(context.Background(), c), cmd); err != nil {
		t.Fatalf("list returned error: %v", err)
	}

	return out.String()
}

func seedLibrary(t *testing.T, c *app.Container) (library.FolderID, library.ArtistID) {
	t.Helper()
	ctx := context.Background()

	folder, err := c.FolderService.Create(ctx, "", library.FolderID(library.RootID), library.FolderName("VT"))
	if err != nil {
		t.Fatalf("create folder: %v", err)
	}
	artist, err := c.ArtistService.Create(ctx, "", folder.ID, library.ArtistName("Nekomata Okayu"))
	if err != nil {
		t.Fatalf("create artist: %v", err)
	}

	return folder.ID, artist.ID
}

// TestListOutput exercises the rendering the CLI actually produces, against
// a real migrated database. Tree connectors, column headers and the empty
// states are all user-facing text that no other test covers.
func TestListOutput(t *testing.T) {
	t.Run("folders render a tree", func(t *testing.T) {
		c := newTestContainer(t)
		ctx := context.Background()

		vt, err := c.FolderService.Create(ctx, "", library.FolderID(library.RootID), library.FolderName("VT"))
		if err != nil {
			t.Fatalf("create folder: %v", err)
		}
		hololive, err := c.FolderService.Create(ctx, "", vt.ID, library.FolderName("Hololive"))
		if err != nil {
			t.Fatalf("create child folder: %v", err)
		}
		if _, err := c.FolderService.Create(ctx, "", hololive.ID, library.FolderName("EN")); err != nil {
			t.Fatalf("create grandchild folder: %v", err)
		}

		got := runList(t, c, listFolders)

		// The root row is seeded and must not appear as a tree node.
		if strings.Contains(got, "├─ root") {
			t.Errorf("output includes the root row:\n%s", got)
		}
		if !strings.Contains(got, "VT") || !strings.Contains(got, "Hololive") || !strings.Contains(got, "EN") {
			t.Errorf("output missing a folder:\n%s", got)
		}
		if !strings.Contains(got, "└─ ") {
			t.Errorf("output has no leaf connector:\n%s", got)
		}
	})

	t.Run("empty libraries explain how to fix it", func(t *testing.T) {
		c := newTestContainer(t)

		for name, fn := range map[string]func(context.Context, *cobra.Command) error{
			"folders":  listFolders,
			"artists":  listArtists,
			"channels": listChannels,
		} {
			got := runList(t, c, fn)
			if !strings.Contains(got, "No ") {
				t.Errorf("%s empty state missing a message:\n%s", name, got)
			}
			// Every hint must point somewhere that exists.
			if !strings.Contains(got, "yci ") {
				t.Errorf("%s empty state has no next-step hint:\n%s", name, got)
			}
		}
	})

	t.Run("artists show their folder", func(t *testing.T) {
		c := newTestContainer(t)
		_, artistID := seedLibrary(t, c)

		got := runList(t, c, listArtists)

		if !strings.Contains(got, "Nekomata Okayu") {
			t.Errorf("artist missing from output:\n%s", got)
		}
		if !strings.Contains(got, "FOLDER_ID") {
			t.Errorf("missing FOLDER_ID column:\n%s", got)
		}
		if !strings.Contains(got, string(artistID)) {
			t.Errorf("artist ID missing from output:\n%s", got)
		}
	})

	t.Run("tracks truncate long IDs", func(t *testing.T) {
		c := newTestContainer(t)
		ctx := context.Background()
		_, artistID := seedLibrary(t, c)

		longID := "5ead5f5e-d9e3-4bca-b300-600bab493525"
		if _, err := c.TrackService.Create(ctx, library.TrackCreateParams{
			ID:           library.TrackID(longID),
			Title:        "少女A / 猫又おかゆ",
			ArtistID:     artistID,
			RelativePath: "VT/少女A.opus",
			SourceID:     "bRp8fX44icE",
		}); err != nil {
			t.Fatalf("create track: %v", err)
		}

		got := runList(t, c, listTracks)

		if strings.Contains(got, longID) {
			t.Errorf("long ID not truncated:\n%s", got)
		}
		if !strings.Contains(got, "5ead5f5e") {
			t.Errorf("expected an 8-character ID prefix:\n%s", got)
		}
		if !strings.Contains(got, "少女A / 猫又おかゆ") {
			t.Errorf("title missing from output:\n%s", got)
		}
	})

	t.Run("queue reports the failure reason", func(t *testing.T) {
		c := newTestContainer(t)
		ctx := context.Background()
		_, artistID := seedLibrary(t, c)

		if _, err := c.TrackService.Create(ctx, library.TrackCreateParams{
			ID:           "t1",
			Title:        "Song",
			ArtistID:     artistID,
			RelativePath: "VT/song.opus",
			SourceID:     "aaaaaaaaaaa",
		}); err != nil {
			t.Fatalf("create track: %v", err)
		}
		if _, err := c.DownloadService.Enqueue(ctx, download.EnqueueParams{URLs: []string{"https://youtu.be/utpwwJsCYXY"}}); err != nil {
			t.Fatalf("enqueue: %v", err)
		}

		got := runList(t, c, listQueue)

		if !strings.Contains(got, "utpwwJsCYXY") {
			t.Errorf("queued source ID missing:\n%s", got)
		}
		if !strings.Contains(got, "STATUS") {
			t.Errorf("missing STATUS column:\n%s", got)
		}
	})
}

// TestCommandHintsResolve is the guard against the class of bug the
// noun-first migration produced twice: a user-facing string naming a command
// that no longer exists. Nothing else in the suite can catch that, because
// the strings live inside RunE closures and never reach a test.
//
// Every `... yci <command> ...` fragment in a Go string literal must name a
// command in the real tree.
func TestCommandHintsResolve(t *testing.T) {
	// Cobra installs help and completion lazily during Execute, so a tree
	// inspected before then is missing commands the user does see.
	rootCmd.InitDefaultHelpCmd()
	rootCmd.InitDefaultCompletionCmd()

	hintPattern := regexp.MustCompile("`([^`]*)`|\"([^\"]*)\"")

	for _, ref := range collectCommandHints(t, hintPattern) {
		t.Run(ref, func(t *testing.T) {
			if _, _, err := rootCmd.Find(strings.Fields(ref)); err != nil {
				t.Errorf("hint names %q, which is not a command: %v", ref, err)
			}
		})
	}
}

func collectCommandHints(t *testing.T, hintPattern *regexp.Regexp) []string {
	t.Helper()

	sources, err := packageSources(".")
	if err != nil {
		t.Fatalf("read package sources: %v", err)
	}

	seen := map[string]bool{}
	var hints []string

	for _, src := range sources {
		for _, line := range strings.Split(src, "\n") {
			if i := strings.Index(line, "//"); i >= 0 {
				line = line[:i]
			}

			for _, m := range hintPattern.FindAllStringSubmatch(line, -1) {
				literal := m[1] + m[2]
				idx := strings.Index(literal, "yci ")
				if idx < 0 {
					continue
				}

				// The first token after "yci" is the top-level command;
				// placeholders like <name> end the search.
				rest := literal[idx+len("yci "):]
				name, _, _ := strings.Cut(rest, " ")
				name = strings.Trim(name, "`\"',.!)")
				if name == "" || strings.HasPrefix(name, "<") || seen[name] {
					continue
				}

				seen[name] = true
				hints = append(hints, name)
			}
		}
	}

	if len(hints) == 0 {
		t.Fatal("no command hints found; the scan is broken, not the code")
	}

	return hints
}
