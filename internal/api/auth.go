package api

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"
)

// ErrNoToken reports a server built without a bearer token.
var ErrNoToken = errors.New("a bearer token is required; set YCI_SERVE_TOKEN")

// authenticate gates every route on a bearer token.
//
// A single shared secret rather than accounts: this serves one person's
// library, and a user table would be a feature nobody asked for. The token
// lives in the environment rather than the config file so that
// `yci config get` cannot print it.
func (s *Server) authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		const prefix = "Bearer "

		header := r.Header.Get("Authorization")
		if !strings.HasPrefix(header, prefix) {
			s.unauthorized(w)
			return
		}

		got := strings.TrimPrefix(header, prefix)
		// Constant time so a wrong token cannot be discovered one byte at a
		// time. The length is compared first, which is not secret.
		if subtle.ConstantTimeCompare([]byte(got), []byte(s.token)) != 1 {
			s.unauthorized(w)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func (s *Server) unauthorized(w http.ResponseWriter) {
	w.Header().Set("WWW-Authenticate", `Bearer realm="yci"`)
	s.error(w, http.StatusUnauthorized, "invalid or missing bearer token")
}

// fail maps an internal error onto a response.
//
// The wrapped detail is not sent: it can carry a filesystem path, and this is
// a network surface. The log is where the full chain belongs.
func (s *Server) fail(w http.ResponseWriter, err error) {
	log.Printf("api: %v", err)
	s.error(w, http.StatusInternalServerError, "internal error")
}

// error writes a body shaped like every other response, so a client parses
// one thing whether it succeeded or not.
func (s *Server) error(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)

	body := map[string]string{"error": msg}
	if err := json.NewEncoder(w).Encode(body); err != nil {
		return
	}
}
