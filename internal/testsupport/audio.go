// Package testsupport builds real fixtures for tests that must exercise real
// file formats. Synthetic structs would not catch the class of bug that
// matters here: a tag library that cannot read a container, or a container
// that reports no duration.
package testsupport

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// AudioFormat is one encodable container/codec pair.
type AudioFormat struct {
	// Name is the format label used in subtest names.
	Name string
	// Ext is the file extension, including the dot.
	Ext string
	// Encoder is the ffmpeg encoder name.
	Encoder string
	// Lossless marks formats where the signal is not compressed.
	Lossless bool
	// ExtraArgs are appended to the ffmpeg command line.
	ExtraArgs []string
}

// AudioFormats are the formats the library is expected to handle: the one it
// currently imports, plus the ones plausible for a non-YouTube source.
var AudioFormats = []AudioFormat{
	{Name: "opus", Ext: ".opus", Encoder: "libopus", ExtraArgs: []string{"-b:a", "96k"}},
	{Name: "ogg", Ext: ".ogg", Encoder: "libvorbis", ExtraArgs: []string{"-b:a", "192k"}},
	{Name: "flac", Ext: ".flac", Encoder: "flac", Lossless: true},
	{Name: "mp3", Ext: ".mp3", Encoder: "libmp3lame", ExtraArgs: []string{"-b:a", "192k"}},
	{Name: "m4a", Ext: ".m4a", Encoder: "aac", ExtraArgs: []string{"-b:a", "192k"}},
	{Name: "wav", Ext: ".wav", Encoder: "pcm_s16le", Lossless: true},
}

// FFMpegAvailable reports whether ffmpeg is installed. Format tests skip
// rather than fail when it is absent, so the suite still runs on a machine
// without a media toolchain.
func FFMpegAvailable() bool {
	_, err := exec.LookPath("ffmpeg")
	return err == nil
}

// SynthesizeAudio writes a real audio file of the given format, carrying real
// tags and lasting approximately the requested number of seconds.
//
// It is deliberately not a silent stub: every assertion downstream depends on
// the bytes being a genuine file of that container.
func SynthesizeAudio(t *testing.T, dir string, format AudioFormat, title, artist, album string, seconds int) string {
	t.Helper()

	if !FFMpegAvailable() {
		t.Skip("ffmpeg not installed; cannot synthesize audio fixtures")
	}

	path := filepath.Join(dir, sanitizeFixtureName(title)+format.Ext)

	// A sine tone is enough: the assertions care about container and tag
	// handling, not about the audio itself.
	args := []string{
		"-hide_banner", "-loglevel", "error", "-y",
		"-f", "lavfi",
		"-i", fmt.Sprintf("sine=frequency=440:sample_rate=48000:duration=%d", seconds),
		"-ac", "2",
		"-c:a", format.Encoder,
	}
	args = append(args, format.ExtraArgs...)

	if title != "" {
		args = append(args, "-metadata", "title="+title)
	}
	if artist != "" {
		args = append(args, "-metadata", "artist="+artist)
	}
	if album != "" {
		args = append(args, "-metadata", "album="+album)
	}

	args = append(args, path)

	out, err := exec.Command("ffmpeg", args...).CombinedOutput()
	if err != nil {
		t.Fatalf("synthesize %s: %v\n%s", format.Name, err, out)
	}

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("synthesize %s produced no file: %v", format.Name, err)
	}

	return path
}

// sanitizeFixtureName keeps titles usable as filenames without disturbing
// non-ASCII content, which some of these tests depend on.
func sanitizeFixtureName(s string) string {
	if s == "" {
		return "track"
	}

	out := make([]rune, 0, len(s))
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r > 0x7f:
			out = append(out, r)
		case r == ' ' || r == '-' || r == '_':
			out = append(out, '-')
		}
	}

	if len(out) == 0 {
		return "track"
	}
	return string(out)
}
