package download

import (
	"context"
	"testing"
)

// stubRetirement reports specific video IDs as deliberately removed.
type stubRetirement struct {
	retired map[string]bool
	queried []string
	err     error
}

func (s *stubRetirement) IsRetired(_ context.Context, sourceID string) (bool, error) {
	s.queried = append(s.queried, sourceID)

	if s.err != nil {
		return false, s.err
	}

	return s.retired[sourceID], nil
}

// stubFetcher serves a fixed playlist, so the filter can be tested without
// yt-dlp.
type stubFetcher struct {
	meta PlaylistMetadata
}

func (s stubFetcher) Fetch(context.Context, string) (PlaylistMetadata, error) {
	return s.meta, nil
}

func TestPlaylistChannelSelectionMatchesOnID(t *testing.T) {
	// The channel renamed between saving the selection and polling. The
	// stored ID still identifies it; the name no longer matches.
	fetcher := stubFetcher{meta: PlaylistMetadata{
		Channels: []ChannelRef{{ID: "UCsame", Name: "New Name"}},
		Videos:   []VideoMetadata{{ID: "vid1", ChannelID: "UCsame", Channel: "New Name"}},
	}}

	svc := newSourceTestService(fetcher)

	selected := []ChannelRef{{ID: "UCsame", Name: "Old Name"}}

	var added []string
	err := svc.processPlaylist(t.Context(), "u", fixedSelector(selected), func(id string) error {
		added = append(added, id)

		return nil
	})
	if err != nil {
		t.Fatalf("processPlaylist: %v", err)
	}

	if len(added) != 1 || added[0] != "vid1" {
		t.Errorf("added %v, want the video to survive a channel rename", added)
	}
}

// TestPlaylistSelectionSkipsUnselectedChannels: an ID-only match must not
// become a match-all.
func TestPlaylistSelectionSkipsUnselectedChannels(t *testing.T) {
	fetcher := stubFetcher{meta: PlaylistMetadata{
		Channels: []ChannelRef{
			{ID: "UCa", Name: "Alpha"},
			{ID: "UCb", Name: "Beta"},
		},
		Videos: []VideoMetadata{
			{ID: "a1", ChannelID: "UCa", Channel: "Alpha"},
			{ID: "b1", ChannelID: "UCb", Channel: "Beta"},
		},
	}}

	svc := newSourceTestService(fetcher)

	var added []string
	err := svc.processPlaylist(t.Context(), "u", fixedSelector([]ChannelRef{{ID: "UCa", Name: "Alpha"}}), func(id string) error {
		added = append(added, id)

		return nil
	})
	if err != nil {
		t.Fatalf("processPlaylist: %v", err)
	}

	if len(added) != 1 || added[0] != "a1" {
		t.Errorf("added %v, want only the selected channel's video", added)
	}
}

// TestPlaylistSelectionFallsBackToName covers a listing that reports no
// channel ID at all, such as a channel's own uploads list.
func TestPlaylistSelectionFallsBackToName(t *testing.T) {
	fetcher := stubFetcher{meta: PlaylistMetadata{
		Channels: []ChannelRef{{Name: "Alpha"}},
		Videos: []VideoMetadata{
			{ID: "a1", Channel: "Alpha"},
			{ID: "b1", Channel: "Beta"},
		},
	}}

	svc := newSourceTestService(fetcher)

	var added []string
	err := svc.processPlaylist(t.Context(), "u", fixedSelector([]ChannelRef{{Name: "Alpha"}}), func(id string) error {
		added = append(added, id)

		return nil
	})
	if err != nil {
		t.Fatalf("processPlaylist: %v", err)
	}

	if len(added) != 1 || added[0] != "a1" {
		t.Errorf("added %v, want name matching when no ID is reported", added)
	}
}

func TestEnqueueSkipsRetiredVideos(t *testing.T) {
	retirement := &stubRetirement{retired: map[string]bool{"bRp8fX44icE": true}}

	svc := newSourceTestService(stubFetcher{})

	res, err := svc.Enqueue(t.Context(), EnqueueParams{
		URLs:       []string{"https://www.youtube.com/watch?v=bRp8fX44icE"},
		Retirement: retirement,
	})
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	if res.Enqueued != 0 {
		t.Errorf("enqueued %d, want 0 for a deleted video", res.Enqueued)
	}

	if res.Skipped != 1 {
		t.Errorf("skipped %d, want 1", res.Skipped)
	}

	if len(retirement.queried) != 1 {
		t.Errorf("asked about retirement %d times, want 1", len(retirement.queried))
	}
}

func TestEnqueueStillTakesVideosThatWereNeverDeleted(t *testing.T) {
	retirement := &stubRetirement{retired: map[string]bool{}}

	svc := newSourceTestService(stubFetcher{})

	res, err := svc.Enqueue(t.Context(), EnqueueParams{
		URLs:       []string{"https://www.youtube.com/watch?v=bRp8fX44icE"},
		Retirement: retirement,
	})
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	if res.Enqueued != 1 {
		t.Errorf("enqueued %d, want 1", res.Enqueued)
	}
}

// TestEnqueuePropagatesRetirementFailure: an unreadable journal must not be
// read as "not deleted", which would silently re-download what the user
// removed.
func TestEnqueuePropagatesRetirementFailure(t *testing.T) {
	retirement := &stubRetirement{err: context.DeadlineExceeded}

	svc := newSourceTestService(stubFetcher{})

	_, err := svc.Enqueue(t.Context(), EnqueueParams{
		URLs:       []string{"https://www.youtube.com/watch?v=bRp8fX44icE"},
		Retirement: retirement,
	})
	if err == nil {
		t.Error("Enqueue with a failing retirement check returned no error")
	}
}

// fixedSelector returns a fixed selection, standing in for the stored one.
type fixedSelector []ChannelRef

func (f fixedSelector) SelectChannels(context.Context, []ChannelRef) ([]ChannelRef, error) {
	return f, nil
}

// newSourceTestService builds a service with the shared fakes and a stub
// playlist fetcher, so channel filtering is tested without yt-dlp.
func newSourceTestService(fetcher PlaylistFetcher) *Service {
	return NewService(ServiceConfig{
		QueueRepo:       &fakeQueueRepo{},
		TrackChecker:    &fakeTrackChecker{},
		DirReader:       &fakeDirReader{},
		Transactor:      fakeTransactor{},
		StagingDirs:     &fakeStagingDirs{dir: "/tmp/staging"},
		PlaylistFetcher: fetcher,
	})
}
