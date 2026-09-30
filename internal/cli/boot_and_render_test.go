package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/yappachino-dev/yci/internal/app"
	"github.com/yappachino-dev/yci/internal/library"
)

// TestPrintReseedPlanStatesEverythingThatMatters.
//
// A reseed diff is the one screen where a user decides whether a recipe still
// means what they meant. Anything the plan fails to state is a decision made
// without information: the mode, which says how wide the selection is; the
// exclusions, which explain why a matching track is absent; and the counts,
// which distinguish "nothing changed" from "everything changed".
func TestPrintReseedPlanStatesEverythingThatMatters(t *testing.T) {
	t.Context()

	t.Run("an unchanged plan says so", func(t *testing.T) {
		var out bytes.Buffer
		cmd := &cobra.Command{}
		cmd.SetOut(&out)

		printReseedPlan(cmd, "P", library.ReseedReport{
			Recipe:    []string{"星街すいせい", "Mori"},
			MatchMode: library.MatchAll,
			SeedTitle: "Cry Baby / 星街すいせい",
			Current:   4,
			Matched:   4,
			Unchanged: 4,
		})

		got := out.String()
		for _, want := range []string{`Playlist "P"`, "Cry Baby", "AND", "Matches 4", "Nothing to change"} {
			if !strings.Contains(got, want) {
				t.Errorf("plan does not mention %q:\n%s", want, got)
			}
		}
	})

	t.Run("an ANY mode is called out", func(t *testing.T) {
		var out bytes.Buffer
		cmd := &cobra.Command{}
		cmd.SetOut(&out)

		// Two tokens, because a single-token recipe renders no operator at
		// all and could not show the joiner being right.
		printReseedPlan(cmd, "P", library.ReseedReport{
			Recipe:    []string{"星街すいせい", "Calliope"},
			MatchMode: library.MatchAny,
			SeedTitle: "seed",
			Matched:   70,
			Current:   1,
		})

		got := out.String()
		// The operator has to follow the mode, or the recipe reads as
		// narrower than it is.
		if !strings.Contains(got, "OR") {
			t.Errorf("an ANY plan does not show OR:\n%s", got)
		}
		if !strings.Contains(got, "ANY") {
			t.Errorf("an ANY plan does not flag the mode:\n%s", got)
		}
	})

	t.Run("exclusions are reported", func(t *testing.T) {
		var out bytes.Buffer
		cmd := &cobra.Command{}
		cmd.SetOut(&out)

		printReseedPlan(cmd, "P", library.ReseedReport{
			Recipe:    []string{"星街すいせい"},
			SeedTitle: "seed",
			Excludes:  []library.TrackID{"a", "b"},
		})

		if !strings.Contains(out.String(), "Excluded: 2") {
			t.Errorf("a plan with exclusions does not report them:\n%s", out.String())
		}
	})

	t.Run("removals are listed before additions", func(t *testing.T) {
		var out bytes.Buffer
		cmd := &cobra.Command{}
		cmd.SetOut(&out)

		printReseedPlan(cmd, "P", library.ReseedReport{
			Recipe:    []string{"x"},
			SeedTitle: "seed",
			Current:   2,
			Matched:   2,
			Changes: []library.ReseedChange{
				{Track: library.Track{Title: "added one"}, Add: true},
				{Track: library.Track{Title: "removed one"}, Add: false},
			},
		})

		got := out.String()
		removeAt := strings.Index(got, "remove")
		addAt := strings.Index(got, "add")
		if removeAt < 0 || addAt < 0 {
			t.Fatalf("plan did not list both changes:\n%s", got)
		}
		// Removals first: a user scanning the list needs to see what is
		// about to disappear before what is about to appear.
		if removeAt > addAt {
			t.Errorf("additions are listed before removals:\n%s", got)
		}
	})
}

