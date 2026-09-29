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

func isTerminalError(err error) bool {
	return errors.Is(err, ErrForbidden)
}

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

// parseError classifies yt-dlp's stderr into one of two sentinels.
//
// The 403 case is matched on the wording of another program's diagnostic,
// which is the only way to tell a rate-limited or cookie-blocked download
// from any other failure without parsing yt-dlp's output format. It is a
// known coupling: if yt-dlp rewords this, downloads start reporting
// ErrExecution and the message the user sees loses its "sign in or supply
// cookies" hint. Treat the wording below as something to re-check on a
// yt-dlp upgrade rather than as a stable contract.
//
// Everything else is ErrExecution, which is deliberately coarse: the queue
// retries those and the user does not need the distinction.
func parseError(stderr string) error {
	s := strings.ToLower(stderr)

	// These are the phrasings yt-dlp has used for an HTTP 403. Matched on
	// the status code rather than the surrounding sentence where possible,
	// so a wording change in the rest of the line does not break it.
	switch {
	case strings.Contains(s, "http error 403"),
		strings.Contains(s, "403: forbidden"),
		strings.Contains(s, "403 forbidden"),
		strings.Contains(s, "error 403"),
		strings.Contains(s, "status code 403"),
		// yt-dlp reports a bot check without a status code. It is the
		// failure the cookies_file setting exists to fix, so it belongs
		// with the 403s rather than in the generic bucket where the user
		// is told nothing actionable.
		strings.Contains(s, "sign in to confirm you"),
		strings.Contains(s, "confirm you're not a bot"),
		strings.Contains(s, "confirm you are not a bot"),
		strings.Contains(s, "use --cookies"):
		return ErrForbidden
	default:
		return ErrExecution
	}
}
