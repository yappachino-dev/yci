// Package downloader uses yt-dlp to download music from youtube
package downloader

import (
	"fmt"
	"os"
	"os/exec"
)

type Config struct {
	AbsoluteOutputPath string
	URL                string
	MinSleep           string
	MaxSleep           string
}

func Download(cfg Config) error {
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
		"--sleep-interval", cfg.MinSleep,
		"--max-sleep-interval", cfg.MaxSleep,

		// "--sponsorblock-remove music_off,intro,outro"
		"--output", cfg.AbsoluteOutputPath,
		cfg.URL,
	}

	cmd := exec.Command("yt-dlp", args...)

	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("execute yt-dlp: %w", err)
	}

	return nil
}
