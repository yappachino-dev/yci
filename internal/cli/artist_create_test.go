package cli

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/yappachino-dev/yci/internal/library"
)

// resetArtistCreateFlags clears value *and* Changed: pflag never resets
// Changed on Set, so setting a flag back to "" would leave the guard armed.
func resetArtistCreateFlags(t *testing.T) {
	t.Helper()

	clear := func() {
		for _, name := range []string{flagParent, flagFolder} {
			f := artistCreateCmd.Flags().Lookup(name)
			if f == nil {
				continue
			}

			_ = f.Value.Set("")
			f.Changed = false
		}
	}

	t.Cleanup(clear)
}

// TestArtistCreateRejectsParentAndFolder pins the guard the two flags need:
// one mints a folder, the other links an existing one, and guessing which the
// user meant is how library structure gets invented by accident.
func TestArtistCreateRejectsParentAndFolder(t *testing.T) {
	resetArtistCreateFlags(t)

	if err := artistCreateCmd.Flags().Set(flagParent, "hololive"); err != nil {
		t.Fatalf("set --%s: %v", flagParent, err)
	}

	if err := artistCreateCmd.Flags().Set(flagFolder, "vt"); err != nil {
		t.Fatalf("set --%s: %v", flagFolder, err)
	}

	err := artistCreateCmd.PreRunE(artistCreateCmd, nil)
	if err == nil {
		t.Fatal("expected --parent and --folder together to be rejected")
	}

	if !strings.Contains(err.Error(), "mutually exclusive") {
		t.Errorf("error %q does not explain that the flags conflict", err)
	}
}

func TestArtistCreateAcceptsEitherFlagAlone(t *testing.T) {
	resetArtistCreateFlags(t)

	for _, alone := range []string{flagParent, flagFolder} {
		for _, name := range []string{flagParent, flagFolder} {
			f := artistCreateCmd.Flags().Lookup(name)
			_ = f.Value.Set("")
			f.Changed = false
		}

		if err := artistCreateCmd.Flags().Set(alone, "hololive"); err != nil {
			t.Fatalf("set --%s: %v", alone, err)
		}

		if err := artistCreateCmd.PreRunE(artistCreateCmd, nil); err != nil {
			t.Errorf("--%s alone was rejected: %v", alone, err)
		}
	}
}

// TestArtistCreateFlagsRegistered stops a refactor from quietly dropping
// --parent and leaving the feature unreachable.
func TestArtistCreateFlagsRegistered(t *testing.T) {
	for _, name := range []string{flagParent, flagFolder, flagName} {
		if artistCreateCmd.Flags().Lookup(name) == nil {
			t.Errorf("artist create is missing the --%s flag", name)
		}
	}
}

// TestArtistCreateWithParentMintsFolder is the wiring test: it runs the real
// RunE, so a --parent that is parsed but never acted on fails here.
func TestArtistCreateWithParentMintsFolder(t *testing.T) {
	c := newTestContainer(t)
	ctx := t.Context()

	parent, err := c.FolderService.Create(ctx, "hololive", "", library.FolderName("hololive"))
	if err != nil {
		t.Fatalf("create parent: %v", err)
	}

	var out strings.Builder

	cmd := &cobra.Command{}
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetContext(WithContainer(ctx, c))
	cmd.Flags().String(flagName, "", "")
	cmd.Flags().String(flagID, "", "")
	cmd.Flags().String(flagFolder, "", "")
	cmd.Flags().String(flagParent, "", "")

	if err := cmd.Flags().Parse([]string{"--name", "Cecilia Immergreen", "--parent", string(parent.ID)}); err != nil {
		t.Fatalf("parse: %v", err)
	}

	if err := artistCreateCmd.RunE(cmd, nil); err != nil {
		t.Fatalf("artist create --parent: %v", err)
	}

	artist, err := c.ArtistService.ByID(ctx, "cecilia-immergreen")
	if err != nil {
		t.Fatalf("artist was not created: %v", err)
	}

	if artist.FolderID != "cecilia-immergreen" {
		t.Errorf("artist folder = %q, want a folder named after it", artist.FolderID)
	}

	leaf, err := c.FolderService.ByID(ctx, artist.FolderID)
	if err != nil {
		t.Fatalf("leaf folder was not created: %v", err)
	}

	if leaf.ParentID != parent.ID {
		t.Errorf("leaf parent = %q, want %q", leaf.ParentID, parent.ID)
	}
}
