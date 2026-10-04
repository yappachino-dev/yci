package download

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
)

// ChannelRef is one channel of a playlist as the user selects it. Both halves
// are kept because they answer different questions: the UC... ID is what a
// selection is matched against and survives the performer renaming the
// channel, while the name is what a person reads. ID is empty for listings
// that do not report one, such as a channel's own uploads list.
type ChannelRef struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type PlaylistMetadata struct {
	Channels []ChannelRef
	Videos   []VideoMetadata
	// Title is the playlist's own name, taken from the first entry. Empty
	// for a listing that does not report one.
	Title string
}

type VideoMetadata struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	Channel string `json:"channel"`
	// ChannelID is the immutable YouTube channel ID. A real playlist
	// carries it; a @channel/videos listing does not, so empty is normal.
	ChannelID string `json:"channel_id"`
	// Title is the name of the playlist this entry came from, carried by
	// every entry of a real playlist listing. Used to name a source when
	// the user did not.
	PlaylistTitle string `json:"playlist_title"`
}

// Fetch resolves a playlist URL with yt-dlp. It is the method form of the
// parser, reachable through the PlaylistFetcher seam.
func (y ytDlp) Fetch(ctx context.Context, url string) (PlaylistMetadata, error) {
	return parsePlaylist(ctx, url, y.errOut)
}

func parsePlaylist(ctx context.Context, url string, errOut io.Writer) (PlaylistMetadata, error) {
	args := []string{
		"--ignore-config",
		"--no-cookies",
		"--abort-on-error",

		"--flat-playlist",
		"--dump-json",
		"--", url,
	}

	cmd := exec.CommandContext(ctx, "yt-dlp", args...)

	cmd.Stderr = errOut

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
	var channels []ChannelRef
	seenChannels := make(map[string]struct{})

	decoder := json.NewDecoder(stdoutPipe)
	for decoder.More() {
		var video VideoMetadata
		if err := decoder.Decode(&video); err != nil {
			return PlaylistMetadata{}, fmt.Errorf("decode playlist entry: %w", err)
		}

		// Deduplicated on the ID when the listing carries one, so a
		// playlist that spells a channel two ways across its entries
		// still offers it once.
		key := video.ChannelID
		if key == "" {
			key = video.Channel
		}

		if _, seen := seenChannels[key]; !seen {
			seenChannels[key] = struct{}{}
			channels = append(channels, ChannelRef{ID: video.ChannelID, Name: video.Channel})
		}

		videos = append(videos, video)
	}

	if err := wait(); err != nil {
		return PlaylistMetadata{}, fmt.Errorf("parse playlist %q: %w", url, err)
	}

	return PlaylistMetadata{
		Channels: channels,
		Videos:   videos,
		Title:    playlistTitle(videos),
	}, nil
}

// playlistTitle reads the playlist name off the first entry that carries one.
// A listing of a single video, or one that omits the field, yields empty.
func playlistTitle(videos []VideoMetadata) string {
	for _, video := range videos {
		if video.PlaylistTitle != "" {
			return video.PlaylistTitle
		}
	}

	return ""
}