// TestReadURLsFromFileSkipsBlanksAndComments pins the parsing rules, and in
// particular that tracking parameters are stripped: the same video reached by
// a share link and a plain link must queue as one download, not two.
func TestReadURLsFromFileSkipsBlanksAndComments(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "urls.txt")

	body := strings.Join([]string{
		"# a comment",
		"",
		"   ",
		"https://www.youtube.com/watch?v=dQw4w9WgXcQ",
		"  https://youtu.be/abcdefghijk  ",
		"https://www.youtube.com/watch?v=abcdefghijk&si=TRACKING&feature=share",
	}, "\n")

	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := readUrlsFromFile(path)
	if err != nil {
		t.Fatalf("readUrlsFromFile: %v", err)
	}

	if len(got) != 3 {
		t.Fatalf("read %d line(s), want 3: %v", len(got), got)
	}
	if got[0] != "https://www.youtube.com/watch?v=dQw4w9WgXcQ" {
		t.Errorf("first entry = %q", got[0])
	}
	// Whitespace is trimmed from every line, or the URL parser sees it.
	if strings.TrimSpace(got[1]) != got[1] {
		t.Errorf("entry is not trimmed: %q", got[1])
	}
	for _, q := range got {
		if strings.Contains(q, "si=") || strings.Contains(q, "feature=") {
			t.Errorf("tracking parameters survived: %q", q)
		}
	}

	if _, err := readUrlsFromFile(filepath.Join(dir, "absent.txt")); err == nil {
		t.Error("reading a missing file returned no error")
	}
}

// TestResolveFlagOrConfigPrefersTheFlag pins the documented precedence for
// these two values: flag, then config, then the built-in default that the
// caller passes as cfgValue.
func TestResolveFlagOrConfigPrefersTheFlag(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.Flags().String(flagDelay, "1:00", "")

	if got := resolveFlagOrConfig(cmd, flagDelay, "5:30"); got != "1:00" {
		t.Errorf("with the flag at its default, got %q, want the config value", got)
	}

	if err := cmd.Flags().Set(flagDelay, "2:00"); err != nil {
		t.Fatal(err)
	}
	if got := resolveFlagOrConfig(cmd, flagDelay, "5:30"); got != "2:00" {
		t.Errorf("with the flag set, got %q, want the flag value", got)
	}

	// Whitespace-only is treated as unset rather than passed on as a value
	// that then fails to parse.
	if err := cmd.Flags().Set(flagDelay, "   "); err != nil {
		t.Fatal(err)
	}
	if got := resolveFlagOrConfig(cmd, flagDelay, "5:30"); got != "5:30" {
		t.Errorf("with a blank flag, got %q, want the config value", got)
	}
}

// TestInitApplicationBootsAndCloses covers the boot path end to end, which is
// what every command depends on and what no test exercised until now.
//
// It is also the path the foreign-key bug lived on: the pragmas are applied
// when the connection is opened, so a test that builds a container any other
// way would not have caught it.
func TestInitApplicationBootsAndCloses(t *testing.T) {
	dir := t.TempDir()
	configDir := filepath.Join(dir, "config")
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		t.Fatal(err)
	}

	profile := "library_dir = \"" + filepath.Join(dir, "library") + "\"\n" +
		"staging_dir = \"" + filepath.Join(dir, "staging") + "\"\n"
	if err := os.WriteFile(filepath.Join(configDir, "config.toml"), []byte(profile), 0o600); err != nil {
		t.Fatal(err)
	}

	t.Setenv("YCI_CONFIG_PATH", filepath.Join(configDir, "config.toml"))
	t.Setenv("YCI_LIBRARY_PATH", filepath.Join(dir, "library"))
	t.Setenv("YCI_STAGING_PATH", filepath.Join(dir, "staging"))

	container, err := initApplication(t.Context(), true, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("initApplication: %v", err)
	}

	if container.DB == nil {
		t.Fatal("container has no database handle")
	}
	for name, svc := range map[string]any{
		"FolderService":   container.FolderService,
		"ArtistService":   container.ArtistService,
		"TrackService":    container.TrackService,
		"PlaylistService": container.PlaylistService,
		"AuditService":    container.AuditService,
	} {
		if svc == nil {
			t.Errorf("Container.%s is nil", name)
		}
	}

	// The migration ran, so the schema is there.
	if _, err := container.FolderService.All(t.Context()); err != nil {
		t.Errorf("reading folders from a freshly migrated database: %v", err)
	}

	if err := container.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}

	// Closing twice must not panic. A caller that closes in a defer and also
	// on the error path is a normal thing to write, and panicking there
	// would replace a useful error with a crash.
	if err := container.Close(); err != nil {
		t.Logf("second Close returned %v", err)
	}

	var nilContainer *app.Container
	if err := nilContainer.Close(); err != nil {
		t.Errorf("Close on a nil container = %v, want nil", err)
	}
}
