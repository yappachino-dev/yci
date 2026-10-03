package library

import (
	"errors"
	"testing"

	"github.com/yappachino-dev/yci/internal/store"
)

// seedChannelArtists creates the artists a channel link needs, since
// foreign keys are enforced and an orphaned link is not representable.
func seedChannelArtists(t *testing.T, q *store.Queries, ids ...string) {
	t.Helper()

	for _, id := range ids {
		if _, err := q.CreateArtist(t.Context(), store.CreateArtistParams{
			ID:          id,
			DisplayName: id,
		}); err != nil {
			t.Fatalf("seed artist %q: %v", id, err)
		}
	}
}

// TestChannelResolvesByImmutableID is the point of migration 014: the identity
// survives a rename because it never depended on the name.
func TestChannelResolvesByImmutableID(t *testing.T) {
	queries := newLibraryDB(t)
	repo := NewSQLiteChannelRepository(queries)
	seedChannelArtists(t, queries, "irys")

	if err := repo.Create(t.Context(), "irys", "IRyS Ch. hololive-EN", ""); err != nil {
		t.Fatalf("create: %v", err)
	}

	// A download resolves the channel and reports the name YouTube now uses.
	if err := repo.AttachID(t.Context(), "irys", "IRyS Ch. hololive-EN", "UC8rcEBzJSleTkf_-agPM20g"); err != nil {
		t.Fatalf("attach id: %v", err)
	}

	got, ok, err := repo.ResolveByID(t.Context(), "UC8rcEBzJSleTkf_-agPM20g")
	if err != nil || !ok {
		t.Fatalf("ResolveByID = %q, %v, %v; want irys, true, nil", got, ok, err)
	}
	if got != "irys" {
		t.Errorf("resolved artist = %q, want irys", got)
	}

	// The rename: the label is refreshed, the link stays.
	if err := repo.RenameByID(t.Context(), "UC8rcEBzJSleTkf_-agPM20g", "irys", "IRyS Ch. hololive"); err != nil {
		t.Fatalf("rename: %v", err)
	}

	after, ok, err := repo.ResolveByID(t.Context(), "UC8rcEBzJSleTkf_-agPM20g")
	if err != nil || !ok || after != "irys" {
		t.Fatalf("after rename ResolveByID = %q, %v, %v; want irys, true, nil", after, ok, err)
	}

	// One row, not two: the rename must not have created a second link.
	all, err := repo.All(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 1 {
		t.Errorf("%d channel rows after rename, want 1", len(all))
	}
	if all[0].Name != "IRyS Ch. hololive" {
		t.Errorf("stored name = %q, want the refreshed label", all[0].Name)
	}
	if all[0].ChannelID != "UC8rcEBzJSleTkf_-agPM20g" {
		t.Errorf("stored channel id = %q, want the immutable id", all[0].ChannelID)
	}
}

// TestChannelNameAmbiguityIsAnError pins that a name shared by two artists is
// reported rather than resolved to whichever row came first.
func TestChannelNameAmbiguityIsAnError(t *testing.T) {
	queries := newLibraryDB(t)
	repo := NewSQLiteChannelRepository(queries)
	seedChannelArtists(t, queries, "one", "two")

	// Legal today: uniqueness is on the pair, so one name can sit under two
	// artists. Picking one silently would file downloads under the wrong one.
	if err := repo.Create(t.Context(), "one", "Shared Ch.", ""); err != nil {
		t.Fatalf("create one: %v", err)
	}
	if err := repo.Create(t.Context(), "two", "Shared Ch.", ""); err != nil {
		t.Fatalf("create two: %v", err)
	}

	_, ok, err := repo.FindArtistID(t.Context(), "Shared Ch.")
	if ok {
		t.Error("FindArtistID reported a resolution for an ambiguous name")
	}
	if err == nil {
		t.Fatal("FindArtistID = nil error, want an ambiguity error")
	}
	if !errors.Is(err, ErrChannelNameAmbiguous) {
		t.Errorf("error = %v, want it to wrap ErrChannelNameAmbiguous", err)
	}
}

// TestVideoChannelRecordSurvivesRetry covers the upsert, which a migration
// test cannot: it asserts behaviour, not just that the table exists.
func TestVideoChannelRecordSurvivesRetry(t *testing.T) {
	queries := newLibraryDB(t)
	repo := NewSQLiteChannelRepository(queries)

	if err := repo.RecordVideoChannel(t.Context(), "vid1", "UC1"); err != nil {
		t.Fatalf("record: %v", err)
	}

	id, ok, err := repo.VideoChannel(t.Context(), "vid1")
	if err != nil || !ok || id != "UC1" {
		t.Fatalf("VideoChannel = %q, %v, %v; want UC1, true, nil", id, ok, err)
	}

	// A retry re-records. Without the conflict clause this is a constraint
	// failure, which would turn a resumable download into a hard error.
	if err := repo.RecordVideoChannel(t.Context(), "vid1", "UC2"); err != nil {
		t.Fatalf("re-record on retry: %v", err)
	}
	if id, _, _ = repo.VideoChannel(t.Context(), "vid1"); id != "UC2" {
		t.Errorf("after retry channel = %q, want UC2 (the later answer wins)", id)
	}

	if err := repo.ForgetVideoChannel(t.Context(), "vid1"); err != nil {
		t.Fatalf("forget: %v", err)
	}
	if _, ok, _ = repo.VideoChannel(t.Context(), "vid1"); ok {
		t.Error("record survived ForgetVideoChannel")
	}
}

// TestVideoChannelLookupIsScopedToTheVideo keeps one video's channel from
// being read for another, which a mistyped key would otherwise do.
func TestVideoChannelLookupIsScopedToTheVideo(t *testing.T) {
	queries := newLibraryDB(t)
	repo := NewSQLiteChannelRepository(queries)

	if err := repo.RecordVideoChannel(t.Context(), "vid1", "UC1"); err != nil {
		t.Fatal(err)
	}

	if _, ok, err := repo.VideoChannel(t.Context(), "vid2"); ok || err != nil {
		t.Errorf("VideoChannel(vid2) = %v, %v; want not-found, nil", ok, err)
	}
}
