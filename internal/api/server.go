// Package api serves the library over HTTP.
//
// It exists because the CLI is not always the way the library is reached: a
// client that is not a terminal wants the same data the CLI already reports.
// Every response is the JSON contract from internal/dto, so the two consumers
// cannot drift.
package api

import (
	"net/http"

	"github.com/yappachino-dev/yci/internal/app"
	"github.com/yappachino-dev/yci/internal/dto"
)

// Server answers read requests against one container.
type Server struct {
	c     *app.Container
	token string
}

// New takes the bearer token the server requires. An empty token is refused
// at construction rather than at request time: a server listening without
// authentication is the failure this package exists to make unlikely.
func New(c *app.Container, token string) (*Server, error) {
	if token == "" {
		return nil, ErrNoToken
	}

	return &Server{c: c, token: token}, nil
}

// Handler returns the routing table with authentication in front of it.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/tracks", s.tracks)
	mux.HandleFunc("GET /api/v1/artists", s.artists)
	mux.HandleFunc("GET /api/v1/folders", s.folders)
	mux.HandleFunc("GET /api/v1/playlists", s.playlists)
	mux.HandleFunc("GET /api/v1/sources", s.sources)
	mux.HandleFunc("GET /api/v1/queue", s.queue)
	mux.HandleFunc("POST /api/v1/queue", s.enqueue)

	// The mux would otherwise answer an unknown path with plain text, so a
	// client would have one shape to parse on failure and another on success.
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		s.error(w, http.StatusNotFound, "no such endpoint")
	})

	return s.authenticate(mux)
}

func (s *Server) tracks(w http.ResponseWriter, r *http.Request) {
	tracks, err := s.c.TrackService.All(r.Context())
	if err != nil {
		s.fail(w, err)
		return
	}

	out := make([]dto.Track, 0, len(tracks))
	for _, t := range tracks {
		out = append(out, dto.ToTrack(t))
	}

	s.write(w, out)
}

func (s *Server) artists(w http.ResponseWriter, r *http.Request) {
	artists, err := s.c.ArtistService.All(r.Context())
	if err != nil {
		s.fail(w, err)
		return
	}

	out := make([]dto.Artist, 0, len(artists))
	for _, a := range artists {
		out = append(out, dto.ToArtist(a))
	}

	s.write(w, out)
}

func (s *Server) folders(w http.ResponseWriter, r *http.Request) {
	folders, err := s.c.FolderService.All(r.Context())
	if err != nil {
		s.fail(w, err)
		return
	}

	s.write(w, dto.FolderTree(folders))
}

func (s *Server) playlists(w http.ResponseWriter, r *http.Request) {
	playlists, err := s.c.PlaylistService.All(r.Context())
	if err != nil {
		s.fail(w, err)
		return
	}

	out := make([]dto.Playlist, 0, len(playlists))
	for _, p := range playlists {
		out = append(out, dto.ToPlaylist(p))
	}

	s.write(w, out)
}

func (s *Server) sources(w http.ResponseWriter, r *http.Request) {
	sources, err := s.c.SourceService.List(r.Context())
	if err != nil {
		s.fail(w, err)
		return
	}

	linked, err := s.c.ChannelService.LinkedChannels(r.Context())
	if err != nil {
		s.fail(w, err)
		return
	}

	s.write(w, dto.SourceList(sources, linked))
}

func (s *Server) queue(w http.ResponseWriter, r *http.Request) {
	entries, err := s.c.DownloadService.All(r.Context())
	if err != nil {
		s.fail(w, err)
		return
	}

	out := make([]dto.QueueEntry, 0, len(entries))
	for _, e := range entries {
		out = append(out, dto.ToQueueEntry(e))
	}

	s.write(w, out)
}

// write emits a successful response. The DTO builders already return empty
// slices rather than nil, so an empty library is `[]` and not `null`.
func (s *Server) write(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")

	if err := dto.WriteJSON(w, v); err != nil {
		// The status line is already sent by this point, so the only honest
		// signal left is to stop rather than append a half-written body.
		return
	}
}
