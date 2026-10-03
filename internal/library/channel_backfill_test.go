package library

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

// stubProbe answers "which channel does this video belong to" without a
// network, and records what it was asked so a test can assert on the number
// of requests rather than only the outcome.
type stubProbe struct {
	byVideo map[string]struct{ id, name string }
	err     error
	calls   []string
}

func (s *stubProbe) probe(_ context.Context, videoID string) (string, string, error) {
	s.calls = append(s.calls, videoID)

	if s.err != nil {
		return "", "", s.err
	}
	if r, ok := s.byVideo[videoID]; ok {
		return r.id, r.name, nil
	}

	return "", "", errors.New("no channel")
}

// backfillService builds a ChannelService wired for backfilling, over the real
// migrated database behind newLibraryEnv.
func backfillService(t *testing.T, probe *stubProbe) (*ChannelService, *libraryEnv) {
	t.Helper()

	env := newLibraryEnv(t)
	svc := NewChannelService(
		NewSQLiteChannelRepository(env.queries),
		WithBackfill(env.tracks, probe.probe),
	)

	return svc, env
}

// musicFolder returns the shared folder, creating it once per env. Tests that
// seed several artists would otherwise collide on the second create.
func musicFolder(t *testing.T, env *libraryEnv) FolderID {
	t.Helper()

	if id, ok := env.testFolder["music"]; ok {
		return id
	}

	f := env.mustCreateFolder(t, "Music")
	if env.testFolder == nil {
		env.testFolder = map[string]FolderID{}
	}
	env.testFolder["music"] = f.ID

	return f.ID
}

// artistWithTrack creates an artist holding one track under a known video ID.
func artistWithTrack(t *testing.T, env *libraryEnv, artistID, videoID string) {
	t.Helper()

	folderID := musicFolder(t, env)
	folder, err := env.folders.ByID(t.Context(), folderID)
	if err != nil {
		t.Fatalf("read folder: %v", err)
	}
	folderRef := folder
	artist, err := env.artists.Create(t.Context(), "", folderRef.ID, ArtistName(artistID))
	if err != nil {
		t.Fatal(err)
	}
	if artist.ID != ArtistID(artistID) {
		t.Fatalf("artist id = %q, want %q", artist.ID, artistID)
	}

	if _, err := env.tracks.Create(t.Context(), TrackCreateParams{
		ID:           TrackID("track-" + videoID),
		Title:        "A Song",
		RelativePath: TrackPath(videoID + ".opus"),
		SourceID:     videoID,
		ArtistID:     artist.ID,
	}); err != nil {
		t.Fatalf("create track: %v", err)
	}
}

