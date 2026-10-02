package cli

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/yappachino-dev/yci/internal/app"
	"github.com/yappachino-dev/yci/internal/library"
)

// TestJSONOutputSchemaIsStable pins the published shape of `--json`.
//
// The DTOs in jsonout.go are hand-written precisely so the schema does not
// track internal Go field names. That only holds if the keys are asserted
// here: a renamed field would otherwise silently change what every script
// reading yci's output sees, with nothing failing.
func TestJSONOutputSchemaIsStable(t *testing.T) {
	cases := []struct {
		name  string
		value any
		want  []string
	}{
		{"track", jsonTrack{}, []string{"id", "title", "album", "artist_id", "source_id", "source", "relative_path", "duration_sec"}},
		{"artist", jsonArtist{}, []string{"id", "name", "folder_id"}},
		{"folder", jsonFolder{}, []string{"id", "name", "parent_id", "depth"}},
		{"queue", jsonQueueEntry{}, []string{"id", "source_id", "status", "attempts", "error_msg", "error_kind"}},
		{"playlist", jsonPlaylist{}, []string{"id", "name", "seed_track_id", "fragment_tokens", "fragment_mode", "fragment_excludes"}},
		{"playlist entry", jsonPlaylistEntry{}, []string{"position", "track_id", "title", "duration_sec"}},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			raw, err := json.Marshal(c.value)
			if err != nil {
				t.Fatal(err)
			}

			var got map[string]json.RawMessage
			if err := json.Unmarshal(raw, &got); err != nil {
				t.Fatal(err)
			}

			if len(got) != len(c.want) {
				t.Errorf("%s has %d keys, want %d: %v", c.name, len(got), len(c.want), keysOf(got))
			}

			for _, key := range c.want {
				if _, ok := got[key]; !ok {
					t.Errorf("%s is missing key %q; got %v", c.name, key, keysOf(got))
				}
			}
		})
	}
}

// seedUUIDTracks mints tracks with UUID-shaped IDs, because the interesting
// property is that a 36-character ID survives --json intact while the human
// table shows an 8-character prefix of it.
func seedUUIDTracks(t *testing.T, c *app.Container, titles ...string) []string {
	t.Helper()

	ids := make([]string, 0, len(titles))
	for i, title := range titles {
		id := fmt.Sprintf("5ead5f5e-%04d-4bca-b300-600bab4935%02d", i, i)
		trackID := library.TrackID(id)

		if _, err := c.TrackService.Create(t.Context(), library.TrackCreateParams{
			ID:           trackID,
			Title:        title,
			RelativePath: library.TrackPath(fmt.Sprintf("uuid/%02d.opus", i)),
		}); err != nil {
			t.Fatalf("create track %q: %v", title, err)
		}

		ids = append(ids, id)
	}

	return ids
}

func keysOf(m map[string]json.RawMessage) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}

	return out
}

// TestJSONCarriesFullIDs is the reason `--json` exists at all.
//
// The human table prints an 8-character ID prefix out of a 36-character UUID,
// and there was no way to get the rest. A script that could not recover the
// full ID had no way to refer to a row it had just read.
func TestJSONCarriesFullIDs(t *testing.T) {
	c := newTestContainer(t)

	// seedTracks mints "track-00" style IDs, which are already shorter than
	// the truncation width and would pass trivially. These are UUID-shaped,
	// like the ones production has.
	ids := seedUUIDTracks(t, c, "a / b", "c / d")
	_ = ids

	cmd := &cobra.Command{}
	cmd.Flags().Bool(flagJSON, true, "")
	cmd.SetContext(WithContainer(t.Context(), c))

	var out strings.Builder
	cmd.SetOut(&out)

	if err := listTracks(WithContainer(t.Context(), c), cmd); err != nil {
		t.Fatal(err)
	}

	var tracks []jsonTrack
	if err := json.Unmarshal([]byte(out.String()), &tracks); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, out.String())
	}

	if len(tracks) != 2 {
		t.Fatalf("got %d tracks, want 2", len(tracks))
	}

	for _, tr := range tracks {
		if len(tr.ID) <= 8 {
			t.Errorf("id %q is still truncated", tr.ID)
		}
	}
	// The fixture's own IDs, so a fixture that quietly stopped using UUIDs
	// would not make this test meaningless.
	if len(tracks[0].ID) != 36 {
		t.Errorf("fixture id %q is %d chars; the test needs 36-char IDs to be meaningful",
			tracks[0].ID, len(tracks[0].ID))
	}
}

