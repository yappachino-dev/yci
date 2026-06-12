// Package downloader uses yt-dlp to download music from youtube
package downloader

import (
	"fmt"
	"os"
	"os/exec"
)

func Download(exactOutputPath string, url string, minSleep string, maxSleep string) error {
	args := []string{
		"--ignore-config",
		"--no-cookies",
		"--abort-on-error",

		"--extract-audio",
		"--audio-format", "opus",
		"--audio-quality", "0",

		"--embed-metadata",
		"--embed-thumbnail",
		"--convert-thumbnails", "jpg",

		"--no-playlist",
		"--sleep-interval", minSleep,
		"--max-sleep-interval", maxSleep,

		// "--sponsorblock-remove music_off,intro,outro"
		"--output", exactOutputPath,
		url,
	}

	cmd := exec.Command("yt-dlp", args...)

	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("execute yt-dlp: %w", err)
	}

	return nil
}
