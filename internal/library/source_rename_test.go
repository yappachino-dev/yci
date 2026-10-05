package library

import (
	"errors"
	"strings"
	"testing"
)

// TestRenameKeepsTheWatchedPlaylistIntact is the reason rename exists as its
// own verb rather than a flag on update: a relabel must not cost a playlist
// its url or its channel selection. update would refetch and re-prompt to
// achieve the same thing.
func TestRenameKeepsTheWatchedPlaylistIntact(t *testing.T) {
	svc := NewSourceService(NewSQLiteSourceRepository(newLibraryDB(t)))

	channels := []SourceChannel{{ID: "UCa", Name: "Channel A"}, {ID: "UCb", Name: "Channel B"}}
	const url = "https://youtube.com/playlist?list=PL1"

	if err := svc.Add(t.Context(), "before", url, channels); err != nil {
		t.Fatalf("Add: %v", err)
	}

	if err := svc.Rename(t.Context(), "before", "after"); err != nil {
		t.Fatalf("Rename: %v", err)
	}

	got, err := svc.ByName(t.Context(), "after")
	if err != nil {
		t.Fatalf("ByName after rename: %v", err)
	}

	if got.URL != url {
		t.Errorf("url = %q, want %q", got.URL, url)
	}
	if len(got.Channels) != len(channels) {
		t.Fatalf("kept %d channels, want %d", len(got.Channels), len(channels))
	}
	for i, ch := range channels {
		if got.Channels[i] != ch {
			t.Errorf("channel %d = %+v, want %+v", i, got.Channels[i], ch)
		}
	}

	if _, err := svc.ByName(t.Context(), "before"); !errors.Is(err, ErrSourceNotFound) {
		t.Errorf("the old name still resolves: %v", err)
	}
}

func TestRenameRejectsNamesItCannotStore(t *testing.T) {
	svc := NewSourceService(NewSQLiteSourceRepository(newLibraryDB(t)))

	if err := svc.Add(t.Context(), "a", "https://example.com/1",
		[]SourceChannel{{ID: "UCa", Name: "A"}}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if err := svc.Add(t.Context(), "b", "https://example.com/2",
		[]SourceChannel{{ID: "UCb", Name: "B"}}); err != nil {
		t.Fatalf("Add second: %v", err)
	}

	t.Run("empty", func(t *testing.T) {
		if err := svc.Rename(t.Context(), "a", "   "); err == nil {
			t.Error("a blank name was accepted")
		}
	})

	t.Run("already taken", func(t *testing.T) {
		// Colliding would silently merge two watched playlists, so it has to
		// fail rather than overwrite.
		if err := svc.Rename(t.Context(), "a", "b"); !errors.Is(err, ErrSourceExists) {
			t.Errorf("colliding rename = %v, want ErrSourceExists", err)
		}
		if _, err := svc.ByName(t.Context(), "b"); err != nil {
			t.Fatalf("the surviving source is unreadable: %v", err)
		}
	})

	t.Run("unknown source", func(t *testing.T) {
		err := svc.Rename(t.Context(), "nope", "whatever")
		if !errors.Is(err, ErrSourceNotFound) {
			t.Errorf("rename of an unknown source = %v, want ErrSourceNotFound", err)
		}
	})
}

func TestRenameWrapsWithTheOldName(t *testing.T) {
	svc := NewSourceService(NewSQLiteSourceRepository(newLibraryDB(t)))

	err := svc.Rename(t.Context(), "gone", "after")
	if !strings.Contains(err.Error(), `"gone"`) {
		t.Errorf("the message does not name the source: %v", err)
	}
}
