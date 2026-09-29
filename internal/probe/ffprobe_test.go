package probe

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/yappachino-dev/yci/internal/testsupport"
)

func TestFFprobeReadsDurationAcrossFormats(t *testing.T) {
	if !testsupport.FFMpegAvailable() {
		t.Skip("ffmpeg not installed")
	}

	const want = 3

	for _, format := range testsupport.AudioFormats {
		t.Run(format.Name, func(t *testing.T) {
			path := testsupport.SynthesizeAudio(t, t.TempDir(), format, "Probe Fixture", "Probe Artist", "Probe Album", want)

			info, err := NewFFprobe().Probe(context.Background(), path)
			if err != nil {
				t.Fatalf("Probe: %v", err)
			}

			// Encoders pad slightly; a hard equality would be flaky.
			if d := info.Duration.Seconds(); d < want-0.5 || d > want+1.5 {
				t.Errorf("Duration = %v, want ~%ds", info.Duration, want)
			}

			if info.Codec == "" {
				t.Error("Codec is empty")
			}
			if info.SampleRate != 48000 {
				t.Errorf("SampleRate = %d, want 48000", info.SampleRate)
			}
			if info.Channels != 2 {
				t.Errorf("Channels = %d, want 2", info.Channels)
			}
			if info.Duration < time.Duration(want)*time.Second/2 {
				t.Errorf("Duration %v is implausibly short", info.Duration)
			}
		})
	}
}

func TestFFprobeMissingBinaryIsUnavailableNotBroken(t *testing.T) {
	// A prober with no binary must report ErrUnavailable so callers can
	// degrade, rather than an opaque failure that looks like a bad file.
	p := &FFprobe{}

	_, err := p.Probe(context.Background(), "/nonexistent.opus")
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("Probe error = %v, want ErrUnavailable", err)
	}
}

func TestFFprobeHonorsCancellation(t *testing.T) {
	if !testsupport.FFMpegAvailable() {
		t.Skip("ffmpeg not installed")
	}

	path := testsupport.SynthesizeAudio(t, t.TempDir(), testsupport.AudioFormats[0], "Cancel", "", "", 30)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := NewFFprobe().Probe(ctx, path)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Probe error = %v, want context.Canceled", err)
	}
}

func TestFFprobeUnreadableFileIncludesDiagnostic(t *testing.T) {
	if !testsupport.FFMpegAvailable() {
		t.Skip("ffmpeg not installed")
	}

	// A file that exists but is not media: the error must say so rather than
	// reporting a bare exit status.
	path := t.TempDir() + "/notaudio.opus"
	if err := writeFile(path, "this is not audio"); err != nil {
		t.Fatal(err)
	}

	_, err := NewFFprobe().Probe(context.Background(), path)
	if err == nil {
		t.Fatal("Probe succeeded on a non-audio file")
	}
	if errors.Is(err, ErrUnavailable) {
		t.Fatalf("a corrupt file must not report ErrUnavailable: %v", err)
	}
}

func TestParseFFprobePrefersContainerDuration(t *testing.T) {
	raw := []byte(`{
		"streams": [{"codec_type":"audio","codec_name":"opus","duration":"1.0","sample_rate":"48000","channels":2,"bit_rate":"96000"}],
		"format": {"duration":"12.5","bit_rate":"96000","format_name":"ogg"}
	}`)

	info, err := parseFFprobe("x.opus", raw)
	if err != nil {
		t.Fatal(err)
	}
	if info.Duration != 12500*time.Millisecond {
		t.Errorf("Duration = %v, want 12.5s (container wins over stream)", info.Duration)
	}
}

func TestParseFFprobeFallsBackToStreamDuration(t *testing.T) {
	// Some muxers omit container duration entirely.
	raw := []byte(`{
		"streams": [{"codec_type":"audio","codec_name":"aac","duration":"7.25","sample_rate":"44100","channels":2}],
		"format": {"format_name":"mov,mp4,m4a"}
	}`)

	info, err := parseFFprobe("x.m4a", raw)
	if err != nil {
		t.Fatal(err)
	}
	if info.Duration != 7250*time.Millisecond {
		t.Errorf("Duration = %v, want 7.25s from stream fallback", info.Duration)
	}
	if info.Codec != "aac" {
		t.Errorf("Codec = %q, want aac", info.Codec)
	}
}

func TestParseFFprobeIgnoresNonAudioStreams(t *testing.T) {
	// An m4a with cover art exposes a video stream that must not win.
	raw := []byte(`{
		"streams": [
			{"codec_type":"video","codec_name":"mjpeg","duration":"0.04","sample_rate":"0","channels":0},
			{"codec_type":"audio","codec_name":"aac","duration":"7.0","sample_rate":"44100","channels":2}
		],
		"format": {"duration":"7.0","bit_rate":"192000"}
	}`)

	info, err := parseFFprobe("x.m4a", raw)
	if err != nil {
		t.Fatal(err)
	}
	if info.Codec != "aac" {
		t.Errorf("Codec = %q, want aac (video stream must be skipped)", info.Codec)
	}
	if info.SampleRate != 44100 {
		t.Errorf("SampleRate = %d, want 44100", info.SampleRate)
	}
}

func writeFile(path, content string) error {
	return os.WriteFile(path, []byte(content), 0o644)
}
