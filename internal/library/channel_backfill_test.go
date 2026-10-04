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

// TestBackfillRefreshesADriftedNameOnAKnownLink is the case that was wrong.
//
// A link with an ID whose *name* has drifted was reported "known" and skipped,
// so a renamed channel kept its old label forever. Whether a name is current
// is not knowable from the ID, so the run has to ask.
func TestBackfillRefreshesADriftedNameOnAKnownLink(t *testing.T) {
	probe := &stubProbe{byVideo: map[string]struct{ id, name string }{
		"aaaaaaaaaaa": {id: "UC1", name: "Current Name"},
	}}
	svc, env := backfillService(t, probe)

	artistWithTrack(t, env, "someone", "aaaaaaaaaaa")
	if err := svc.Link(t.Context(), "someone", "Old Name", "UC1"); err != nil {
		t.Fatal(err)
	}

	results, err := svc.BackfillChannels(t.Context(), BackfillParams{})
	if err != nil {
		t.Fatal(err)
	}
	if results[0].Outcome != BackfillRenamed {
		t.Errorf("outcome = %q, want %q (reason: %s)", results[0].Outcome, BackfillRenamed, results[0].Reason)
	}

	all, err := svc.All(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if all[0].Name != "Current Name" {
		t.Errorf("stored name = %q, want it refreshed to the current one", all[0].Name)
	}
	if all[0].ChannelID != "UC1" {
		t.Errorf("channel id = %q, want it left alone", all[0].ChannelID)
	}
}

// TestBackfillLeavesACurrentNameAlone: nothing to do must still say nothing
// to do rather than reporting a rename.
func TestBackfillLeavesACurrentNameAlone(t *testing.T) {
	probe := &stubProbe{byVideo: map[string]struct{ id, name string }{
		"aaaaaaaaaaa": {id: "UC1", name: "Current Name"},
	}}
	svc, env := backfillService(t, probe)

	artistWithTrack(t, env, "someone", "aaaaaaaaaaa")
	if err := svc.Link(t.Context(), "someone", "Current Name", "UC1"); err != nil {
		t.Fatal(err)
	}

	results, err := svc.BackfillChannels(t.Context(), BackfillParams{})
	if err != nil {
		t.Fatal(err)
	}
	if results[0].Outcome != BackfillAlreadyKnown {
		t.Errorf("outcome = %q, want %q", results[0].Outcome, BackfillAlreadyKnown)
	}

	all, err := svc.All(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if all[0].Name != "Current Name" {
		t.Errorf("stored name = %q, want it untouched", all[0].Name)
	}
}

// TestBackfillDryRunDoesNotRefreshNames: the dry run reports the rename but
// must not write it, or "what would change" becomes "what changed".
func TestBackfillDryRunDoesNotRefreshNames(t *testing.T) {
	probe := &stubProbe{byVideo: map[string]struct{ id, name string }{
		"aaaaaaaaaaa": {id: "UC1", name: "Current Name"},
	}}
	svc, env := backfillService(t, probe)

	artistWithTrack(t, env, "someone", "aaaaaaaaaaa")
	if err := svc.Link(t.Context(), "someone", "Old Name", "UC1"); err != nil {
		t.Fatal(err)
	}

	results, err := svc.BackfillChannels(t.Context(), BackfillParams{DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	if results[0].Outcome != BackfillRenamed {
		t.Errorf("outcome = %q, want the rename reported", results[0].Outcome)
	}

	all, err := svc.All(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if all[0].Name != "Old Name" {
		t.Errorf("stored name = %q, want a dry run to write nothing", all[0].Name)
	}
}

// TestBackfillLeavesOtherChannelsOfAnArtistAlone: an artist publishing on two
// channels has more links than the single channel the probe answered about.
// Applying that name to all of them would corrupt the other.
func TestBackfillLeavesOtherChannelsOfAnArtistAlone(t *testing.T) {
	probe := &stubProbe{byVideo: map[string]struct{ id, name string }{
		"aaaaaaaaaaa": {id: "UC1", name: "Channel One"},
	}}
	svc, env := backfillService(t, probe)

	artistWithTrack(t, env, "someone", "aaaaaaaaaaa")
	if err := svc.Link(t.Context(), "someone", "Channel One", "UC1"); err != nil {
		t.Fatal(err)
	}
	if err := svc.Link(t.Context(), "someone", "Channel Two", "UC2"); err != nil {
		t.Fatal(err)
	}

	results, err := svc.BackfillChannels(t.Context(), BackfillParams{})
	if err != nil {
		t.Fatal(err)
	}

	// Found stays empty for a link the probe never asked about, so the
	// outcome is what distinguishes "left alone" from "rewritten".
	for _, r := range results {
		if r.Stored != "Channel Two" {
			continue
		}
		if r.Outcome != BackfillAlreadyKnown {
			t.Errorf("un-queried link outcome = %q, want %q", r.Outcome, BackfillAlreadyKnown)
		}
		if r.Reason == "" {
			t.Error("un-queried link gives no reason, so the row reads as an unexplained no-op")
		}
	}

	all, err := svc.All(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range all {
		if c.Name == "Channel One" && c.ChannelID != "UC1" {
			t.Errorf("channel id moved: %+v", c)
		}
		if c.Name != "Channel One" && c.Name != "Channel Two" {
			t.Errorf("a link was renamed to something unasked for: %q", c.Name)
		}
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

// TestBackfillReportsNothingToDo: a fully converged library must say so per
// link rather than printing an empty result.
func TestBackfillReportsNothingToDo(t *testing.T) {
	probe := &stubProbe{byVideo: map[string]struct{ id, name string }{
		"aaaaaaaaaaa": {id: "UC1", name: "An Artist"},
	}}
	svc, env := backfillService(t, probe)
	reporter := &recordingBackfillReporter{}

	artistWithTrack(t, env, "one", "aaaaaaaaaaa")
	if err := svc.Link(t.Context(), "one", "An Artist", "UC1"); err != nil {
		t.Fatal(err)
	}

	results, err := svc.BackfillChannels(t.Context(), BackfillParams{Reporter: reporter})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Outcome != BackfillAlreadyKnown {
		t.Errorf("results = %+v, want one known link", results)
	}
	if reporter.events[0] != "start: 1 artists / 1 links" {
		t.Errorf("start event = %q, want the artist counted", reporter.events[0])
	}
}

// TestBackfillWaitsBetweenRequests: pacing is what keeps a long run from
// drawing a 403, the failure the download path already classifies.
//
// Seven artists, because the run has to clear noPaceThreshold before any
// wait happens at all. Three is enough to prove gaps are skipped after the
// last request, but too few to prove pacing engages.
func TestBackfillWaitsBetweenRequests(t *testing.T) {
	svc, _, _ := backfillWithArtists(t, 7)

	const gap = 100 * time.Millisecond

	start := time.Now()
	if _, err := svc.BackfillChannels(t.Context(), BackfillParams{
		MinWait: gap,
		MaxWait: gap,
	}); err != nil {
		t.Fatalf("BackfillChannels: %v", err)
	}
	elapsed := time.Since(start)

	// The run paces while more than noPaceThreshold requests remain: with
	// seven artists that is the first three. The lower bound alone would not
	// catch a fourth gap, so the upper bound is the real assertion here.
	if elapsed < 3*gap {
		t.Errorf("ran in %v, want at least three %v gaps", elapsed, gap)
	}
	if elapsed >= 4*gap {
		t.Errorf("ran in %v, want no gap once pacing stops applying", elapsed)
	}
}

// TestBackfillSkipsPacingForSmallRuns: a handful of flat lookups costs less
// bandwidth than the wait before the first of them would take.
func TestBackfillSkipsPacingForSmallRuns(t *testing.T) {
	svc, _, _ := backfillWithArtists(t, noPaceThreshold-1)

	const gap = 2 * time.Second

	start := time.Now()
	if _, err := svc.BackfillChannels(t.Context(), BackfillParams{
		MinWait: gap,
		MaxWait: gap,
	}); err != nil {
		t.Fatalf("BackfillChannels: %v", err)
	}

	if elapsed := time.Since(start); elapsed >= gap {
		t.Errorf("ran in %v with %d artists, want no %v wait below the threshold", elapsed, noPaceThreshold-1, gap)
	}
}

// backfillWithArtists seeds n artists that each own one track and one channel
// link, with a probe that resolves every one of them.
func backfillWithArtists(t *testing.T, n int) (*ChannelService, *libraryEnv, *stubProbe) {
	t.Helper()

	probe := &stubProbe{byVideo: map[string]struct{ id, name string }{}}
	svc, env := backfillService(t, probe)

	for i := range n {
		artist := fmt.Sprintf("a%02d", i)
		video := fmt.Sprintf("%011d", i+1)

		artistWithTrack(t, env, artist, video)
		if err := svc.Link(t.Context(), ArtistID(artist), ChannelName(artist+" ch"), ""); err != nil {
			t.Fatal(err)
		}

		probe.byVideo[video] = struct{ id, name string }{id: fmt.Sprintf("UC%d", i+1), name: artist + " ch"}
	}

	return svc, env, probe
}

// TestBackfillStopsWhenCancelled: a paced run must remain interruptible, or
// Ctrl-C waits out the timer.
//
// Seven artists, because below noPaceThreshold no wait happens and the run
// would finish before cancellation could be observed at all: the test would
// pass without ever exercising the select.
func TestBackfillStopsWhenCancelled(t *testing.T) {
	svc, _, _ := backfillWithArtists(t, 7)

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

// TestBackfillNumbersArtistsConsistently pins the counter against the exact
// bug it had: the numerator indexed every artist while the denominator counted
// only those needing work, printing "[9/6]".
//
// The invariant is that no event number exceeds the total, and that the last
// one equals it.
func TestBackfillNumbersArtistsConsistently(t *testing.T) {
	artists := []struct{ id, video string }{
		{"one", "aaaaaaaaaaa"},
		{"two", "bbbbbbbbbbb"},
		{"three", "ccccccccccc"},
		{"four", "ddddddddddd"},
	}

	probe := &stubProbe{byVideo: map[string]struct{ id, name string }{}}
	svc, env := backfillService(t, probe)
	reporter := &numberingReporter{}

	// Two are seeded already carrying an ID and a current name. A mix is the
	// point: with every artist unknown the two counts coincide, which is why
	// the original test could not see the mismatch it was written for.
	known := map[string]bool{"one": true, "three": true}

	for i, a := range artists {
		artistWithTrack(t, env, a.id, a.video)

		id := ChannelID(fmt.Sprintf("UC%d", i+1))
		if known[a.id] {
			// Distinct per artist: the v14 index forbids one channel
			// ID on two artists, and sharing one fails the link.
			id = ChannelID(fmt.Sprintf("UCKEEP%d", i+1))
		}

		if err := svc.Link(t.Context(), ArtistID(a.id), ChannelName(a.id+" ch"), id); err != nil {
			t.Fatal(err)
		}
		probe.byVideo[a.video] = struct {
			id, name string
		}{id: fmt.Sprintf("UC%d", i+1), name: a.id + " ch"}
	}

	if _, err := svc.BackfillChannels(t.Context(), BackfillParams{Reporter: reporter}); err != nil {
		t.Fatal(err)
	}

	if reporter.total != len(artists) {
		t.Errorf("start reported %d artists, want %d", reporter.total, len(artists))
	}
	if len(reporter.numbers) != len(artists) {
		t.Fatalf("%d start events, want %d", len(reporter.numbers), len(artists))
	}

	for i, n := range reporter.numbers {
		d := reporter.denoms[i]

		// The exact shape of the reported bug: a number larger than its own
		// denominator on the same line.
		if n > d {
			t.Errorf("event %d numbered %d of %d", i, n, d)
		}
		// And the denominator must agree with the count announced at the
		// start, or progress contradicts itself across the two.
		if d != reporter.total {
			t.Errorf("event %d says %d of %d, but the start announced %d", i, n, d, reporter.total)
		}
		if n != i+1 {
			t.Errorf("event %d numbered %d, want %d (out of order or repeated)", i, n, i+1)
		}
	}

	if last := reporter.numbers[len(reporter.numbers)-1]; last != reporter.total {
		t.Errorf("last event numbered %d, want %d", last, reporter.total)
	}
}

// numberingReporter records both halves of each event, because the bug was a
// disagreement between them within one line ("[9/6]"), not a disagreement with
// the separate start message.
type numberingReporter struct {
	total   int
	numbers []int
	denoms  []int
}

func (r *numberingReporter) OnBackfillStart(artists, _ int) { r.total = artists }
func (r *numberingReporter) OnArtistStart(current, artists int, _ ArtistID) {
	r.numbers = append(r.numbers, current)
	r.denoms = append(r.denoms, artists)
}
func (r *numberingReporter) OnArtistResolved(ArtistID, ChannelID, ChannelName) {}
func (r *numberingReporter) OnArtistUnresolved(ArtistID, string)               {}
