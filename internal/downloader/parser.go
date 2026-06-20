package downloader

import (
	"fmt"
	"net/url"
	"strings"
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

func ParseURL(rawURL string) (ParsedURL, error) {
	var result ParsedURL

	u, err := url.Parse(rawURL)
	if err != nil {
		return result, fmt.Errorf("invalid url format: %w", err)
	}

	query := u.Query()
	result.VideoID = query.Get("v")
	result.PlaylistID = query.Get("list")

	if u.Host == "youtu.be" || u.Host == "www.youtu.be" {
		result.VideoID = strings.TrimPrefix(u.Path, "/")
	}

	switch {
	case result.VideoID != "" && result.PlaylistID != "":
		result.Type = URLTypeHybrid
	case result.VideoID != "":
		result.Type = URLTypeVideo
	case result.PlaylistID != "":
		result.Type = URLTypePlaylist
	}

	return result, nil
}
