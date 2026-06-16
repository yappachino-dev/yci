package downloader

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
)

type VideoMetadata struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

func parsePlaylist(ctx context.Context, url string) ([]VideoMetadata, error) {
	args := []string{
		"--ignore-config",
		"--no-cookies",
		"--abort-on-error",

		"--flat-playlist",
		"--dump-json",
		url,
	}

	cmd := exec.CommandContext(ctx, "yt-dlp", args...)

	cmd.Stderr = os.Stderr

	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}

	if err := cmd.Start(); err != nil {
		return nil, err
	}

	defer func() {
		_ = cmd.Wait()
	}()

	var videos []VideoMetadata
	decoder := json.NewDecoder(stdoutPipe)

	for decoder.More() {
		var video VideoMetadata
		if err := decoder.Decode(&video); err != nil {
			return nil, err
		}
		videos = append(videos, video)
	}

	if err := cmd.Wait(); err != nil {
		return nil, err
	}

	return videos, nil
}
