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
func RenderM3U(name string, entries []PlaylistEntry, relPrefix string) string {
	var b strings.Builder

	b.WriteString("#EXTM3U\n")
	b.WriteString(fmt.Sprintf("#PLAYLIST:%s\n", name))

	for _, e := range entries {
		b.WriteString(fmt.Sprintf("#EXTINF:%d,%s\n", e.Duration, e.Track.Title))
		b.WriteString(relPrefix + string(e.Track.RelativePath) + "\n")
	}

	return b.String()
}
