package api

import (
	"net/http"

	"github.com/yappachino-dev/yci/internal/dto"
	"github.com/yappachino-dev/yci/internal/library"
)

// importStaging moves everything in the staging directory into the library.
//
// It answers per file rather than failing the whole call. A batch where one
// channel has no artist yet must still import the files that do have one;
// returning a single error would throw away the good work and tell the client
// nothing it could act on. Blocked files come back naming the channel, so the
// client can link it and retry exactly those.
func (s *Server) importStaging(w http.ResponseWriter, r *http.Request) {
	// A caller has nothing to say beyond "do it", but an empty JSON object is
	// still required so a malformed body is rejected rather than ignored.
	var req dto.StagingImportRequest

	if !s.decodeBody(w, r, &req) {
		return
	}

	// The handler never prompts. There is nobody to prompt: the decision
	// about an unknown channel belongs to the client, which can name it back
	// and resolve it deliberately.
	outcomes, err := s.c.StagingService.ProcessStaging(r.Context(), func(channelName, _ string) (library.StagingResult, error) {
		return library.StagingResult{Action: library.ActionSkip}, nil
	})
	if err != nil {
		// The path can name a staging directory, which is not the caller's
		// to learn about over the network.
		s.fail(w, err)
		return
	}

	body := dto.StagingImportResponse{Items: dto.StagingImportItems(outcomes)}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)

	if err := dto.WriteJSON(w, body); err != nil {
		return
	}
}
