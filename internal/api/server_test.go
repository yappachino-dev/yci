package api

import (
	"context"

	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/yappachino-dev/yci/internal/app"
	"github.com/yappachino-dev/yci/internal/config"
	"github.com/yappachino-dev/yci/internal/db"
	"github.com/yappachino-dev/yci/internal/dto"
)

const testToken = "s3cret"

func newTestContainer(t *testing.T) *app.Container {
	t.Helper()

	if err := db.SetUpMigrationContext(); err != nil {
		t.Fatalf("set up migrations: %v", err)
	}

	dbConn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { dbConn.Close() })

	if err := db.RunMigrations(context.Background(), dbConn); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	c, err := app.NewContainer(dbConn, config.Config{LibraryDir: t.TempDir(), StagingDir: t.TempDir()})
	if err != nil {
		t.Fatalf("new container: %v", err)
	}

	return c
}

func newTestServer(t *testing.T) http.Handler {
	t.Helper()

	c := newTestContainer(t)
	s, err := New(c, testToken)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	return s.Handler()
}

// get issues an authenticated request and returns the response.
func get(t *testing.T, h http.Handler, path string) *http.Response {
	t.Helper()

	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Header.Set("Authorization", "Bearer "+testToken)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	return rec.Result()
}

// TestRequiresABearerToken is the guardrail for the whole surface: every
// route sits behind the middleware, so a request without the token learns
// nothing about the library.
func TestRequiresABearerToken(t *testing.T) {
	h := newTestServer(t)

	t.Run("no header", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/tracks", nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Errorf("status = %d, want 401", rec.Code)
		}
		if rec.Body.Len() == 0 {
			t.Error("a rejected request returned no body to parse")
		}
	})

	t.Run("wrong token", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/tracks", nil)
		req.Header.Set("Authorization", "Bearer nope")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Errorf("status = %d, want 401", rec.Code)
		}
	})

	t.Run("prefix without a token", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/tracks", nil)
		req.Header.Set("Authorization", "Bearer ")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Errorf("status = %d, want 401", rec.Code)
		}
	})
}

// TestRefusesToStartWithoutAToken pins the failure that would otherwise be a
// listener on a network with no authentication at all.
func TestRefusesToStartWithoutAToken(t *testing.T) {
	if _, err := New(newTestContainer(t), ""); err == nil {
		t.Error("a server with no token was constructed")
	}
}

func TestEveryRouteAnswersTheContract(t *testing.T) {
	h := newTestServer(t)

	cases := []struct {
		path string
		want any
	}{
		{"/api/v1/tracks", &[]dto.Track{}},
		{"/api/v1/artists", &[]dto.Artist{}},
		{"/api/v1/folders", &[]dto.Folder{}},
		{"/api/v1/playlists", &[]dto.Playlist{}},
		{"/api/v1/sources", &[]dto.Source{}},
		{"/api/v1/queue", &[]dto.QueueEntry{}},
	}

	for _, c := range cases {
		t.Run(c.path, func(t *testing.T) {
			res := get(t, h, c.path)
			if res.StatusCode != http.StatusOK {
				t.Fatalf("status = %d, want 200", res.StatusCode)
			}

			if ct := res.Header.Get("Content-Type"); ct != "application/json; charset=utf-8" {
				t.Errorf("content-type = %q", ct)
			}

			// Decoding into the DTO proves the body is the contract, not
			// just something that happens to be valid JSON.
			if err := json.NewDecoder(res.Body).Decode(c.want); err != nil {
				t.Fatalf("body is not the expected shape: %v", err)
			}
		})
	}
}

// TestUnknownPathIsStillJSON keeps one response shape for a client to parse,
// which the mux's plain-text 404 would otherwise break.
func TestUnknownPathIsStillJSON(t *testing.T) {
	h := newTestServer(t)

	res := get(t, h, "/api/v1/nope")
	if res.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404", res.StatusCode)
	}

	var body map[string]string
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		t.Fatalf("body is not json: %v", err)
	}
	if body["error"] == "" {
		t.Error("no error message in the body")
	}
}

// TestEmptyLibraryIsAnArrayNotNull pins the rule the DTO package states:
// an empty list is [], because a client ranging over it should not have to
// guard against null.
func TestEmptyLibraryIsAnArrayNotNull(t *testing.T) {
	h := newTestServer(t)

	res := get(t, h, "/api/v1/tracks")

	raw, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}

	if got := string(trimSpace(raw)); got != "[]" {
		t.Errorf("empty library = %s, want []", got)
	}
}

func trimSpace(b []byte) []byte {
	start, end := 0, len(b)
	for start < end && (b[start] == '\n' || b[start] == ' ' || b[start] == '\t') {
		start++
	}
	for end > start && (b[end-1] == '\n' || b[end-1] == ' ' || b[end-1] == '\t') {
		end--
	}

	return b[start:end]
}
