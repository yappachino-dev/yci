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

	// The flags a list command really has. The list helpers read them
	// unconditionally, so a bare command would fail with "flag accessed but
	// not defined" and quietly stop testing anything.
	cmd.Flags().Bool(flagJSON, false, "")
	cmd.Flags().Int(flagLimit, defaultTrackLimit, "")

	if err := fn(WithContainer(context.Background(), c), cmd); err != nil {
		t.Fatalf("list returned error: %v", err)
	}

	return out.String()
}

// newListCmd builds a list command with the flags it really has.
//
// The list helpers read those flags unconditionally, so a bare command
// fails with "flag accessed but not defined" and stops testing anything.
// Wiring them here keeps that in one place.
func newListCmd(t *testing.T, c *app.Container, args ...string) (*cobra.Command, *strings.Builder) {
	t.Helper()

	var out strings.Builder
	cmd := &cobra.Command{}
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetContext(WithContainer(t.Context(), c))

	cmd.Flags().Bool(flagJSON, false, "")
	cmd.Flags().Int(flagLimit, defaultTrackLimit, "")
	if err := cmd.Flags().Parse(args); err != nil {
		t.Fatalf("parse %v: %v", args, err)
	}

	return cmd, &out
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

	t.Run("channels show only what a command accepts", func(t *testing.T) {
		c := newTestContainer(t)
		_, artistID := seedLibrary(t, c)
		if err := c.ChannelService.Link(t.Context(), artistID, library.ChannelName("Polka Ch. 尾丸ポルカ")); err != nil {
			t.Fatalf("link channel: %v", err)
		}

		got := runList(t, c, listChannels)

		// `channel create` and `channel delete` identify a link by artist
		// plus channel name, so those are the two columns that can be typed
		// back. The integer rowid is not accepted by anything.
		if !strings.Contains(got, "ARTIST_ID") || !strings.Contains(got, "CHANNEL_NAME") {
			t.Errorf("missing the identifying columns:\n%s", got)
		}
		if !strings.Contains(got, "Polka Ch. 尾丸ポルカ") {
			t.Errorf("channel name missing from output:\n%s", got)
		}
		// Checked on the header line, not the whole output: "ARTIST_ID"
		// itself ends in "ID" and would match a naive substring search.
		header := strings.SplitN(got, "\n", 2)[0]
		if strings.HasPrefix(header, "ID") {
			t.Errorf("channel table still leads with a rowid column: %q", header)
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
// maxHintDepth bounds how many command words a hint may contribute. Real
// hints are `yci <group> <verb>`; prose that happens to share a literal with
// a command ("... yci config set creates it on first write") would otherwise
// contribute English words and fail the check for the wrong reason.
//
// The bound is self-verified below: if a command is ever nested deeper, the
// test says so instead of quietly truncating a real hint.
const maxHintDepth = 3

func TestCommandHintsResolve(t *testing.T) {
	// Cobra installs help and completion lazily during Execute, so a tree
	// inspected before then is missing commands the user does see.
	rootCmd.InitDefaultHelpCmd()
	rootCmd.InitDefaultCompletionCmd()

	assertNoCommandIsDeeperThan(t, maxHintDepth)

	for _, ref := range collectCommandHints(t) {
		t.Run(ref, func(t *testing.T) {
			// Find resolves the longest matching prefix and returns the
			// unconsumed words. A hint whose whole path resolves has
			// nothing left over; a stale subcommand leaves its own name
			// behind, which is what this asserts.
			_, remaining, err := rootCmd.Find(strings.Fields(ref))
			if err != nil {
				t.Errorf("hint names %q, which is not a command: %v", ref, err)
				return
			}
			if len(remaining) > 0 {
				t.Errorf("hint names %q, but %q is not a command under it", ref, remaining[0])
			}
		})
	}
}

// assertNoCommandIsDeeperThan keeps maxHintDepth honest. Without it, a
// third-level command added later would have its hints truncated to two
// words and pass, which is the same blind spot this test exists to close.
func assertNoCommandIsDeeperThan(t *testing.T, depth int) {
	t.Helper()

	var walk func(c *cobra.Command, level int)
	walk = func(c *cobra.Command, level int) {
		for _, sub := range c.Commands() {
			if level+1 > depth {
				t.Errorf("command %q is nested deeper than maxHintDepth=%d; raise the constant so its hints stay checkable", sub.CommandPath(), depth)
				continue
			}
			walk(sub, level+1)
		}
	}

	walk(rootCmd, 0)
}

func collectCommandHints(t *testing.T) []string {
	t.Helper()

	sources, err := packageSources(".")
	if err != nil {
		t.Fatalf("read package sources: %v", err)
	}

	// Command words start with a letter: that excludes escape fragments and
	// placeholder arguments like <name> without enumerating them.
	commandWord := regexp.MustCompile(`[A-Za-z][A-Za-z0-9_-]*`)

	seen := map[string]bool{}
	var hints []string

	add := func(ref string) {
		if ref == "" || seen[ref] {
			return
		}
		seen[ref] = true
		hints = append(hints, ref)
	}

	for _, src := range sources {
		for _, line := range strings.Split(src, "\n") {
			if i := strings.Index(line, "//"); i >= 0 {
				line = line[:i]
			}

			// Backticked spans are precise: the user wrote an explicit
			// command, so everything inside is that command. This is what
			// keeps "Run `yci download resume` to process them" from
			// reading "to" as a subcommand.
			for _, m := range regexp.MustCompile("`([^`]*)`").FindAllStringSubmatch(line, -1) {
				if idx := strings.Index(m[1], "yci "); idx >= 0 {
					add(strings.Join(commandWord.FindAllString(m[1][idx+len("yci "):], -1), " "))
				}
			}

			// Plain string literals carry messages with a command embedded
			// ("Re-derive it later with: yci playlist reseed %s"). There the
			// path has to be inferred, so it is cut at the first thing that
			// cannot be part of a command: an escape, a format verb, or a
			// placeholder argument.
			for _, m := range regexp.MustCompile(`"([^"]*)"`).FindAllStringSubmatch(line, -1) {
				idx := strings.Index(m[1], "yci ")
				if idx < 0 {
					continue
				}

				// Cut at anything that cannot be part of a command
				// path: a backtick (a backticked path inside a Go
				// string is handled precisely by the branch above),
				// an escape, a format verb, or a placeholder.
				rest := m[1][idx+len("yci "):]
				for _, cut := range []string{"`", `\`, "%", "<"} {
					if j := strings.Index(rest, cut); j >= 0 {
						rest = rest[:j]
					}
				}

				path := commandWord.FindAllString(rest, -1)
				if len(path) > maxHintDepth {
					path = path[:maxHintDepth]
				}
				add(strings.Join(path, " "))
			}
		}
	}

	if len(hints) == 0 {
		t.Fatal("no command hints found; the scan is broken, not the code")
	}

	return hints
}
