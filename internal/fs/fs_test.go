package fs

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestSanitizeFilename(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "plain name unchanged", in: "Daft Punk", want: "Daft Punk"},
		{name: "slash replaced", in: "AC/DC", want: "AC_DC"},
		{name: "backslash replaced", in: `AC\DC`, want: "AC_DC"},
		{name: "all illegal characters replaced", in: `a/b\c?d%e*f:g|h"i<j>k`, want: "a_b_c_d_e_f_g_h_i_j_k"},
		{name: "surrounding whitespace trimmed", in: "  spaced  ", want: "spaced"},
		{name: "legal punctuation kept", in: "Björk (1997) [live]", want: "Björk (1997) [live]"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := SanitizeFilename(tt.in); got != tt.want {
				t.Errorf("SanitizeFilename(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestOSDiskReaderFiltersDirectories(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "subdir"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"one.opus", "two.opus"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	files, err := NewOSDiskReader().ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir returned error: %v", err)
	}

	if len(files) != 2 || files[0] != "one.opus" || files[1] != "two.opus" {
		t.Errorf("files = %v, want [one.opus two.opus]", files)
	}
}

func TestOSDiskReaderMissingDirectory(t *testing.T) {
	_, err := NewOSDiskReader().ReadDir(filepath.Join(t.TempDir(), "nope"))
	if err == nil || !errors.Is(err, os.ErrNotExist) {
		t.Errorf("error = %v, want wrapped not-exist error naming the path", err)
	}
}

func TestOSDiskMoverMoveAndDelete(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src.opus")
	dst := filepath.Join(root, "nested", "dst.opus")

	if err := os.WriteFile(src, []byte("audio"), 0o644); err != nil {
		t.Fatal(err)
	}

	mover := NewOSDiskMover()

	if err := mover.MoveFile(t.Context(), src, dst); err != nil {
		t.Fatalf("MoveFile returned error: %v", err)
	}
	if _, err := os.Stat(src); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("source still present after move: %v", err)
	}
	data, err := os.ReadFile(dst)
	if err != nil || string(data) != "audio" {
		t.Errorf("destination content = %q, err = %v", data, err)
	}

	if err := mover.DeleteFile(t.Context(), dst); err != nil {
		t.Fatalf("DeleteFile returned error: %v", err)
	}
	if _, err := os.Stat(dst); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("file still present after delete: %v", err)
	}
}
