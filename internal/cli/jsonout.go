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
// The default rendering of every list command is a column-aligned table
// built with tabwriter, which replaces each tab with padding spaces. That is
// the right shape for a person and useless for a script:
//
//	$ yci track list | cut -f1
//	5ead5f5e   nekomata-okayu       bRp8fX44icE   少女A / 猫又おかゆ
//
// cut sees no tabs, so it returns the whole row, and feeding that back as an
// ID fails. There was no way to recover the full 36-character ID either, since
// list output prints an 8-character prefix. `--json` exists to close both gaps:
// it is the only output in this CLI that is a contract.
//
// The types below are declared here rather than marshalled from the domain
// structs so the schema is deliberate. Reflecting internal structs would tie
// the published shape to internal field names, so renaming a Go field would
// silently break every script that reads yci's output. The downside is that
// they can drift, which is what TestJSONOutputSchemaIsStable pins.
//
// No field uses omitempty. A consumer reading `.artist_id` should get an
// empty string, not a missing key: absence and emptiness are different
// things, and a schema that changes shape based on the data is not a schema.
// It is harder to read by eye and it breaks `jq` expressions written against
// a row that happened to be populated.

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
