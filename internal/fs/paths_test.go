package fs

import (
	"os"
	"path/filepath"
	"testing"
)

// TestEnsureDirsCreatesBothRoots is the TD-45 regression: a fresh install had
// no staging directory, and Enqueue lists staging on every download, so the
// first `download <url>` failed with a raw path error instead of the command
// just working.
func TestEnsureDirsCreatesBothRoots(t *testing.T) {
	library := filepath.Join(t.TempDir(), "Music", "Managed")
	staging := filepath.Join(t.TempDir(), "Music", "Staging")

	paths := NewAppPaths(t.TempDir(), library, staging)
	if err := EnsureDirs(paths); err != nil {
		t.Fatalf("EnsureDirs: %v", err)
	}

	for name, dir := range map[string]string{"library": library, "staging": staging} {
		info, err := os.Stat(dir)
		if err != nil {
			t.Fatalf("%s directory was not created: %v", name, err)
		}
		if !info.IsDir() {
			t.Fatalf("%s is not a directory", name)
		}

		// AGENTS.md fixes library directories at 0755 so another device can
		// read them. MkdirAll applies the mode to what it creates, so this
		// is the mode a fresh install actually gets.
		if perm := info.Mode().Perm(); perm != DirPerm {
			t.Errorf("%s mode = %o, want %o", name, perm, DirPerm)
		}
	}
}

// A pre-existing directory is the user's, not ours: EnsureDirs must not
// retune its permissions the way MkdirAll would if it were to recreate it.
func TestEnsureDirsLeavesAnExistingDirectoryAlone(t *testing.T) {
	library := filepath.Join(t.TempDir(), "Managed")
	if err := os.MkdirAll(library, 0o700); err != nil {
		t.Fatal(err)
	}

	if err := EnsureDirs(NewAppPaths(t.TempDir(), library, filepath.Join(t.TempDir(), "Staging"))); err != nil {
		t.Fatalf("EnsureDirs: %v", err)
	}

	info, err := os.Stat(library)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o700 {
		t.Errorf("mode = %o, want 0700: an existing directory was retuned", perm)
	}
}
