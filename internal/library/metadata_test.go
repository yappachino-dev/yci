package library

import (
	"testing"

	"github.com/yappachino-dev/yci/internal/testsupport"
)

// tagReadableFormats is the measured intersection: containers the import path
// accepts AND dhowden/tag can read titles from. WAV is absent because it is
// measured as unreadable, not merely unimplemented.
var tagReadableFormats = []testsupport.AudioFormat{
	AudioFormatByName("opus"),
	AudioFormatByName("ogg"),
	AudioFormatByName("flac"),
	AudioFormatByName("mp3"),
	AudioFormatByName("m4a"),
}

func AudioFormatByName(name string) testsupport.AudioFormat {
	for _, f := range testsupport.AudioFormats {
		if f.Name == name {
			return f
		}
	}
	panic("unknown audio format: " + name)
}

// TestTagMetadataReaderAcrossFormats is a compatibility measurement, not a
// wish: it records which containers dhowden/tag can actually read. A format
// that silently yields empty tags is worse than one that errors, because the
// import would proceed with a blank title.
func TestTagMetadataReaderAcrossFormats(t *testing.T) {
	if !testsupport.FFMpegAvailable() {
		t.Skip("ffmpeg not installed")
	}

	const (
		title  = "星街すいせい / 彗星"
		artist = "Mori Calliope"
		album  = "Cover Album"
	)

	for _, format := range tagReadableFormats {
		t.Run(format.Name, func(t *testing.T) {
			path := testsupport.SynthesizeAudio(t, t.TempDir(), format, title, artist, album, 2)

			meta, err := NewTagMetadataReader().Read(path)
			if err != nil {
				t.Fatalf("Read: %v", err)
			}

			if meta.Title != title {
				t.Errorf("Title = %q, want %q", meta.Title, title)
			}
			if meta.Artist != artist {
				t.Errorf("Artist = %q, want %q", meta.Artist, artist)
			}
		})
	}
}

// TestWAVIsUnreadableByTagReader pins the measurement that keeps WAV out of
// the import set. If a future dhowden release adds LIST/INFO support, this
// test fails and tells us to add .wav back to supportedImportExtensions.
func TestWAVIsUnreadableByTagReader(t *testing.T) {
	if !testsupport.FFMpegAvailable() {
		t.Skip("ffmpeg not installed")
	}

	path := testsupport.SynthesizeAudio(t, t.TempDir(), AudioFormatByName("wav"), "T", "A", "B", 1)

	if _, err := NewTagMetadataReader().Read(path); err == nil {
		t.Fatal("dhowden/tag now reads WAV; add .wav to supportedImportExtensions")
	}

	if isSupportedImportExtension(".wav") {
		t.Error(".wav is in the import set but the tag reader cannot read it")
	}
}

// TestSupportedImportExtensionsAllReadTags guards the invariant behind
// supportedImportExtensions: nothing may be accepted that the tag layer
// cannot handle.
func TestSupportedImportExtensionsAllReadTags(t *testing.T) {
	if !testsupport.FFMpegAvailable() {
		t.Skip("ffmpeg not installed")
	}

	for _, ext := range SupportedImportExtensions() {
		t.Run(ext, func(t *testing.T) {
			var format testsupport.AudioFormat
			for _, f := range testsupport.AudioFormats {
				if f.Ext == ext {
					format = f
				}
			}
			if format.Name == "" {
				t.Fatalf("import extension %q has no fixture in testsupport.AudioFormats", ext)
			}

			path := testsupport.SynthesizeAudio(t, t.TempDir(), format, "Readable", "A", "B", 1)

			meta, err := NewTagMetadataReader().Read(path)
			if err != nil {
				t.Fatalf("import set accepts %q but tags are unreadable: %v", ext, err)
			}
			if meta.Title != "Readable" {
				t.Errorf("Title = %q, want Readable", meta.Title)
			}
		})
	}
}

// TestTagMetadataReaderEmptyTagsIsNotSilent documents the failure mode worth
// guarding: a container whose tags cannot be read returns empty strings with
// no error, so a caller cannot distinguish "no tags" from "unsupported
// container" without inspecting the file itself.
func TestTagMetadataReaderReportsEmptyTagsVerbatim(t *testing.T) {
	if !testsupport.FFMpegAvailable() {
		t.Skip("ffmpeg not installed")
	}

	// A tone with no tags at all must come back empty, not defaulted or
	// guessed.
	path := testsupport.SynthesizeAudio(t, t.TempDir(), testsupport.AudioFormats[0], "", "", "", 1)

	meta, err := NewTagMetadataReader().Read(path)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}

	if meta.Title != "" || meta.Artist != "" {
		t.Errorf("expected empty tags, got title=%q artist=%q", meta.Title, meta.Artist)
	}
}

func TestTagMetadataReaderRejectsMissingFile(t *testing.T) {
	if _, err := NewTagMetadataReader().Read("/nonexistent/nope.opus"); err == nil {
		t.Fatal("Read succeeded on a missing file")
	}
}
