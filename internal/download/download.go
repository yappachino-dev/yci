// Package download uses yt-dlp to download music from youtube
package download

import (
	"context"
	"fmt"
	"os"
	"os/exec"
)

type Config struct {
	AbsoluteOutputPath string
	URL                string
	MinSleep           string
	MaxSleep           string
	CookiesFile        string
}

// buildArgs assembles the yt-dlp invocation. Cookies are only passed when
// configured; otherwise scraping stays cookie-free.
func buildArgs(cfg Config) []string {
	args := []string{
		"--ignore-config",
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
	}

	if cfg.CookiesFile != "" {
		args = append(args, "--cookies", cfg.CookiesFile)
	} else {
		args = append(args, "--no-cookies")
	}

	args = append(args,
		// "--sponsorblock-remove music_off,intro,outro"
		"--output", cfg.AbsoluteOutputPath,
		"--", cfg.URL,
	)

	return args
}

func Download(ctx context.Context, cfg Config) error {
	cmd := exec.CommandContext(ctx, "yt-dlp", buildArgs(cfg)...)

	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("execute yt-dlp: %w", err)
	}

	return nil
}
