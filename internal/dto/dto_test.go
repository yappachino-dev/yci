package dto

import (
	"encoding/json"
	"testing"
)

// TestOutputSchemaIsStable pins the published shape of the machine-readable
// output.
//
// The DTOs are hand-written precisely so the schema does not track internal
// Go field names. That only holds if the keys are asserted here: a renamed
// field would otherwise silently change what every script reading yci's
// output sees, with nothing failing.
func TestOutputSchemaIsStable(t *testing.T) {
	cases := []struct {
		name  string
		value any
		want  []string
	}{
		{"track", Track{}, []string{"id", "title", "album", "artist_id", "source_id", "source", "relative_path", "duration_sec"}},
		{"artist", Artist{}, []string{"id", "name", "folder_id"}},
		{"folder", Folder{}, []string{"id", "name", "parent_id", "depth"}},
		{"queue", QueueEntry{}, []string{"id", "source_id", "status", "attempts", "error_msg", "error_kind"}},
		{"playlist", Playlist{}, []string{"id", "name", "seed_track_id", "fragment_tokens", "fragment_mode", "fragment_excludes"}},
		{"playlist entry", PlaylistEntry{}, []string{"position", "track_id", "title", "duration_sec"}},
		{"source", Source{}, []string{"name", "url", "channels", "linked_channels", "unlinked_count"}},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			raw, err := json.Marshal(c.value)
			if err != nil {
				t.Fatal(err)
			}

			var got map[string]json.RawMessage
			if err := json.Unmarshal(raw, &got); err != nil {
				t.Fatal(err)
			}

			if len(got) != len(c.want) {
				t.Errorf("%s has %d keys, want %d: %v", c.name, len(got), len(c.want), keysOf(got))
			}

			for _, key := range c.want {
				if _, ok := got[key]; !ok {
					t.Errorf("%s is missing key %q; got %v", c.name, key, keysOf(got))
				}
			}
		})
	}
}

func keysOf(m map[string]json.RawMessage) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}

	return keys
}
