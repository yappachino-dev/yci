package library

import (
	"context"
	"errors"
	"testing"
)

// TestDeleteJournalsTheRemoval: this is the whole point of the journal. Once
// the row is gone, nothing else remembers the video, so a watched playlist
// that still offers it would download it again.
func TestDeleteJournalsTheRemoval(t *testing.T) {
	env := newLibraryEnv(t)
	ctx := t.Context()

	env.mustPutTrack(t, "t1", "Song", "dir/song.opus")

	if err := env.repos.tracks.Create(ctx, Track{
		ID:           "t2",
		Title:        "Other",
		RelativePath: "dir/other.opus",
		SourceID:     "bRp8fX44icE",
	}); err != nil {
		t.Fatalf("seed track: %v", err)
	}

	if err := env.tracks.Delete(ctx, "t2"); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	retired, err := env.trackJournal.IsRetired(ctx, "bRp8fX44icE")
	if err != nil {
		t.Fatalf("IsRetired: %v", err)
	}

	if !retired {
		t.Error("the deleted video is not recorded as retired, so a poll would download it again")
	}
}

// TestJournalIgnoresTracksWithNoVideo: a hand-staged file has no YouTube ID
// behind it, so it can never match a source. Journalling a NULL would make
// every hand-staged deletion look like a retired video.
func TestJournalIgnoresTracksWithNoVideo(t *testing.T) {
	env := newLibraryEnv(t)

	env.mustPutTrack(t, "t1", "Song", "dir/song.opus")

	if err := env.tracks.Delete(t.Context(), "t1"); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	retired, err := env.trackJournal.IsRetired(t.Context(), "")
	if err != nil {
		t.Fatalf("IsRetired on an empty id: %v", err)
	}

	if retired {
		t.Error("a track with no video id was recorded as retired")
	}
}

func TestUndeletedVideoIsNotRetired(t *testing.T) {
	env := newLibraryEnv(t)

	retired, err := env.trackJournal.IsRetired(t.Context(), "bRp8fX44icE")
	if err != nil {
		t.Fatalf("IsRetired: %v", err)
	}

	if retired {
		t.Error("a video that was never deleted reported as retired")
	}
}

// TestDeleteIsAtomicWithTheJournal: a removal recorded for a track that
// survived would suppress a download the user never cancelled, so the two
// writes have to share a transaction.
func TestDeleteIsAtomicWithTheJournal(t *testing.T) {
	env := newLibraryEnv(t)
	ctx := t.Context()

	if err := env.repos.tracks.Create(ctx, Track{
		ID:           "t1",
		Title:        "Song",
		RelativePath: "dir/song.opus",
		SourceID:     "bRp8fX44icE",
	}); err != nil {
		t.Fatalf("seed track: %v", err)
	}

	// Replacing the journal with one that refuses to write is the smallest
	// way to fail the second statement of the transaction.
	svc := NewTrackService(TrackServiceConfig{
		TrackRepo:      env.repos.tracks,
		FolderResolver: env.folders,
		DiskMover:      env.journaledMover,
		Transactor:     env.txr,
		Paths:          env.paths,
		Journal:        refusingJournal{},
	})

	if err := svc.Delete(ctx, "t1"); err == nil {
		t.Fatal("Delete returned no error when the journal refused to record it")
	}

	if _, err := env.repos.tracks.ByID(ctx, "t1"); err != nil {
		t.Errorf("the track was deleted even though the journal write failed: %v", err)
	}

	retired, err := env.trackJournal.IsRetired(ctx, "bRp8fX44icE")
	if err != nil {
		t.Fatalf("IsRetired: %v", err)
	}

	if retired {
		t.Error("a failed delete still recorded the video as retired")
	}
}

type refusingJournal struct{}

func (refusingJournal) Record(context.Context, TrackID, VideoID, TrackEventOp) error {
	return errors.New("journal unavailable")
}

func (refusingJournal) IsRetired(context.Context, string) (bool, error) {
	return false, nil
}
