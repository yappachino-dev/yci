package library

import (
	"testing"
)

func TestGetPath(t *testing.T) {
	ft := FolderTree{
		{"artists", root, "Artists"},
		{"ado", "artists", "Ado"},
		{"vt", "artists", "VT"},
		{"hololive", "vt", "Hololive"},
	}

	fm := ft.ToMap()

	tests := []struct {
		name     string
		id       string
		expected string
	}{
		{"InvalidID", "unknown", ""},
		{"RootLevel", "artists", "Artists"},
		{"DeepLevel", "ado", "Artists/Ado"},
		{"DeeperLevel", "hololive", "Artists/VT/Hololive"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Run("FolderTree", func(t *testing.T) {
				if result := ft.GetPath(test.id); result != test.expected {
					t.Errorf("got %q, want %q", result, test.expected)
				}
			})

			t.Run("FolderMap", func(t *testing.T) {
				if result := fm.GetPath(test.id); result != test.expected {
					t.Errorf("got %q, want %q", result, test.expected)
				}
			})
		})
	}
}
