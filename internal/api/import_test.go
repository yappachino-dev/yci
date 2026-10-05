package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yappachino-dev/yci/internal/app"
	"github.com/yappachino-dev/yci/internal/dto"
	"github.com/yappachino-dev/yci/internal/library"
	"github.com/yappachino-dev/yci/internal/testsupport"
)

func newImportServer(t *testing.T) (*app.Container, http.Handler) {
	t.Helper()

	c := newTestContainer(t)

	s, err := New(c, testToken)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	return c, s.Handler()
}

func mp3Fixture(t *testing.T) testsupport.AudioFormat {
	t.Helper()

	for _, f := range testsupport.AudioFormats {
		if f.Name == "mp3" {
			return f
		}
	}

	t.Fatal("no mp3 fixture format")
	return testsupport.AudioFormat{}
}

// stage synthesises a real tagged file and renames it to the <video-id><ext>
// the importer insists on, so the endpoint runs the genuine path rather than
// a stubbed metadata reader.
func stage(t *testing.T, c *app.Container, videoID, artist, title string) string {
	t.Helper()

	scratch := t.TempDir()
	produced := testsupport.SynthesizeAudio(t, scratch, mp3Fixture(t), title, artist, "Album", 1)

	name := videoID + ".mp3"
	if err := os.Rename(produced, filepath.Join(c.Config.StagingDir, name)); err != nil {
		t.Fatalf("stage %s: %v", name, err)
	}

	return name
}

func decodeItems(t *testing.T, res *http.Response) dto.StagingImportResponse {
	t.Helper()

	var got dto.StagingImportResponse
	if err := json.NewDecoder(res.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}

	return got
}

// TestImportMovesALinkedVideoIntoTheLibrary is the endpoint's reason to
// exist: without it a client can download audio and never place it.
func TestImportMovesALinkedVideoIntoTheLibrary(t *testing.T) {
	c, h := newImportServer(t)

	folder, err := c.FolderService.Create(t.Context(), "", library.FolderID(library.RootID), "Music")
	if err != nil {
		t.Fatalf("create folder: %v", err)
	}

	artist, err := c.ArtistService.Create(t.Context(), "", folder.ID, "Test Artist")
	if err != nil {
		t.Fatalf("create artist: %v", err)
	}

	if err := c.ChannelService.Link(t.Context(), artist.ID, "Test Artist", ""); err != nil {
		t.Fatalf("link channel: %v", err)
	}

	name := stage(t, c, "dQw4w9WgXcQ", "Test Artist", "A Real Song")

	res := post(t, h, "/api/v1/import", dto.StagingImportRequest{})
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", res.StatusCode)
	}

	got := decodeItems(t, res)
	if len(got.Items) != 1 {
		t.Fatalf("%d items, want 1", len(got.Items))
	}
	if got.Items[0].Status != string(library.StagingImported) {
		t.Fatalf("status = %q, reason %q", got.Items[0].Status, got.Items[0].Reason)
	}
	if got.Items[0].File != name {
		t.Errorf("file = %q, want %q", got.Items[0].File, name)
	}

	tracks, err := c.TrackService.All(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	found := false
	for _, tr := range tracks {
		if tr.SourceID == "dQw4w9WgXcQ" {
			found = true
		}
	}
	if !found {
		t.Error("the track was not registered: the endpoint reported success but did nothing")
	}

	// The file must have left staging rather than been copied.
	entries, err := os.ReadDir(c.Config.StagingDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("%d files left in staging, want 0", len(entries))
	}
}

// TestImportBlocksOnAnUnlinkedChannelWithoutLosingTheRest is why this
// endpoint answers per file. One unknown channel must not discard the files
// that did resolve, and the caller has to learn which channel is blocking so
// it can link an artist and retry exactly those.
func TestImportBlocksOnAnUnlinkedChannelWithoutLosingTheRest(t *testing.T) {
	c, h := newImportServer(t)

	first := stage(t, c, "aaaaaaaaaaa", "Channel One", "First Song")
	second := stage(t, c, "bbbbbbbbbbb", "Channel Two", "Second Song")

	res := post(t, h, "/api/v1/import", dto.StagingImportRequest{})
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200: a blocked file must not fail the whole call", res.StatusCode)
	}

	got := decodeItems(t, res)
	if len(got.Items) != 2 {
		t.Fatalf("%d items, want 2: a blocked file was dropped from the report", len(got.Items))
	}

	byFile := map[string]dto.StagingImportItem{}
	for _, item := range got.Items {
		byFile[item.File] = item
	}

	for _, name := range []string{first, second} {
		item, ok := byFile[name]
		if !ok {
			t.Fatalf("%s missing from the report", name)
		}
		if item.Status != string(library.StagingBlocked) {
			t.Errorf("%s status = %q, want %q", name, item.Status, library.StagingBlocked)
		}
		if item.ChannelName == "" {
			t.Errorf("%s reports no channel: the caller is blocked with nothing to resolve", name)
		}
		if item.Reason == "" {
			t.Errorf("%s is blocked with no reason", name)
		}
	}

	if byFile[first].ChannelName == byFile[second].ChannelName {
		t.Error("both files report the same channel; the per-file outcome is not per file")
	}

	// Nothing may have moved: a blocked file stays for a retry.
	entries, err := os.ReadDir(c.Config.StagingDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Errorf("%d files in staging, want 2: a blocked file was moved or deleted", len(entries))
	}
}

// An empty staging directory is an empty result, not an error and not null:
// a client polling for work to do has to be able to tell those apart.
func TestImportWithNothingStagedReturnsAnEmptyArray(t *testing.T) {
	_, h := newImportServer(t)

	res := post(t, h, "/api/v1/import", dto.StagingImportRequest{})
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", res.StatusCode)
	}

	got := decodeItems(t, res)
	if got.Items == nil {
		t.Error("items is null, want []")
	}
}

func TestImportRejectsAMalformedBody(t *testing.T) {
	_, h := newImportServer(t)

	// Genuinely invalid json: Go's decoder accepts unknown keys, so a body
	// with the wrong fields would decode cleanly into an empty request.
	req := httptest.NewRequest(http.MethodPost, "/api/v1/import", strings.NewReader("{not json"))
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Content-Type", "application/json")

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

// The guardrail for the whole surface: a write that moves files must be as
// protected as a read that leaks the library.
func TestImportRequiresABearerToken(t *testing.T) {
	_, h := newImportServer(t)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/import", strings.NewReader("{}"))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
}
