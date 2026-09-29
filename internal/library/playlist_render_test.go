package library

import (
	"strings"
	"testing"
)

// m3uPathLines returns the lines a player would read as file paths, which is
// every line that is not a comment.
func m3uPathLines(m3u string) []string {
	var paths []string
	for _, line := range strings.Split(m3u, "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		paths = append(paths, line)
	}

	return paths
}

// TestRenderM3UKeepsOneLinePerTrack is the regression test for a real
// artifact defect: a track title containing a newline ended the #EXTINF line
// early, and the rest of the title was left in the file as what a player
// reads as a path. Tag values are the input and multi-line ones occur, so
// this was reachable by an ordinary import.
func TestRenderM3UKeepsOneLinePerTrack(t *testing.T) {
	entries := []PlaylistEntry{
		{Duration: 100, Track: Track{Title: "Real Song", RelativePath: "a/b.mp3"}},
		{Duration: 200, Track: Track{
			Title:        "Two\nLines / Performer\n/etc/passwd",
			RelativePath: "c/d.opus",
		}},
		{Duration: 300, Track: Track{
			Title:        "CR\rand U+2028 line separators",
			RelativePath: "e/f.flac",
		}},
	}

	out := RenderM3U("p", entries, "../")

	want := []string{"../a/b.mp3", "../c/d.opus", "../e/f.flac"}
	got := m3uPathLines(out)
	if len(got) != len(want) {
		t.Fatalf("player would read %d path line(s), want %d:\n%s", len(got), len(want), out)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("path line %d = %q, want %q", i, got[i], want[i])
		}
	}

	// The title text must survive, flattened, rather than being dropped.
	if !strings.Contains(out, "Two Lines / Performer /etc/passwd") {
		t.Errorf("title text was lost rather than flattened:\n%s", out)
	}
	if !strings.Contains(out, "CR and U+2028 line separators") {
		t.Errorf("CR or U+2028 was not flattened:\n%s", out)
	}
}

func TestRenderM3UFlattensPlaylistName(t *testing.T) {
	out := RenderM3U("my\nplaylist", nil, "../")

	if got := len(m3uPathLines(out)); got != 0 {
		t.Errorf("a multi-line playlist name produced %d path line(s):\n%s", got, out)
	}
	if !strings.HasPrefix(out, "#EXTM3U\n#PLAYLIST:my playlist\n") {
		t.Errorf("playlist name not flattened:\n%q", out)
	}
}

func TestRenderM3UKeepsOrdinaryOutputIntact(t *testing.T) {
	entries := []PlaylistEntry{
		{Duration: 282, Track: Track{Title: "星街すいせい - Cry Baby", RelativePath: "星街すいせい/cry.opus"}},
	}
	out := RenderM3U("Suisei", entries, "../")
	want := "#EXTM3U\n#PLAYLIST:Suisei\n#EXTINF:282,星街すいせい - Cry Baby\n../星街すいせい/cry.opus\n"
	if out != want {
		t.Errorf("RenderM3U changed ordinary output:\ngot  %q\nwant %q", out, want)
	}
}