// TestBackfillAttachesTheChannelID is the ordinary case: a link with no ID is
// given one, looked up from a video the artist published.
func TestBackfillAttachesTheChannelID(t *testing.T) {
	probe := &stubProbe{byVideo: map[string]struct{ id, name string }{
		"aaaaaaaaaaa": {id: "UC8rcEBzJSleTkf_-agPM20g", name: "An Artist"},
	}}
	svc, env := backfillService(t, probe)

	artistWithTrack(t, env, "someone", "aaaaaaaaaaa")
	if err := svc.Link(t.Context(), "someone", "An Artist", ""); err != nil {
		t.Fatalf("link: %v", err)
	}

	results, err := svc.BackfillChannels(t.Context(), BackfillParams{DryRun: false})
	if err != nil {
		t.Fatalf("BackfillChannels: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("results = %d, want 1", len(results))
	}
	if results[0].Outcome != BackfillAttached {
		t.Errorf("outcome = %q, want %q (reason: %s)", results[0].Outcome, BackfillAttached, results[0].Reason)
	}
	if results[0].Channel != "UC8rcEBzJSleTkf_-agPM20g" {
		t.Errorf("channel = %q, want the resolved id", results[0].Channel)
	}

	// Stored, not merely reported.
	all, err := svc.All(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if all[0].ChannelID != "UC8rcEBzJSleTkf_-agPM20g" {
		t.Errorf("stored channel id = %q, want it attached", all[0].ChannelID)
	}
}

// TestBackfillDryRunWritesNothing: a dry run that mutates is worse than no
// dry run, because the output said otherwise.
func TestBackfillDryRunWritesNothing(t *testing.T) {
	probe := &stubProbe{byVideo: map[string]struct{ id, name string }{
		"aaaaaaaaaaa": {id: "UC1", name: "An Artist"},
	}}
	svc, env := backfillService(t, probe)

	artistWithTrack(t, env, "someone", "aaaaaaaaaaa")
	if err := svc.Link(t.Context(), "someone", "An Artist", ""); err != nil {
		t.Fatal(err)
	}

	results, err := svc.BackfillChannels(t.Context(), BackfillParams{DryRun: true})
	if err != nil {
		t.Fatalf("BackfillChannels: %v", err)
	}
	if results[0].Outcome != BackfillAttached {
		t.Errorf("outcome = %q, want the dry run to report what it would do", results[0].Outcome)
	}

	all, err := svc.All(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if all[0].ChannelID != "" {
		t.Errorf("channel id = %q, want a dry run to write nothing", all[0].ChannelID)
	}
}

// TestBackfillReportsARename is the case the feature exists for. Eight of the
// fourteen real links are affected, so it is the common outcome, not an edge.
func TestBackfillReportsARename(t *testing.T) {
	probe := &stubProbe{byVideo: map[string]struct{ id, name string }{
		"aaaaaaaaaaa": {id: "UC1", name: "New Channel Name"},
	}}
	svc, env := backfillService(t, probe)

	artistWithTrack(t, env, "someone", "aaaaaaaaaaa")
	if err := svc.Link(t.Context(), "someone", "Old Channel Name", ""); err != nil {
		t.Fatal(err)
	}

	results, err := svc.BackfillChannels(t.Context(), BackfillParams{DryRun: false})
	if err != nil {
		t.Fatal(err)
	}
	if results[0].Outcome != BackfillRenamed {
		t.Errorf("outcome = %q, want %q (reason: %s)", results[0].Outcome, BackfillRenamed, results[0].Reason)
	}
	if results[0].Found != "New Channel Name" {
		t.Errorf("found name = %q, want what YouTube reports", results[0].Found)
	}
	if results[0].Channel != "UC1" {
		t.Errorf("channel = %q, want UC1 attached despite the rename", results[0].Channel)
	}

	// The stored name is deliberately left alone: the label refresh belongs to
	// import, which knows the link is right because the ID matched.
	all, err := svc.All(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if all[0].Name != "Old Channel Name" {
		t.Errorf("stored name = %q, want the backfill not to rewrite labels", all[0].Name)
	}
}

// TestBackfillRefusesToGuessAcrossSeveralLinks: with more than one link per
// artist there is nothing to resolve against, and picking one would file
// downloads under the wrong artist.
func TestBackfillRefusesToGuessAcrossSeveralLinks(t *testing.T) {
	probe := &stubProbe{byVideo: map[string]struct{ id, name string }{
		"aaaaaaaaaaa": {id: "UC1", name: "Ch. A"},
	}}
	svc, env := backfillService(t, probe)

	artistWithTrack(t, env, "someone", "aaaaaaaaaaa")
	if err := svc.Link(t.Context(), "someone", "Ch. A", ""); err != nil {
		t.Fatal(err)
	}
	if err := svc.Link(t.Context(), "someone", "Ch. B", ""); err != nil {
		t.Fatal(err)
	}

	results, err := svc.BackfillChannels(t.Context(), BackfillParams{DryRun: false})
	if err != nil {
		t.Fatal(err)
	}

	for _, r := range results {
		if r.Outcome != BackfillUnresolved {
			t.Errorf("link %q outcome = %q, want %q", r.Stored, r.Outcome, BackfillUnresolved)
		}
	}

	all, err := svc.All(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range all {
		if c.ChannelID != "" {
			t.Errorf("link %q got channel id %q, want nothing attached", c.Name, c.ChannelID)
		}
	}
}

// TestBackfillSkipsAlreadyKnownLinks: a second run must not re-query YouTube
// for links that already answered.
func TestBackfillSkipsAlreadyKnownLinks(t *testing.T) {
	probe := &stubProbe{byVideo: map[string]struct{ id, name string }{
		"aaaaaaaaaaa": {id: "UC1", name: "An Artist"},
	}}
	svc, env := backfillService(t, probe)

	artistWithTrack(t, env, "someone", "aaaaaaaaaaa")
	if err := svc.Link(t.Context(), "someone", "An Artist", ""); err != nil {
		t.Fatal(err)
	}

	if _, err := svc.BackfillChannels(t.Context(), BackfillParams{DryRun: false}); err != nil {
		t.Fatal(err)
	}
	first := len(probe.calls)

	results, err := svc.BackfillChannels(t.Context(), BackfillParams{DryRun: false})
	if err != nil {
		t.Fatal(err)
	}
	if len(probe.calls) != first {
		t.Errorf("second run made %d new lookups, want 0", len(probe.calls)-first)
	}
	if results[0].Outcome != BackfillAlreadyKnown {
		t.Errorf("outcome = %q, want %q", results[0].Outcome, BackfillAlreadyKnown)
	}
}

// TestBackfillReportsLookupFailureWithoutGuessing: an error is reported per
// link and nothing is attached.
func TestBackfillReportsLookupFailureWithoutGuessing(t *testing.T) {
	probe := &stubProbe{err: errors.New("network down")}
	svc, env := backfillService(t, probe)

	artistWithTrack(t, env, "someone", "aaaaaaaaaaa")
	if err := svc.Link(t.Context(), "someone", "An Artist", ""); err != nil {
		t.Fatal(err)
	}

	results, err := svc.BackfillChannels(t.Context(), BackfillParams{DryRun: false})
	if err != nil {
		t.Fatalf("BackfillChannels: %v", err)
	}
	if results[0].Outcome != BackfillFailed {
		t.Errorf("outcome = %q, want %q", results[0].Outcome, BackfillFailed)
	}
	if results[0].Reason == "" {
		t.Error("failure carries no reason")
	}
}

// TestBackfillProbesOneVideoPerArtist keeps the network cost bounded: 341
// tracks must not become 341 requests.
func TestBackfillProbesOneVideoPerArtist(t *testing.T) {
	probe := &stubProbe{byVideo: map[string]struct{ id, name string }{
		"aaaaaaaaaaa": {id: "UC1", name: "An Artist"},
	}}
	svc, env := backfillService(t, probe)

	artistWithTrack(t, env, "someone", "aaaaaaaaaaa")
	for _, extra := range []string{"bbbbbbbbbbb", "ccccccccccc", "ddddddddddd"} {
		if _, err := env.tracks.Create(t.Context(), TrackCreateParams{
			ID:           TrackID("track-" + extra),
			Title:        "Another",
			RelativePath: TrackPath(extra + ".opus"),
			SourceID:     extra,
			ArtistID:     "someone",
		}); err != nil {
			t.Fatal(err)
		}
	}
	if err := svc.Link(t.Context(), "someone", "An Artist", ""); err != nil {
		t.Fatal(err)
	}

	if _, err := svc.BackfillChannels(t.Context(), BackfillParams{DryRun: false}); err != nil {
		t.Fatal(err)
	}
	if len(probe.calls) != 1 {
		t.Errorf("made %d lookups for one artist, want 1: %v", len(probe.calls), probe.calls)
	}
}

// TestBackfillWithoutConfigurationSaysSo: a service built without the lookup
// must say it cannot work rather than reporting every link as unresolved.
func TestBackfillWithoutConfigurationSaysSo(t *testing.T) {
	env := newLibraryEnv(t)
	svc := NewChannelService(NewSQLiteChannelRepository(env.queries))

	artistWithTrack(t, env, "someone", "aaaaaaaaaaa")
	if err := svc.Link(t.Context(), "someone", "An Artist", ""); err != nil {
		t.Fatal(err)
	}

	results, err := svc.BackfillChannels(t.Context(), BackfillParams{DryRun: false})
	if err != nil {
		t.Fatalf("BackfillChannels: %v", err)
	}
	if results[0].Outcome != BackfillFailed {
		t.Errorf("outcome = %q, want %q", results[0].Outcome, BackfillFailed)
	}
	if results[0].Reason == "" {
		t.Error("failure gives no reason; the operator needs to know to configure it")
	}
}

// recordingBackfillReporter captures the narration a run emits, which is the
// only thing distinguishing a slow run from a hung terminal.
type recordingBackfillReporter struct {
	events []string
}

func (r *recordingBackfillReporter) OnBackfillStart(artists, links int) {
	r.events = append(r.events, fmt.Sprintf("start: %d artists / %d links", artists, links))
}

func (r *recordingBackfillReporter) OnArtistStart(current, artists int, artistID ArtistID) {
	r.events = append(r.events, fmt.Sprintf("artist: %d/%d %s", current, artists, artistID))
}

func (r *recordingBackfillReporter) OnArtistResolved(artistID ArtistID, id ChannelID, name ChannelName) {
	r.events = append(r.events, fmt.Sprintf("resolved: %s %s", artistID, id))
}

func (r *recordingBackfillReporter) OnArtistUnresolved(artistID ArtistID, reason string) {
	r.events = append(r.events, fmt.Sprintf("unresolved: %s", artistID))
}

// TestBackfillReportsProgress pins that a run says what it is doing. Each
// lookup is a separate yt-dlp process taking seconds, so silence reads as a
// hang rather than as progress.
func TestBackfillReportsProgress(t *testing.T) {
	probe := &stubProbe{byVideo: map[string]struct{ id, name string }{
		"aaaaaaaaaaa": {id: "UC1", name: "An Artist"},
		"bbbbbbbbbbb": {id: "UC2", name: "Another"},
	}}
	svc, env := backfillService(t, probe)
	reporter := &recordingBackfillReporter{}

	artistWithTrack(t, env, "one", "aaaaaaaaaaa")
	artistWithTrack(t, env, "two", "bbbbbbbbbbb")
	if err := svc.Link(t.Context(), "one", "An Artist", ""); err != nil {
		t.Fatal(err)
	}
	if err := svc.Link(t.Context(), "two", "Another", ""); err != nil {
		t.Fatal(err)
	}

	if _, err := svc.BackfillChannels(t.Context(), BackfillParams{Reporter: reporter}); err != nil {
		t.Fatal(err)
	}

	want := []string{
		"start: 2 artists / 2 links",
		"artist: 1/2 one",
		"resolved: one UC1",
		"artist: 2/2 two",
		"resolved: two UC2",
	}
	if len(reporter.events) != len(want) {
		t.Fatalf("events = %v, want %v", reporter.events, want)
	}
	for i := range want {
		if reporter.events[i] != want[i] {
			t.Errorf("event %d = %q, want %q", i, reporter.events[i], want[i])
		}
	}
}

// TestBackfillReportsNothingToDo: a library already backfilled must say so
// rather than print an empty table and exit, which looks like a failure.
func TestBackfillReportsNothingToDo(t *testing.T) {
	probe := &stubProbe{}
	svc, env := backfillService(t, probe)
	reporter := &recordingBackfillReporter{}

	artistWithTrack(t, env, "one", "aaaaaaaaaaa")
	if err := svc.Link(t.Context(), "one", "An Artist", "UC1"); err != nil {
		t.Fatal(err)
	}

	if _, err := svc.BackfillChannels(t.Context(), BackfillParams{Reporter: reporter}); err != nil {
		t.Fatal(err)
	}

	if len(probe.calls) != 0 {
		t.Errorf("made %d lookups when every link was known: %v", len(probe.calls), probe.calls)
	}
	if len(reporter.events) != 1 || reporter.events[0] != "start: 0 artists / 1 links" {
		t.Errorf("events = %v, want a single zero-work start", reporter.events)
	}
}

// TestBackfillWaitsBetweenRequests: pacing is what keeps a long run from
// drawing a 403, the failure the download path already classifies.
func TestBackfillWaitsBetweenRequests(t *testing.T) {
	probe := &stubProbe{byVideo: map[string]struct{ id, name string }{
		"aaaaaaaaaaa": {id: "UC1", name: "An Artist"},
	}}
	svc, env := backfillService(t, probe)

	for i, spec := range []struct{ artist, video string }{
		{"one", "aaaaaaaaaaa"}, {"two", "bbbbbbbbbbb"}, {"three", "ccccccccccc"},
	} {
		artistWithTrack(t, env, spec.artist, spec.video)
		if err := svc.Link(t.Context(), ArtistID(spec.artist), ChannelName(spec.artist+" ch"), ""); err != nil {
			t.Fatal(err)
		}
		probe.byVideo[spec.video] = struct{ id, name string }{id: fmt.Sprintf("UC%d", i+1), name: spec.artist + " ch"}
	}

	const gap = 100 * time.Millisecond

	start := time.Now()
	if _, err := svc.BackfillChannels(t.Context(), BackfillParams{
		MinWait: gap,
		MaxWait: gap,
	}); err != nil {
		t.Fatalf("BackfillChannels: %v", err)
	}
	elapsed := time.Since(start)

	// Three artists means two gaps between requests, not three: there is no
	// reason to sleep after the last one. The lower bound alone would not
	// catch a third gap, so the upper bound is the real assertion here.
	if elapsed < 2*gap {
		t.Errorf("ran in %v, want at least two %v gaps", elapsed, gap)
	}
	if elapsed >= 3*gap {
		t.Errorf("ran in %v, want no gap after the final artist (would be %v)", elapsed, 3*gap)
	}
}

// TestBackfillStopsWhenCancelled: a paced run must remain interruptible, or
// Ctrl-C waits out the timer.
func TestBackfillStopsWhenCancelled(t *testing.T) {
	probe := &stubProbe{byVideo: map[string]struct{ id, name string }{
		"aaaaaaaaaaa": {id: "UC1", name: "An Artist"},
		"bbbbbbbbbbb": {id: "UC2", name: "Another"},
	}}
	svc, env := backfillService(t, probe)

	artistWithTrack(t, env, "one", "aaaaaaaaaaa")
	artistWithTrack(t, env, "two", "bbbbbbbbbbb")
	for _, spec := range []struct{ artist, video string }{{"one", "aaaaaaaaaaa"}, {"two", "bbbbbbbbbbb"}} {
		if err := svc.Link(t.Context(), ArtistID(spec.artist), ChannelName(spec.artist+" ch"), ""); err != nil {
			t.Fatal(err)
		}
	}

	// Cancelled once the first artist is under way, so the run is sitting in
	// the gap between requests rather than failing at the loop head. That is
	// the path where a wait can swallow cancellation.
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	reporter := &cancellingReporter{cancel: cancel}

	start := time.Now()
	if _, err := svc.BackfillChannels(ctx, BackfillParams{
		MinWait:  2 * time.Second,
		MaxWait:  2 * time.Second,
		Reporter: reporter,
	}); err == nil {
		t.Error("BackfillChannels after cancellation = nil error, want cancellation")
	}
	// Returning promptly is the point: sitting out the timer would look like
	// a hang, which is the failure this pacing introduced in the first place.
	if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
		t.Errorf("took %v to notice cancellation mid-wait", elapsed)
	}
}

// cancellingReporter cancels the run as soon as the first artist starts, which
// lands the cancellation inside the wait between requests.
type cancellingReporter struct {
	cancel context.CancelFunc
}

func (r *cancellingReporter) OnBackfillStart(int, int)                          {}
func (r *cancellingReporter) OnArtistResolved(ArtistID, ChannelID, ChannelName) {}
func (r *cancellingReporter) OnArtistUnresolved(ArtistID, string)               {}

func (r *cancellingReporter) OnArtistStart(int, int, ArtistID) {
	r.cancel()
}