// TestJSONEmptyListIsAnArray covers the branch a script actually hits on a
// fresh install: with no rows the output must be `[]`, not the prose that
// tells a human what to do next. `json.Unmarshal` into a slice would fail
// loudly on `No tracks in the library`, which is the point.
func TestJSONEmptyListIsAnArray(t *testing.T) {
	c := newTestContainer(t)

	cmd := &cobra.Command{}
	cmd.Flags().Bool(flagJSON, true, "")
	cmd.SetContext(WithContainer(t.Context(), c))

	var out strings.Builder
	cmd.SetOut(&out)

	if err := listTracks(WithContainer(t.Context(), c), cmd); err != nil {
		t.Fatal(err)
	}

	var tracks []jsonTrack
	if err := json.Unmarshal([]byte(out.String()), &tracks); err != nil {
		t.Fatalf("empty output is not an empty JSON array: %v\n%q", err, out.String())
	}
	if len(tracks) != 0 {
		t.Errorf("got %d tracks, want 0", len(tracks))
	}
}

// TestJSONIsOnStdoutOnly guards the separation a script depends on: advisory
// text such as the migration log belongs on stderr, and any of it on stdout
// would make the JSON unparseable.
func TestJSONIsOnStdoutOnly(t *testing.T) {
	c := newTestContainer(t)
	seedTracks(t, c, "a / b")

	var out, errOut strings.Builder

	cmd := &cobra.Command{}
	cmd.Flags().Bool(flagJSON, true, "")
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetContext(WithContainer(t.Context(), c))

	if err := listTracks(WithContainer(t.Context(), c), cmd); err != nil {
		t.Fatal(err)
	}

	var tracks []jsonTrack
	if err := json.Unmarshal([]byte(out.String()), &tracks); err != nil {
		t.Fatalf("stdout is not parseable JSON: %v\n%s", err, out.String())
	}
}

// TestJSONEscapesNothingAway checks the CJK and symbol content this library
// is actually made of survives round-tripping through the encoder.
func TestJSONEscapesNothingAway(t *testing.T) {
	// Through writeJSON rather than json.Marshal, because that is where the
	// escaping behaviour is set. Marshalling directly would escape HTML
	// entities and this test would not notice.
	var out strings.Builder
	err := writeJSON(&out, jsonTrack{
		ID:    "id-1",
		Title: "星街すいせい <live> & 「うた」",
	})
	if err != nil {
		t.Fatal(err)
	}
	raw := []byte(out.String())

	if !strings.Contains(string(raw), "星街すいせい") {
		t.Errorf("CJK was escaped: %s", raw)
	}
	// HTML escaping would turn these into \\u003c and friends.
	for _, literal := range []string{"<live>", "&"} {
		if !strings.Contains(string(raw), literal) {
			t.Errorf("%q was escaped away: %s", literal, raw)
		}
	}

	var back jsonTrack
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatal(err)
	}
	if back.Title != "星街すいせい <live> & 「うた」" {
		t.Errorf("round trip changed the title: %q", back.Title)
	}
}

// TestFolderJSONIsFlatAndOrdered checks the hierarchy is expressed as data
// rather than as the box-drawing characters the human view draws.
func TestFolderJSONIsFlatAndOrdered(t *testing.T) {
	folders := []library.Folder{
		{ID: "root", Name: "root", ParentID: ""},
		{ID: "artists", Name: "Artists", ParentID: "root"},
		{ID: "vt", Name: "VT", ParentID: "artists"},
		{ID: "hololive", Name: "Hololive", ParentID: "vt"},
	}

	got := folderTreeJSON(folders)

	if len(got) != 3 {
		t.Fatalf("got %d folders, want 3 (root excluded): %+v", len(got), got)
	}

	// Parent before child, so a consumer can process the array in order.
	for i := 1; i < len(got); i++ {
		if got[i].Depth <= got[i-1].Depth {
			break
		}
	}

	wantDepths := []int{0, 1, 2}
	for i, want := range wantDepths {
		if got[i].Depth != want {
			t.Errorf("folder %d (%s) has depth %d, want %d", i, got[i].ID, got[i].Depth, want)
		}
	}

	for _, f := range got {
		if strings.ContainsAny(f.Name, "├└│─") {
			t.Errorf("folder name carries tree drawing characters: %q", f.Name)
		}
	}

	if got[0].ID != "artists" || got[0].ParentID != "root" {
		t.Errorf("first folder = %+v, want artists under root", got[0])
	}
}

// TestQueueErrorKindIsClassified pins the short tokens a script branches on,
// so the mapping is not left to be rediscovered by parsing diagnostics.
func TestQueueErrorKindIsClassified(t *testing.T) {
	cases := map[string]string{
		"":                                    "",
		"HTTP Error 403: Forbidden":           "forbidden",
		"Use --cookies to work around":        "forbidden",
		"Sign in to confirm you're not a bot": "forbidden",
		"HTTP Error 404: Not Found":           "gone",
		"Video unavailable":                   "gone",
		"HTTP Error 429: Too Many Requests":   "rate_limited",
		"HTTP Error 503: Service Unavailable": "server_error",
		"connection timed out":                "timeout",
		"something else entirely":             "other",
	}

	for msg, want := range cases {
		if got := queueErrorKind(msg); got != want {
			t.Errorf("queueErrorKind(%q) = %q, want %q", msg, got, want)
		}
	}
}

