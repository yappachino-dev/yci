package cli

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/yappachino-dev/yci/internal/app"
	"github.com/yappachino-dev/yci/internal/library"
)

func seedNTtracks(t *testing.T, c *app.Container, n int) {
	t.Helper()

	folder, err := c.FolderService.Create(t.Context(), "", library.FolderID(library.RootID), library.FolderName("VT"))
	if err != nil {
		t.Fatalf("create folder: %v", err)
	}
	artist, err := c.ArtistService.Create(t.Context(), "", folder.ID, library.ArtistName("Someone"))
	if err != nil {
		t.Fatalf("create artist: %v", err)
	}

	for i := range n {
		if _, err := c.TrackService.Create(t.Context(), library.TrackCreateParams{
			ID:           library.TrackID(fmt.Sprintf("5ead5f5e-%04d-4bca-b300-600bab4935aa", i)),
			Title:        fmt.Sprintf("Track %02d", i),
			RelativePath: library.TrackPath(fmt.Sprintf("vt/%02d.opus", i)),
			ArtistID:     artist.ID,
		}); err != nil {
			t.Fatalf("create track %d: %v", i, err)
		}
	}
}

// TestTrackListLimitsAndReportsTotal is the point of --limit: a shortened
// list must say so. Silently capping the output would look like a library
// that only owns 50 tracks.
func TestTrackListLimitsAndReportsTotal(t *testing.T) {
	c := newTestContainer(t)
	// More tracks than the default limit, so the default itself truncates and
	// is tested rather than assumed.
	total := defaultTrackLimit + 10
	seedNTtracks(t, c, total)

	tests := []struct {
		name       string
		limit      string
		wantRows   int
		wantFooter string
	}{
		{"default", "", defaultTrackLimit, fmt.Sprintf("Showing %d of %d tracks.", defaultTrackLimit, total)},
		{"explicit limit", "--limit=3", 3, fmt.Sprintf("Showing 3 of %d tracks.", total)},
		{"limit zero shows everything", "--limit=0", total, fmt.Sprintf("%d tracks.", total)},
		{"limit above total is not truncation", "--limit=1000", total, fmt.Sprintf("%d tracks.", total)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var args []string
			if tt.limit != "" {
				args = append(args, tt.limit)
			}

			cmd, out := newListCmd(t, c, args...)
			if err := listTracks(cmd.Context(), cmd); err != nil {
				t.Fatalf("listTracks: %v", err)
			}

			rows := dataRows(out.String())
			if len(rows) != tt.wantRows {
				t.Errorf("showed %d rows, want %d", len(rows), tt.wantRows)
			}
			if !strings.Contains(out.String(), tt.wantFooter) {
				t.Errorf("footer missing %q in:\n%s", tt.wantFooter, out.String())
			}
		})
	}
}

// TestTrackListRejectsNegativeLimit: a negative limit silently meaning "all"
// would be the worst reading of the flag.
func TestTrackListRejectsNegativeLimit(t *testing.T) {
	c := newTestContainer(t)
	seedNTtracks(t, c, 3)

	cmd, _ := newListCmd(t, c, "--limit=-1")
	err := listTracks(cmd.Context(), cmd)
	if err == nil {
		t.Fatal("listTracks(--limit=-1) = nil, want an error")
	}
	if !strings.Contains(err.Error(), "must not be negative") {
		t.Errorf("error = %q, want it to explain the limit", err)
	}
}

// TestTrackListJSONRespectsLimit: a script paging with --limit must get the
// page it asked for, not the whole library.
func TestTrackListJSONRespectsLimit(t *testing.T) {
	c := newTestContainer(t)
	seedNTtracks(t, c, 8)

	cmd, out := newListCmd(t, c, "--json", "--limit=4")
	if err := listTracks(cmd.Context(), cmd); err != nil {
		t.Fatalf("listTracks: %v", err)
	}

	var got []jsonTrack
	if err := json.Unmarshal([]byte(out.String()), &got); err != nil {
		t.Fatalf("output is not a JSON array: %v\n%s", err, out.String())
	}
	if len(got) != 4 {
		t.Errorf("got %d rows, want 4", len(got))
	}
}

// TestTrackListKeepsSourceIDInJSONOnly guards the column removal: the human
// table drops SOURCE_ID to fit a terminal, but a script must not lose it.
func TestTrackListKeepsSourceIDInJSONOnly(t *testing.T) {
	c := newTestContainer(t)
	seedNTtracks(t, c, 2)

	cmd, out := newListCmd(t, c)
	if err := listTracks(cmd.Context(), cmd); err != nil {
		t.Fatalf("listTracks: %v", err)
	}
	if strings.Contains(out.String(), "SOURCE_ID") {
		t.Errorf("human table still has SOURCE_ID:\n%s", out.String())
	}

	cmd, out = newListCmd(t, c, "--json")
	if err := listTracks(cmd.Context(), cmd); err != nil {
		t.Fatalf("listTracks: %v", err)
	}
	var got []jsonTrack
	if err := json.Unmarshal([]byte(out.String()), &got); err != nil {
		t.Fatal(err)
	}
	if len(got) == 0 {
		t.Fatal("no rows in JSON output")
	}
	// The key must exist even when empty: no omitempty, per style-guide 7a.
	var raw []map[string]any
	if err := json.Unmarshal([]byte(out.String()), &raw); err != nil {
		t.Fatal(err)
	}
	if _, ok := raw[0]["source_id"]; !ok {
		t.Errorf("source_id missing from JSON object: %v", raw[0])
	}
}

// dataRows returns the table body: the lines after the header and its dash
// rule, stopping at the blank line that separates the footer.
func dataRows(s string) []string {
	var rows []string
	for i, line := range strings.Split(s, "\n") {
		if i < 2 {
			continue
		}
		if strings.TrimSpace(line) == "" {
			break
		}
		rows = append(rows, line)
	}
	return rows
}
