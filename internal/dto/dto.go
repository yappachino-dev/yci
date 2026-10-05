// Package dto owns yci's machine-readable output contract.
//
// These types are hand-written rather than reflected from the domain structs
// and carry no omitempty; the reasoning is in docs/style-guide.md §7a. They
// live outside internal/cli because a second consumer now exists: the HTTP
// API serves the same shapes, and a contract that exists once in the command
// package and once in the server package would drift.
package dto

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/yappachino-dev/yci/internal/download"
	"github.com/yappachino-dev/yci/internal/library"
)

// Track is one track as reported to a machine consumer.
type Track struct {
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

// Artist is one artist as reported to a machine consumer.
type Artist struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	FolderID string `json:"folder_id"`
}

// Folder is one folder as reported to a machine consumer.
//
// The fields are flat, not the indented tree the human view draws: a consumer
// reconstructing the hierarchy from parent_id is a two-line loop, whereas
// parsing box-drawing characters out of a name is not a thing anyone should
// have to do.
type Folder struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	ParentID string `json:"parent_id"`
	Depth    int    `json:"depth"`
}

// QueueEntry is one download-queue row as reported to a machine consumer.
type QueueEntry struct {
	ID        int    `json:"id"`
	SourceID  string `json:"source_id"`
	Status    string `json:"status"`
	Attempts  int    `json:"attempts"`
	ErrorMsg  string `json:"error_msg"`
	ErrorKind string `json:"error_kind"`
}

// Playlist is one playlist as reported to a machine consumer.
type Playlist struct {
	ID          int64    `json:"id"`
	Name        string   `json:"name"`
	SeedTrackID string   `json:"seed_track_id"`
	Tokens      []string `json:"fragment_tokens"`
	MatchMode   string   `json:"fragment_mode"`
	Excludes    []string `json:"fragment_excludes"`
}

// PlaylistEntry is one ordered membership row.
type PlaylistEntry struct {
	Position int64  `json:"position"`
	Track    string `json:"track_id"`
	Title    string `json:"title"`
	Duration int    `json:"duration_sec"`
}

// QueueEnqueueRequest is the body of POST /queue.
type QueueEnqueueRequest struct {
	URLs []string `json:"urls"`
	// ForcePlaylist treats a hybrid watch/list URL as a playlist instead of
	// the single video it names.
	ForcePlaylist bool `json:"force_playlist"`
}

// QueueEnqueueResponse reports one item per video the request resolved to.
// A playlist URL yields many items sharing its url.
type QueueEnqueueResponse struct {
	Items []QueueEnqueueItem `json:"items"`
}

// QueueEnqueueItem is what happened to a single video.
type QueueEnqueueItem struct {
	URL     string `json:"url"`
	VideoID string `json:"video_id"`
	Skipped bool   `json:"skipped"`
	// Reason is a download.SkipReason, and is empty unless skipped.
	Reason string `json:"reason"`
}

// Source is one watched YouTube playlist as reported to a machine consumer.
type Source struct {
	Name     string   `json:"name"`
	URL      string   `json:"url"`
	Channels []string `json:"channels"`
	// LinkedChannels counts selections already attached to an artist.
	// UnlinkedCount is the remainder, which is what delivers nothing.
	LinkedChannels int `json:"linked_channels"`
	UnlinkedCount  int `json:"unlinked_count"`
}

func ToTrack(t library.Track) Track {
	return Track{
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

func ToArtist(a library.Artist) Artist {
	return Artist{
		ID:       string(a.ID),
		Name:     string(a.Name),
		FolderID: string(a.FolderID),
	}
}

func ToFolder(f library.Folder, depth int) Folder {
	return Folder{
		ID:       string(f.ID),
		Name:     string(f.Name),
		ParentID: string(f.ParentID),
		Depth:    depth,
	}
}

func ToPlaylist(p library.Playlist) Playlist {
	jp := Playlist{
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

// ToQueueEntry classifies the failure as it converts, so no caller can
// forget the step and ship a raw yt-dlp message as a contract value.
// QueueEnqueueItems converts enqueue outcomes into their wire form.
func QueueEnqueueItems(outcomes []download.EnqueueOutcome) []QueueEnqueueItem {
	items := make([]QueueEnqueueItem, 0, len(outcomes))
	for _, o := range outcomes {
		items = append(items, QueueEnqueueItem{
			URL:     o.URL,
			VideoID: o.VideoID,
			Skipped: o.Skipped,
			Reason:  o.Reason,
		})
	}

	return items
}

func ToQueueEntry(e download.QueueEntry) QueueEntry {
	return QueueEntry{
		ID:        e.ID,
		SourceID:  e.SourceID,
		Status:    string(e.Status),
		Attempts:  e.Attempts,
		ErrorMsg:  e.ErrorMsg,
		ErrorKind: QueueErrorKind(e.ErrorMsg),
	}
}

// SourceList derives the linked/unlinked split from the channels table rather
// than storing it, so linking or unlinking a channel needs no repair pass.
func SourceList(sources []library.Source, linked library.LinkedChannels) []Source {
	rows := make([]Source, 0, len(sources))
	for _, src := range sources {
		linkedCount, unlinked := LinkStatus(src.Channels, linked)

		rows = append(rows, Source{
			Name:           src.Name,
			URL:            src.URL,
			Channels:       src.ChannelNames(),
			LinkedChannels: linkedCount,
			UnlinkedCount:  unlinked,
		})
	}

	return rows
}

// WriteJSON emits v and returns no error of its own beyond the write itself.
func WriteJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	// Indented because the realistic consumers are a human at a terminal and
	// jq, and a single compact line is miserable to read in the first case.
	enc.SetIndent("", "  ")
	// HTML escaping would turn <, > and & into \u003c and friends, which
	// is noise in titles that legitimately contain them.
	enc.SetEscapeHTML(false)

	if err := enc.Encode(v); err != nil {
		return fmt.Errorf("write json output: %w", err)
	}

	return nil
}

// QueueErrorKind classifies a queue failure into a short, stable token.
//
// The full yt-dlp message belongs in error_msg for a human to read; a script
// wants to branch on "is this worth retrying", and matching on 120 characters
// of another program's diagnostic is not a contract. The classifier reads the
// same wording yt-dlp does, so it inherits the coupling recorded in
// internal/download — which is why it is exposed as data rather than hidden.
func QueueErrorKind(msg string) string {
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

// LinkStatus counts a selection against the linked channels, so a channel
// that was never attached to an artist is visible rather than silently
// delivering nothing. The human table computes the same split.
func LinkStatus(channels []library.SourceChannel, linked library.LinkedChannels) (int, int) {
	linkedCount := 0

	for _, ch := range channels {
		if linked.Contains(ch.ID, ch.Name) {
			linkedCount++
		}
	}

	return linkedCount, len(channels) - linkedCount
}

func FolderTree(folders []library.Folder) []Folder {
	childrenOf := make(map[library.FolderID][]library.Folder)
	for _, f := range folders {
		childrenOf[f.ParentID] = append(childrenOf[f.ParentID], f)
	}

	root := library.FolderID(library.RootID)

	var out []Folder

	var traverse func(parentID library.FolderID, depth int)
	traverse = func(parentID library.FolderID, depth int) {
		for _, child := range childrenOf[parentID] {
			if child.ID == root {
				continue
			}
			out = append(out, ToFolder(child, depth))
			traverse(child.ID, depth+1)
		}
	}

	traverse(root, 0)

	return out
}
