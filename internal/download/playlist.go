package download

import (
	"context"
	"encoding/json"
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
		return PlaylistMetadata{}, err
	}

	if err := cmd.Start(); err != nil {
		return PlaylistMetadata{}, err
	}

	defer func() {
		_ = cmd.Wait()
	}()

	var videos []VideoMetadata
	var channels []string
	seenChannels := make(map[string]struct{})

	decoder := json.NewDecoder(stdoutPipe)
	for decoder.More() {
		var video VideoMetadata
		if err := decoder.Decode(&video); err != nil {
			return PlaylistMetadata{}, err
		}

		if _, seen := seenChannels[video.Channel]; !seen {
			seenChannels[video.Channel] = struct{}{}
			channels = append(channels, video.Channel)
		}

		videos = append(videos, video)
	}

	if err := cmd.Wait(); err != nil {
		return PlaylistMetadata{}, err
	}

	return PlaylistMetadata{
		Channels: channels,
		Videos:   videos,
	}, nil
}
