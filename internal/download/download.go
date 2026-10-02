// Package download uses yt-dlp to download music from youtube
package download

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
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

// metadataPrefix marks the line yt-dlp prints for our own consumption, so it
// can be picked out of stdout and never shown to the user. Deliberately
// distinctive: anything else on that stream is yt-dlp's and is forwarded.
const metadataPrefix = "yci-video"

// metadataTemplate asks for the immutable identifiers alongside the display
// name. channel_id is the whole point: the name is a label the performer can
// change, and import used to trust it.
const metadataTemplate = metadataPrefix + "\t%(id)s\t%(channel_id)s\t%(channel)s"

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

		// --print, not --dump-json: the latter simulates the download
		// unless --no-simulate is also passed, which would quietly turn
		// every download into a no-op.
		"--print", metadataTemplate,
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

// Download fetches one track with yt-dlp. It is the method form of the
// package-level runner, reachable through the Downloader seam.
//
// It reports what yt-dlp saw, because a download is the only moment the
// authoritative channel ID is available: by import time the file carries only
// the artist's tag, which is the name and can drift.
func (y ytDlp) Download(ctx context.Context, cfg Config) (VideoMetadata, error) {
	var meta VideoMetadata

	cmd := exec.CommandContext(ctx, "yt-dlp", buildArgs(cfg)...)

	var stderrBuf bytes.Buffer

	cmd.Stderr = io.MultiWriter(y.errOut, &stderrBuf)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return meta, fmt.Errorf("create stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return meta, fmt.Errorf("start yt-dlp: %w", err)
	}

	meta, err = consumeStdout(stdout, y.out)
	if err != nil {
		return meta, err
	}

	if err := cmd.Wait(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			stderrStr := strings.TrimSpace(stderrBuf.String())

			return meta, fmt.Errorf("execute yt-dlp: %w: %s", parseError(stderrStr), stderrStr)
		}

		return meta, fmt.Errorf("execute yt-dlp: %w", err)
	}

	return meta, nil
}

// consumeStdout takes the one line we asked for out of yt-dlp's output and
// forwards everything else to the user untouched.
//
// Split out of Download because the decision is per line and worth testing
// directly: a version of this test that reimplemented the loop would pass
// while the loop itself was wrong.
func consumeStdout(r io.Reader, out io.Writer) (VideoMetadata, error) {
	var meta VideoMetadata

	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := strings.TrimRight(scanner.Text(), "\r")

		if parsed, ok := parseMetadataLine(line); ok {
			meta = parsed
			continue
		}

		fmt.Fprintln(out, line)
	}

	if err := scanner.Err(); err != nil {
		return meta, fmt.Errorf("read yt-dlp output: %w", err)
	}

	return meta, nil
}

// parseMetadataLine reads the tab-separated line our --print template emits.
func parseMetadataLine(line string) (VideoMetadata, bool) {
	parts := strings.Split(line, "\t")
	if len(parts) != 4 || parts[0] != metadataPrefix {
		return VideoMetadata{}, false
	}

	return VideoMetadata{
		ID:        parts[1],
		ChannelID: parts[2],
		Channel:   parts[3],
	}, true
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
		// A bot check arrives without a status code. It belongs with the
		// 403s because cookies_file is the fix, and the generic bucket
		// tells the user nothing actionable.
		strings.Contains(s, "sign in to confirm you"),
		strings.Contains(s, "confirm you're not a bot"),
		strings.Contains(s, "confirm you are not a bot"),
		strings.Contains(s, "use --cookies"):
		return ErrForbidden
	default:
		return ErrExecution
	}
}
