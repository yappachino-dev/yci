package library

import (
	"testing"
)

func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		tree    folderTree
		wantErr bool
	}{
		{
			name: "ValidTree",
			tree: folderTree{
				{"artists", root, "Artists"},
				{"ado", "artists", "Ado"},
			},
			wantErr: false,
		},
		{
			name: "OrphanedFolder",
			tree: folderTree{
				{"ado", "missing_parent", "Ado"},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.tree.validate()

			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestGetPath(t *testing.T) {
	ft := folderTree{
		{"artists", root, "Artists"},
		{"ado", "artists", "Ado"},
		{"vt", "artists", "VT"},
		{"hololive", "vt", "Hololive"},
	}

	fm := ft.toMap()

	tests := []struct {
		name     string
		id       string
		expected string
		wantErr  bool
	}{
		{"InvalidID", "unknown", "", true},
		{"RootLevel", "artists", "Artists", false},
		{"DeepLevel", "ado", "Artists/Ado", false},
		{"DeeperLevel", "hololive", "Artists/VT/Hololive", false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result, err := fm.getPath(test.id)

			if (err != nil) != test.wantErr {
				t.Fatalf("error = %v, wantErr %v", err, test.wantErr)
			}

			if !test.wantErr && result != test.expected {
				t.Errorf("got %q, want %q", result, test.expected)
			}
		})
	}
}
