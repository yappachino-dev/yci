// Package download uses yt-dlp to download music from youtube
package download

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
)

var (
	ErrForbidden = errors.New("access forbidden")
	ErrExecution = errors.New("yt-dlp execution failed")
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

	args = append(
		args,
		"--output", cfg.AbsoluteOutputPath,
		"--", cfg.URL,
	)

	return args
}

func Download(ctx context.Context, cfg Config) error {
	cmd := exec.CommandContext(ctx, "yt-dlp", buildArgs(cfg)...)

	var stderrBuf bytes.Buffer

	cmd.Stdout = os.Stdout
	cmd.Stderr = io.MultiWriter(os.Stderr, &stderrBuf)

	if err := cmd.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			stderrStr := strings.TrimSpace(stderrBuf.String())

			return fmt.Errorf("execute yt-dlp: %w: %s", parseError(stderrStr), stderrStr)
		}

		return fmt.Errorf("execute yt-dlp: %w", err)
	}

	return nil
}

func parseError(stderr string) error {
	s := strings.ToLower(stderr)

	switch {
	case strings.Contains(s, "http error 403") || strings.Contains(s, "403: forbidden"):
		return ErrForbidden
	default:
		return ErrExecution
	}
}
