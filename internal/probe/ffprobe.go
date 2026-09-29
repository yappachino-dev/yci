package probe

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"time"
)

// FFprobe reads media properties by shelling out to ffprobe.
//
// It honors context cancellation, unlike in-process tag reading: a probe is a
// fork/exec plus a file scan, and a repair pass over a large library is
// interruptible work the user expects Ctrl-C to stop.
type FFprobe struct {
	binary string
}

// NewFFprobe locates ffprobe. It returns a usable Prober even when ffprobe is
// missing: Probe then fails with ErrUnavailable, which callers degrade on.
func NewFFprobe() *FFprobe {
	path, err := exec.LookPath("ffprobe")
	if err != nil {
		return &FFprobe{}
	}
	return &FFprobe{binary: path}
}

func (p *FFprobe) Probe(ctx context.Context, path string) (Info, error) {
	if p.binary == "" {
		return Info{}, fmt.Errorf("probe %q: %w", path, ErrUnavailable)
	}

	// -show_entries keeps stdout to the fields we parse; a full -show_streams
	// dump is an order of magnitude larger per file and we read one at a time.
	cmd := exec.CommandContext(ctx, p.binary,
		"-v", "error",
		"-print_format", "json",
		"-show_entries", "format=duration,bit_rate,format_name:stream=codec_type,codec_name,duration,bit_rate,sample_rate,channels",
		path,
	)

	out, err := cmd.Output()
	if err != nil {
		// Distinguish "cancelled" from "cannot read", so Ctrl-C does not
		// masquerade as a corrupt file.
		if ctxErr := ctx.Err(); ctxErr != nil {
			return Info{}, fmt.Errorf("probe %q: %w", path, ctxErr)
		}

		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			// ffprobe writes the reason to stderr; include it, or the caller
			// sees an opaque "exit status 1".
			return Info{}, fmt.Errorf("probe %q: ffprobe: %s", path, trimStderr(exitErr.Stderr))
		}

		return Info{}, fmt.Errorf("probe %q: %w", path, err)
	}

	return parseFFprobe(path, out)
}

func trimStderr(b []byte) string {
	s := string(b)
	for len(s) > 0 && (s[len(s)-1] == '\n' || s[len(s)-1] == '\r') {
		s = s[:len(s)-1]
	}
	if s == "" {
		return "no diagnostic output"
	}
	return s
}

type ffprobeOutput struct {
	Streams []struct {
		CodecType  string `json:"codec_type"`
		CodecName  string `json:"codec_name"`
		Duration   string `json:"duration"`
		BitRate    string `json:"bit_rate"`
		SampleRate string `json:"sample_rate"`
		Channels   int    `json:"channels"`
	} `json:"streams"`
	Format struct {
		Duration   string `json:"duration"`
		BitRate    string `json:"bit_rate"`
		FormatName string `json:"format_name"`
	} `json:"format"`
}

func parseFFprobe(path string, raw []byte) (Info, error) {
	var out ffprobeOutput
	if err := json.Unmarshal(raw, &out); err != nil {
		return Info{}, fmt.Errorf("probe %q: decode ffprobe output: %w", path, err)
	}

	info := Info{}

	// Container duration is the most reliable: stream duration is absent for
	// some muxers and is per-stream rather than per-file.
	if secs, err := strconv.ParseFloat(out.Format.Duration, 64); err == nil && secs > 0 {
		info.Duration = time.Duration(secs * float64(time.Second))
	}

	for _, s := range out.Streams {
		if s.CodecType != "audio" {
			continue
		}
		info.Codec = s.CodecName
		info.Channels = s.Channels

		if rate, err := strconv.Atoi(s.SampleRate); err == nil {
			info.SampleRate = rate
		}
		if br, err := strconv.ParseInt(s.BitRate, 10, 64); err == nil {
			info.Bitrate = br
		}

		if info.Duration == 0 {
			if secs, err := strconv.ParseFloat(s.Duration, 64); err == nil && secs > 0 {
				info.Duration = time.Duration(secs * float64(time.Second))
			}
		}

		break
	}

	// Container bitrate is a better estimate than the stream's when the
	// stream omits it.
	if br, err := strconv.ParseInt(out.Format.BitRate, 10, 64); err == nil && br > 0 {
		info.Bitrate = br
	}

	return info, nil
}