// TestPlaylistJSONCarriesTheRecipe pins that the recipe travels with the
// playlist: a script asking what a playlist is derived from needs the seed,
// the tokens, the mode and the exclusions, not just the name.
func TestPlaylistJSONCarriesTheRecipe(t *testing.T) {
	raw, err := json.Marshal(toJSONPlaylist(library.Playlist{
		ID:               7,
		Name:             "Suisei",
		SeedTrackID:      "seed-1",
		FragmentTokens:   []string{"星街すいせい"},
		MatchMode:        library.MatchAny,
		FragmentExcludes: []library.TrackID{"drop-1"},
	}))
	if err != nil {
		t.Fatal(err)
	}

	var got jsonPlaylist
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}

	if got.ID != 7 || got.Name != "Suisei" || got.SeedTrackID != "seed-1" {
		t.Errorf("identity fields wrong: %+v", got)
	}
	if got.MatchMode != "any" {
		t.Errorf("match mode = %q, want any", got.MatchMode)
	}
	if len(got.Tokens) != 1 || got.Tokens[0] != "星街すいせい" {
		t.Errorf("tokens = %v", got.Tokens)
	}
	if len(got.Excludes) != 1 || got.Excludes[0] != "drop-1" {
		t.Errorf("excludes = %v", got.Excludes)
	}
}

// TestPlaylistJSONKeepsAStableShapeWhenEmpty checks the other half of the
// no-omitempty rule: a hand-built playlist has no recipe, and the keys are
// still there with empty values.
//
// Absence would make the shape depend on the data, so `jq '.[] |
// .fragment_tokens | length'` would fail on a hand-built playlist while
// working on a seeded one.
func TestPlaylistJSONKeepsAStableShapeWhenEmpty(t *testing.T) {
	raw, err := json.Marshal(toJSONPlaylist(library.Playlist{ID: 1, Name: "Handmade"}))
	if err != nil {
		t.Fatal(err)
	}

	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}

	for _, key := range []string{"seed_track_id", "fragment_tokens", "fragment_excludes", "fragment_mode"} {
		if _, ok := got[key]; !ok {
			t.Errorf("key %q is absent for a hand-built playlist: %s", key, raw)
		}
	}

	// fragment_mode is populated even when unset, because it always has a
	// value: rows predating the column default to "all".
	if got["fragment_mode"] != "all" {
		t.Errorf("fragment_mode = %v, want all", got["fragment_mode"])
	}
	if got["seed_track_id"] != "" {
		t.Errorf("seed_track_id = %v, want empty", got["seed_track_id"])
	}
}

// TestArtistJSONMapsEveryField exercises the artist mapping itself, not just
// the shape of the DTO. TestJSONOutputSchemaIsStable marshals jsonArtist
// directly, so it pins the keys while proving nothing about the mapping from
// library.Artist: a field added to the domain type and never copied here
// would pass every test and simply not appear in the output.
func TestArtistJSONMapsEveryField(t *testing.T) {
	raw, err := json.Marshal(toJSONArtist(library.Artist{
		ID:       "mori-calliope",
		Name:     "Mori Calliope",
		FolderID: "vtuber",
	}))
	if err != nil {
		t.Fatal(err)
	}

	var got jsonArtist
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}

	if got.ID != "mori-calliope" || got.Name != "Mori Calliope" || got.FolderID != "vtuber" {
		t.Errorf("artist mapped as %+v", got)
	}
}

// TestArtistJSONKeepsKeysWhenFolderless covers the no-omitempty rule for the
// one artist field that is genuinely optional. An artist created before it
// was filed, or created with no folder, must still emit folder_id; a consumer
// reading it unconditionally would otherwise get a missing key.
func TestArtistJSONKeepsKeysWhenFolderless(t *testing.T) {
	raw, err := json.Marshal(toJSONArtist(library.Artist{
		ID:   "unfiled",
		Name: "未整理",
	}))
	if err != nil {
		t.Fatal(err)
	}

	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}

	if len(got) != 3 {
		t.Errorf("keys = %v, want exactly id, name, folder_id", got)
	}
	if got["folder_id"] != "" {
		t.Errorf("folder_id = %v, want empty string", got["folder_id"])
	}
	if got["name"] != "未整理" {
		t.Errorf("name = %v, want the untranslated name", got["name"])
	}
}
