package download

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
)

type PlaylistMetadata struct {
	Channels []string
	Videos   []VideoMetadata
}

type VideoMetadata struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	Channel string `json:"channel"`
	// ChannelID is the immutable YouTube channel ID. A real playlist
	// carries it; a @channel/videos listing does not, so empty is normal.
	ChannelID string `json:"channel_id"`
}

// Fetch resolves a playlist URL with yt-dlp. It is the method form of the
// parser, reachable through the PlaylistFetcher seam.
func (y ytDlp) Fetch(ctx context.Context, url string) (PlaylistMetadata, error) {
	return parsePlaylist(ctx, url, y.errOut)
}

func parsePlaylist(ctx context.Context, url string, errOut io.Writer) (PlaylistMetadata, error) {
	args := []string{
		"--ignore-config",
		"--no-cookies",
		"--abort-on-error",

		"--flat-playlist",
		"--dump-json",
		"--", url,
	}

	cmd := exec.CommandContext(ctx, "yt-dlp", args...)

	cmd.Stderr = errOut

	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return PlaylistMetadata{}, fmt.Errorf("create stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return PlaylistMetadata{}, fmt.Errorf("start yt-dlp: %w", err)
	}

	// The wait closure is the single owner of cmd.Wait: the deferred call
	// covers early returns (reaping the process), the normal path reads its
	// exit status after decoding.
	waited := false
	var waitErr error

	wait := func() error {
		if !waited {
			waited = true
			waitErr = cmd.Wait()
		}

		return waitErr
	}

	defer func() { _ = wait() }()

	var videos []VideoMetadata
	var channels []string
	seenChannels := make(map[string]struct{})

	decoder := json.NewDecoder(stdoutPipe)
	for decoder.More() {
		var video VideoMetadata
		if err := decoder.Decode(&video); err != nil {
			return PlaylistMetadata{}, fmt.Errorf("decode playlist entry: %w", err)
		}

		if _, seen := seenChannels[video.Channel]; !seen {
			seenChannels[video.Channel] = struct{}{}
			channels = append(channels, video.Channel)
		}

		videos = append(videos, video)
	}

	if err := wait(); err != nil {
		return PlaylistMetadata{}, fmt.Errorf("parse playlist %q: %w", url, err)
	}

	return PlaylistMetadata{
		Channels: channels,
		Videos:   videos,
	}, nil
}
