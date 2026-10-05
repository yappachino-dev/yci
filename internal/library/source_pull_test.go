package library

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

// stubSources is an in-memory SourceRepository, so pull behaviour is tested
// without a database. The real repository is covered by its own tests.
type stubSources struct {
	sources []Source
	created []Source
	removed []string
	failErr error
}

func (s *stubSources) Create(_ context.Context, name, url, channels string) error {
	if s.failErr != nil {
		return s.failErr
	}

	decoded, err := decodeSourceRow(name, url, channels)
	if err != nil {
		return err
	}

	s.created = append(s.created, decoded)
	s.sources = append(s.sources, decoded)

	return nil
}

func (s *stubSources) ByName(_ context.Context, name string) (Source, error) {
	for _, src := range s.sources {
		if src.Name == name {
			return src, nil
		}
	}

	return Source{}, ErrSourceNotFound
}

func (s *stubSources) All(context.Context) ([]Source, error) {
	return s.sources, nil
}

func (s *stubSources) SetChannels(_ context.Context, name, channels string) error {
	decoded, err := decodeSourceRow(name, "", channels)
	if err != nil {
		return err
	}

	for i := range s.sources {
		if s.sources[i].Name == name {
			s.sources[i].Channels = decoded.Channels
			return nil
		}
	}

	return ErrSourceNotFound
}

func (s *stubSources) Rename(_ context.Context, name, newName string) error {
	for i := range s.sources {
		if s.sources[i].Name == name {
			s.sources[i].Name = newName
			return nil
		}
	}

	return ErrSourceNotFound
}

func (s *stubSources) Delete(_ context.Context, name string) error {
	s.removed = append(s.removed, name)

	for i := range s.sources {
		if s.sources[i].Name == name {
			s.sources = append(s.sources[:i], s.sources[i+1:]...)
			return nil
		}
	}

	return ErrSourceNotFound
}

func TestSourceAddRefusesAnEmptySelection(t *testing.T) {
	svc := NewSourceService(&stubSources{})

	if err := svc.Add(t.Context(), "vt", "https://youtube.com/playlist?list=PL1", nil); !errors.Is(err, ErrNoChannelsSaved) {
		t.Errorf("Add with no channels = %v, want ErrNoChannelsSaved", err)
	}
}

// TestSourceRoundTripsChannelsThroughJSON: a channel name is free text and
// may contain the separator a delimited format would have used.
func TestSourceRoundTripsChannelsThroughJSON(t *testing.T) {
	repo := &stubSources{}
	svc := NewSourceService(repo)

	channels := []SourceChannel{
		{ID: "UCa", Name: "A, Channel"},
		{ID: "UCb", Name: `B "Channel" | C`},
	}

	if err := svc.Add(t.Context(), "vt", "https://youtube.com/playlist?list=PL1", channels); err != nil {
		t.Fatalf("Add: %v", err)
	}

	got, err := svc.ByName(t.Context(), "vt")
	if err != nil {
		t.Fatalf("ByName: %v", err)
	}

	if len(got.Channels) != len(channels) {
		t.Fatalf("stored %d channels, want %d", len(got.Channels), len(channels))
	}

	for i, want := range channels {
		if got.Channels[i] != want {
			t.Errorf("channel %d = %+v, want %+v", i, got.Channels[i], want)
		}
	}
}

