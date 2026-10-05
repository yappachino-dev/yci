package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/yappachino-dev/yci/internal/dto"
)

func post(t *testing.T, h http.Handler, path string, body any) *http.Response {
	t.Helper()

	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(string(raw)))
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Content-Type", "application/json")

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	return rec.Result()
}

// TestEnqueueReportsPerItemNotJustCounts is the reason EnqueueResult grew an
// Outcomes slice. A caller posting several URLs cannot act on "2 enqueued,
// 1 skipped" -- it has to know which of its URLs was refused and why.
func TestEnqueueReportsPerItemNotJustCounts(t *testing.T) {
	h := newTestServer(t)

	res := post(t, h, "/api/v1/queue", dto.QueueEnqueueRequest{
		URLs: []string{"https://www.youtube.com/watch?v=dQw4w9WgXcQ"},
	})
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d, want 201", res.StatusCode)
	}

	var got dto.QueueEnqueueResponse
	if err := json.NewDecoder(res.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}

	if len(got.Items) != 1 {
		t.Fatalf("got %d items, want 1", len(got.Items))
	}
	item := got.Items[0]
	if item.VideoID != "dQw4w9WgXcQ" {
		t.Errorf("video_id = %q, want dQw4w9WgXcQ", item.VideoID)
	}
	if item.Skipped {
		t.Errorf("a new video was reported skipped: %q", item.Reason)
	}
	if item.URL == "" {
		t.Error("the outcome does not say which URL asked for it")
	}
}

// A second post of the same URL must be refused with a reason, which is what
// makes enqueueing idempotent rather than duplicating the download.
func TestEnqueueRefusesAVideoAlreadyQueued(t *testing.T) {
	h := newTestServer(t)
	body := dto.QueueEnqueueRequest{URLs: []string{"https://www.youtube.com/watch?v=dQw4w9WgXcQ"}}

	post(t, h, "/api/v1/queue", body)

	res := post(t, h, "/api/v1/queue", body)
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d, want 201", res.StatusCode)
	}

	var got dto.QueueEnqueueResponse
	if err := json.NewDecoder(res.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}

	if len(got.Items) != 1 || !got.Items[0].Skipped {
		t.Fatalf("items = %+v, want one skipped", got.Items)
	}
	if got.Items[0].Reason == "" {
		t.Error("a skipped item carries no reason, so the caller cannot act on it")
	}
}

// A playlist URL expands to every video on it and needs a channel choice that
// only `source add` stores. Accepting it here would download far more than the
// caller named.
func TestEnqueueRefusesAPlaylistURL(t *testing.T) {
	h := newTestServer(t)

	res := post(t, h, "/api/v1/queue", dto.QueueEnqueueRequest{
		URLs: []string{"https://www.youtube.com/playlist?list=PLabcdef"},
	})
	if res.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", res.StatusCode)
	}

	var body map[string]string
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(body["error"], "source add") {
		t.Errorf("the error does not say what to do instead: %q", body["error"])
	}
}

func TestEnqueueRejectsAnEmptyRequest(t *testing.T) {
	h := newTestServer(t)

	res := post(t, h, "/api/v1/queue", dto.QueueEnqueueRequest{URLs: []string{}})
	if res.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", res.StatusCode)
	}
}

func TestEnqueueRejectsAMalformedBody(t *testing.T) {
	h := newTestServer(t)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/queue", strings.NewReader("{not json"))
	req.Header.Set("Authorization", "Bearer "+testToken)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

// The write endpoint must sit behind the same gate as the reads.
func TestEnqueueRequiresABearerToken(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/queue",
		strings.NewReader(`{"urls":["https://youtu.be/x"]}`))

	rec := httptest.NewRecorder()
	newTestServer(t).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
}
