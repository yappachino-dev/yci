package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/yappachino-dev/yci/internal/library"
)

// Machine-readable output.
//
// The DTOs below are hand-written rather than reflected from the domain
// structs, and carry no omitempty. The reasoning is in style-guide 7a.
// jsonTrack is one track as `--json` reports it.
type jsonTrack struct {
	ID           string `json:"id"`
	Title        string `json:"title"`
	Album        string `json:"album"`
	ArtistID     string `json:"artist_id"`
	SourceID     string `json:"source_id"`
	Source       string `json:"source"`
	RelativePath string `json:"relative_path"`
	// DurationSec is 0 when unknown, which is distinct from a zero-length
	// file. Null would say the same thing more clearly at the cost of every
	// consumer having to handle it, so it stays a number.
	DurationSec int64 `json:"duration_sec"`
}

// jsonArtist is one artist as `--json` reports it.
type jsonArtist struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	FolderID string `json:"folder_id"`
}

// jsonFolder is one folder as `--json` reports it.
//
// The fields are flat, not the indented tree the human view draws: a consumer
// reconstructing the hierarchy from parent_id is a two-line loop, whereas
// parsing box-drawing characters out of a name is not a thing anyone should
// have to do.
type jsonFolder struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	ParentID string `json:"parent_id"`
	Depth    int    `json:"depth"`
}

// jsonQueueEntry is one download-queue row as `--json` reports it.
type jsonQueueEntry struct {
	ID        int    `json:"id"`
	SourceID  string `json:"source_id"`
	Status    string `json:"status"`
	Attempts  int    `json:"attempts"`
	ErrorMsg  string `json:"error_msg"`
	ErrorKind string `json:"error_kind"`
}

// jsonPlaylist is one playlist as `--json` reports it.
type jsonPlaylist struct {
	ID          int64    `json:"id"`
	Name        string   `json:"name"`
	SeedTrackID string   `json:"seed_track_id"`
	Tokens      []string `json:"fragment_tokens"`
	MatchMode   string   `json:"fragment_mode"`
	Excludes    []string `json:"fragment_excludes"`
}

// jsonPlaylistEntry is one ordered membership row.
type jsonPlaylistEntry struct {
	Position int64  `json:"position"`
	Track    string `json:"track_id"`
	Title    string `json:"title"`
	Duration int    `json:"duration_sec"`
}

func toJSONTrack(t library.Track) jsonTrack {
	return jsonTrack{
		ID:           string(t.ID),
		Title:        t.Title,
		Album:        t.Album,
		ArtistID:     string(t.ArtistID),
		SourceID:     t.SourceID,
		Source:       t.Source,
		RelativePath: string(t.RelativePath),
		DurationSec:  t.DurationSec,
	}
}

func toJSONPlaylist(p library.Playlist) jsonPlaylist {
	jp := jsonPlaylist{
		ID:        p.ID,
		Name:      p.Name,
		MatchMode: p.MatchMode.String(),
	}
	if p.SeedTrackID != "" {
		jp.SeedTrackID = string(p.SeedTrackID)
	}
	if len(p.FragmentTokens) > 0 {
		jp.Tokens = p.FragmentTokens
	}
	for _, e := range p.FragmentExcludes {
		jp.Excludes = append(jp.Excludes, string(e))
	}

	return jp
}

func toJSONArtist(a library.Artist) jsonArtist {
	return jsonArtist{
		ID:       string(a.ID),
		Name:     string(a.Name),
		FolderID: string(a.FolderID),
	}
}

func toJSONFolder(f library.Folder, depth int) jsonFolder {
	return jsonFolder{
		ID:       string(f.ID),
		Name:     string(f.Name),
		ParentID: string(f.ParentID),
		Depth:    depth,
	}
}

// writeJSON emits v to the command's output and returns no error of its own
// beyond the write itself.
//
// The output goes to stdout, not stderr: a machine-readable mode whose result
// has to be interleaved with progress is not machine-readable. Anything
// advisory belongs on stderr, which is where migration logging already goes.
func writeJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	// Indented because the realistic consumers are a human at a terminal and
	// jq, and a single compact line is miserable to read in the first case.
	enc.SetIndent("", "  ")
	// HTML escaping would turn <, > and & into \u003c and friends, which is
	// noise in titles that legitimately contain them.
	enc.SetEscapeHTML(false)

	if err := enc.Encode(v); err != nil {
		return fmt.Errorf("write json output: %w", err)
	}

	return nil
}

// queueErrorKind classifies a queue failure into a short, stable token.
//
// The full yt-dlp message belongs in error_msg for a human to read; a script
// wants to branch on "is this worth retrying", and matching on 120 characters
// of another program's diagnostic is not a contract. The classifier reads the
// same wording yt-dlp does, so it inherits the coupling recorded in
// internal/download — which is why it is exposed as data rather than hidden.
func queueErrorKind(msg string) string {
	if msg == "" {
		return ""
	}

	switch {
	case strings.Contains(msg, "403"), strings.Contains(msg, "cookies"), strings.Contains(msg, "not a bot"):
		return "forbidden"
	case strings.Contains(msg, "404"), strings.Contains(msg, "unavailable"), strings.Contains(msg, "private"):
		return "gone"
	case strings.Contains(msg, "429"), strings.Contains(msg, "too many requests"):
		return "rate_limited"
	case strings.Contains(msg, "503"), strings.Contains(msg, "502"), strings.Contains(msg, "500"):
		return "server_error"
	case strings.Contains(msg, "timeout"), strings.Contains(msg, "timed out"):
		return "timeout"
	default:
		return "other"
	}
}

// jsonRequested reports whether --json was asked for.
func jsonRequested(cmd *cobra.Command) bool {
	v, _ := cmd.Flags().GetBool(flagJSON)

	return v
}
