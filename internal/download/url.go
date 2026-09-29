package download

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/yappachino-dev/yci/internal/videoid"
)

type URLType int

const (
	URLTypeUnknown URLType = iota
	URLTypeVideo
	URLTypePlaylist
	URLTypeHybrid
)

type ParsedURL struct {
	Type       URLType
	VideoID    string
	PlaylistID string
}

// trustedHosts are the only hosts whose query parameters are accepted; any
// other host yields URLTypeUnknown rather than a rewritten YouTube URL.
var trustedHosts = map[string]struct{}{
	"youtube.com":       {},
	"www.youtube.com":   {},
	"m.youtube.com":     {},
	"music.youtube.com": {},
	"youtu.be":          {},
	"www.youtu.be":      {},
}

// shortHosts carry the video ID in the path instead of the query.
var shortHosts = map[string]struct{}{
	"youtu.be":     {},
	"www.youtu.be": {},
}

// isVideoID reports whether s looks like a YouTube video ID, the form stored
// as track SourceID. The rule itself lives in internal/videoid so that this
// and the staging importer cannot disagree about it.
func isVideoID(s string) bool {
	return videoid.IsValid(s)
}

func (p ParsedURL) CleanURL(forcePlaylist bool) string {
	switch p.Type {
	case URLTypeVideo:
		return "https://www.youtube.com/watch?v=" + p.VideoID
	case URLTypePlaylist:
		return "https://www.youtube.com/playlist?list=" + p.PlaylistID
	case URLTypeHybrid:
		if forcePlaylist {
			return "https://www.youtube.com/playlist?list=" + p.PlaylistID
		}

		return "https://www.youtube.com/watch?v=" + p.VideoID
	default:
		return ""
	}
}

// ParseURL classifies rawURL as a video, playlist, or hybrid link. Bare
// Bare video IDs (as stored by audit) are accepted as videos.
// Unrecognized input yields URLTypeUnknown with a nil error; only truly
// malformed URLs produce an error.
func ParseURL(rawURL string) (ParsedURL, error) {
	var result ParsedURL

	u, err := url.Parse(rawURL)
	if err != nil {
		return result, fmt.Errorf("invalid url format: %w", err)
	}

	host := strings.ToLower(u.Hostname())

	switch {
	case host == "":
		if id := strings.Trim(u.Path, "/"); isVideoID(id) {
			result.Type = URLTypeVideo
			result.VideoID = id
		}
	case isTrustedHost(host):
		query := u.Query()

		if _, ok := shortHosts[host]; ok {
			result.VideoID = strings.TrimPrefix(u.Path, "/")
		} else {
			result.VideoID = query.Get("v")
		}

		result.PlaylistID = query.Get("list")

		result.Type = classify(result.VideoID, result.PlaylistID)
	}

	return result, nil
}

func isTrustedHost(host string) bool {
	_, ok := trustedHosts[host]
	return ok
}

func classify(videoID, playlistID string) URLType {
	switch {
	case videoID != "" && playlistID != "":
		return URLTypeHybrid
	case videoID != "":
		return URLTypeVideo
	case playlistID != "":
		return URLTypePlaylist
	default:
		return URLTypeUnknown
	}
}
