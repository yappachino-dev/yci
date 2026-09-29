package library

import (
	"fmt"
	"strings"

	"github.com/yappachino-dev/yci/internal/fs"
)

// SanitizeForExport derives the .m3u base filename from a playlist name.
func SanitizeForExport(name string) string {
	return fs.SanitizeFilename(name)
}

// RenderM3U renders one playlist in extended M3U format. Track paths are
// joined onto relPrefix so they resolve relative to the exported file's
// directory ("../" when exporting into <library>/playlists/). Forward
// slashes are kept regardless of platform: both target consumers
// (termusic, Poweramp) expect URL-style separators, and the files travel
// across operating systems via sync.
//
// Line breaks are stripped from the playlist name and every title. M3U is a
// line-oriented format, so a newline inside a title would end the #EXTINF
// line early and leave the remainder of the title sitting in the file as
// what looks like a path — a phantom playlist entry pointing at a file that
// does not exist. Tag values are the input here and multi-line ones occur.
func RenderM3U(name string, entries []PlaylistEntry, relPrefix string) string {
	var b strings.Builder

	b.WriteString("#EXTM3U\n")
	b.WriteString("#PLAYLIST:" + singleLine(name) + "\n")

	for _, e := range entries {
		b.WriteString(fmt.Sprintf("#EXTINF:%d,%s\n", e.Duration, singleLine(e.Track.Title)))
		b.WriteString(relPrefix + singleLine(string(e.Track.RelativePath)) + "\n")
	}

	return b.String()
}

// singleLine flattens any line break, including CR and the Unicode
// line/paragraph separators, so one logical value stays one physical line.
func singleLine(s string) string {
	return strings.Map(func(r rune) rune {
		switch r {
		case '\n', '\r', '\u2028', '\u2029':
			return ' '
		default:
			return r
		}
	}, s)
}