// TestPullIsolatesAFailingSource: one playlist going private must not abandon
// the rest of the run, or a single deletion looks like the end of tracking.
func TestPullIsolatesAFailingSource(t *testing.T) {
	repo := &stubSources{sources: []Source{
		{Name: "first", URL: "https://youtube.com/playlist?list=PL1"},
		{Name: "broken", URL: "https://youtube.com/playlist?list=PL2"},
		{Name: "last", URL: "https://youtube.com/playlist?list=PL3"},
	}}
	svc := NewSourceService(repo)

	var polled []string

	results, err := svc.Pull(t.Context(), PullParams{
		Enqueue: func(_ context.Context, url string, _ []SourceChannel) (int, error) {
			polled = append(polled, url)

			if url == "https://youtube.com/playlist?list=PL2" {
				return 0, errors.New("playlist is private")
			}

			return 3, nil
		},
	})
	if err != nil {
		t.Fatalf("Pull: %v", err)
	}

	if len(results) != 3 {
		t.Fatalf("got %d results, want 3", len(results))
	}

	if results[1].Err == nil {
		t.Error("the failing source reported no error")
	}

	if results[2].Err != nil {
		t.Errorf("a source after the failure reported %v, want it polled anyway", results[2].Err)
	}

	// The point of the test: the third source was reached at all.
	if len(polled) != 3 {
		t.Errorf("polled %d sources, want all 3", len(polled))
	}
}

func TestPullCountsWhatWasEnqueued(t *testing.T) {
	repo := &stubSources{sources: []Source{
		{Name: "a", URL: "u1"},
		{Name: "b", URL: "u2"},
	}}
	svc := NewSourceService(repo)

	results, err := svc.Pull(t.Context(), PullParams{
		Enqueue: func(_ context.Context, url string, _ []SourceChannel) (int, error) {
			if url == "u1" {
				return 7, nil
			}

			return 0, nil
		},
	})
	if err != nil {
		t.Fatalf("Pull: %v", err)
	}

	if results[0].Enqueued != 7 {
		t.Errorf("first source enqueued %d, want 7", results[0].Enqueued)
	}

	if results[1].Enqueued != 0 {
		t.Errorf("second source enqueued %d, want 0", results[1].Enqueued)
	}
}

// TestPullSkipsPacingForFewSources: a handful of flat listings costs less
// than the wait that would precede them.
func TestPullSkipsPacingForFewSources(t *testing.T) {
	repo := &stubSources{}
	for i := range noPaceThreshold - 1 {
		repo.sources = append(repo.sources, Source{Name: fmt.Sprintf("s%d", i), URL: "u"})
	}

	svc := NewSourceService(repo)

	start := time.Now()

	noop := func(context.Context, string, []SourceChannel) (int, error) { return 0, nil }
	if _, err := svc.Pull(t.Context(), PullParams{Enqueue: noop, MinWait: 2 * time.Second, MaxWait: 2 * time.Second}); err != nil {
		t.Fatalf("Pull: %v", err)
	}

	if elapsed := time.Since(start); elapsed >= 2*time.Second {
		t.Errorf("ran in %v with %d sources, want no wait below the threshold", elapsed, noPaceThreshold-1)
	}
}

func TestPullRequiresAnEnqueueFunction(t *testing.T) {
	svc := NewSourceService(&stubSources{sources: []Source{{Name: "a", URL: "u"}}})

	if _, err := svc.Pull(t.Context(), PullParams{}); err == nil {
		t.Error("Pull with no enqueue function returned no error")
	}
}

func TestPullStopsWhenCancelled(t *testing.T) {
	svc := NewSourceService(&stubSources{sources: []Source{{Name: "a", URL: "u"}}})

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, err := svc.Pull(ctx, PullParams{
		Enqueue: func(context.Context, string, []SourceChannel) (int, error) { return 0, nil },
	})
	if !errors.Is(err, context.Canceled) {
		t.Errorf("Pull after cancellation = %v, want context.Canceled", err)
	}
}

// TestSourceRemoveReportsAnUnknownName: removing something that was never
// watched must say so rather than claim success.
func TestSourceRemoveReportsAnUnknownName(t *testing.T) {
	svc := NewSourceService(&stubSources{})

	if err := svc.Remove(t.Context(), "nope"); !errors.Is(err, ErrSourceNotFound) {
		t.Errorf("Remove of an unknown source = %v, want ErrSourceNotFound", err)
	}
}
