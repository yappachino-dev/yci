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
	CleanURL   string
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
		result.CleanURL = "https://www.youtube.com/watch?v=" + result.VideoID
	case result.VideoID != "":
		result.Type = URLTypeVideo
		result.CleanURL = "https://www.youtube.com/watch?v=" + result.VideoID
	case result.PlaylistID != "":
		result.Type = URLTypePlaylist
		result.CleanURL = "https://www.youtube.com/playlist?list=" + result.PlaylistID
	}

	return result, nil
}
