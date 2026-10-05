package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/yappachino-dev/yci/internal/download"
	"github.com/yappachino-dev/yci/internal/dto"
)

// maxEnqueueBody bounds the request body. Any real request is a handful of
// URLs; the limit is here so a mistaken upload is refused, not buffered.
const maxEnqueueBody = 64 << 10

// errPlaylistURL points at the command that already owns playlist watching.
// The message names the command because a caller reading it has to know what
// to run instead.
var errPlaylistURL = errors.New(
	"a playlist URL is not accepted here; use `source add <url>` to choose its channels once and poll them")

// enqueue adds videos to the download queue. It does not download them: the
// queue is drained separately, so a caller that posts five URLs gets five rows
// back and not five finished tracks.
func (s *Server) enqueue(w http.ResponseWriter, r *http.Request) {
	var req dto.QueueEnqueueRequest

	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxEnqueueBody)).Decode(&req); err != nil {
		s.error(w, http.StatusBadRequest, "body is not valid json")
		return
	}

	if len(req.URLs) == 0 {
		s.error(w, http.StatusBadRequest, "urls is required")
		return
	}

	// A playlist URL expands to every video on it and needs a channel choice.
	// That choice belongs to `source add`, which stores it once; guessing here
	// would silently download far more than the caller asked for.
	for _, u := range req.URLs {
		parsed, err := download.ParseURL(u)
		if err == nil && parsed.Type == download.URLTypePlaylist {
			s.error(w, http.StatusBadRequest, errPlaylistURL.Error())
			return
		}
	}

	// No reporter: Enqueue defaults it, and the per-item results a caller
	// needs now come back on the result rather than as events.
	result, err := s.c.DownloadService.Enqueue(r.Context(), download.EnqueueParams{
		URLs:          req.URLs,
		ForcePlaylist: req.ForcePlaylist,
		Retirement:    s.retirementFor(req.Force),
	})
	if err != nil {
		// The detail can name a URL and a filesystem path, neither of which
		// belongs in a network response.
		s.fail(w, err)
		return
	}

	body := dto.QueueEnqueueResponse{Items: dto.QueueEnqueueItems(result.Outcomes)}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusCreated)

	if err := dto.WriteJSON(w, body); err != nil {
		return
	}
}

// retirementFor decides whether this request honours the deletion journal.
//
// The journal exists to stop a deleted video coming back on the next
// unattended `source pull`, which is why pull always reads it. A caller
// posting over HTTP is not necessarily present, so the same rule applies by
// default. `force` is how to disagree, and it is not optional: the journal
// only records deletions made through yci and only since migration 015, so
// there are videos it cannot speak for that are still worth having.
func (s *Server) retirementFor(force bool) download.RetirementChecker {
	if force {
		return nil
	}

	return s.c.TrackJournal
}
