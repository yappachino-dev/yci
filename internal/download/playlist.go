package download

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
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
}

func parsePlaylist(ctx context.Context, url string) (PlaylistMetadata, error) {
	args := []string{
		"--ignore-config",
		"--no-cookies",
		"--abort-on-error",

		"--flat-playlist",
		"--dump-json",
		"--", url,
	}

	cmd := exec.CommandContext(ctx, "yt-dlp", args...)

	cmd.Stderr = os.Stderr

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
