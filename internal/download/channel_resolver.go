package download

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
)

// ChannelID is a YouTube channel ID, the immutable UC... form.
type ChannelID string

// ChannelName is a YouTube channel's display name as read at one moment.
type ChannelName string

// ChannelResolver answers "which channel does this video belong to".
//
// It exists for the backfill. Links created before yci recorded channel IDs
// carry only a display name, and the only way to recover the identity is to
// ask the source about a video that artist published.
type ChannelResolver interface {
	ResolveVideoChannel(ctx context.Context, videoID string) (ChannelID, ChannelName, error)
}

// NewVideoChannelResolver returns a resolver that queries yt-dlp directly.
// A caller that only needs cookies and the process streams gets the defaults;
// a caller that needs output captured supplies its own writers.
func NewVideoChannelResolver(cookiesFile string) VideoChannelResolver {
	return VideoChannelResolver{cookiesFile: cookiesFile, out: os.Stdout, errOut: os.Stderr}
}

// VideoChannelResolver queries yt-dlp without downloading anything.
type VideoChannelResolver struct {
	cookiesFile string
	out, errOut io.Writer
}

var _ ChannelResolver = VideoChannelResolver{}

// buildResolveArgs assembles the metadata-only lookup.
//
// It uses metadataTemplate rather than a shorter local one: parseMetadataLine
// reads both invocations, so a template with a different field count parses as
// "not our line" and every backfill comes back empty without erroring.
func buildResolveArgs(videoID, cookiesFile string) []string {
	args := []string{
		"--ignore-config",
		"--no-warnings",
		"--skip-download",
		"--no-playlist",
		"--print", metadataTemplate,
	}

	if cookiesFile != "" {
		args = append(args, "--cookies", cookiesFile)
	} else {
		args = append(args, "--no-cookies")
	}

	return append(args, "--", "https://www.youtube.com/watch?v="+videoID)
}

// ResolveVideoChannel asks yt-dlp for the channel behind one video.
//
// --skip-download keeps this to a metadata request. --dump-json is not used:
// it simulates the download unless --no-simulate is also passed.
func (v VideoChannelResolver) ResolveVideoChannel(ctx context.Context, videoID string) (ChannelID, ChannelName, error) {
	cmd := exec.CommandContext(ctx, "yt-dlp", buildResolveArgs(videoID, v.cookiesFile)...)

	var stdout strings.Builder
	var stderr strings.Builder

	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return "", "", fmt.Errorf("query channel for video %q: %w: %s",
			videoID, err, strings.TrimSpace(stderr.String()))
	}

	var meta VideoMetadata

	for line := range strings.SplitSeq(stdout.String(), "\n") {
		if parsed, ok := parseMetadataLine(strings.TrimRight(line, "\r")); ok {
			meta = parsed
			break
		}
	}

	if meta.ChannelID == "" {
		return "", "", fmt.Errorf("yt-dlp reported no channel for video %q", videoID)
	}

	return ChannelID(meta.ChannelID), ChannelName(meta.Channel), nil
}
