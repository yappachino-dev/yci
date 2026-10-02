package library

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// RenameResult reports what a rename did, including what happened to the
// exported artifact.
type RenameResult struct {
	OldName string
	NewName string
	// ExportMoved reports whether an existing .m3u was renamed to follow the
	// playlist.
	ExportMoved bool
	// OrphanedExport is a .m3u that still exists under the old name. It is
	// reported rather than hidden, because Syncthing will ship it to the
	// phone where it will never update again.
	OrphanedExport string
}

// Rename changes a playlist's name and follows its exported file.
//
// Renaming only the database row would be the worse half of a rename: the
// exported .m3u is what the phone actually reads, so a row-only rename
// leaves a playlist on the other device under the old name that no future
// export will ever update.
//
// The database is the source of truth, so it changes first and the file move
// is best-effort. A failed move is recoverable with one export; the reverse
// order could leave a file with no playlist behind it.
func (s *PlaylistService) Rename(ctx context.Context, oldName, newName string) (RenameResult, error) {
	var result RenameResult

	trimmed := strings.TrimSpace(newName)
	if trimmed == "" {
		return result, fmt.Errorf("playlist name cannot be empty")
	}

	from := strings.TrimSpace(oldName)

	p, err := s.repo.ByName(ctx, from)
	if err != nil {
		return result, fmt.Errorf("find playlist %q: %w", from, err)
	}

	// A case-only rename is allowed through: SQLite excludes the row being
	// updated from its own UNIQUE check, so "gym" -> "Gym" is a real change
	// the user asked for, not a self-conflict.
	if err := s.repo.Rename(ctx, p.ID, trimmed); err != nil {
		return result, err
	}

	result.OldName, result.NewName = p.Name, trimmed

	oldPath, newPath := s.exportPaths(p.Name, trimmed)
	if oldPath == "" {
		return result, nil
	}

	if _, err := os.Stat(oldPath); err != nil {
		// Nothing was ever exported, so there is nothing to follow.
		return result, nil
	}

	if _, err := os.Stat(newPath); err == nil {
		// Refuse to clobber: two playlists can sanitize to one filename,
		// and export-all already refuses to overwrite in that case.
		result.OrphanedExport = oldPath
		return result, nil
	}

	if err := os.Rename(oldPath, newPath); err != nil {
		result.OrphanedExport = oldPath
		return result, fmt.Errorf("rename exported playlist file: %w", err)
	}

	result.ExportMoved = true

	return result, nil
}

// SetMatchMode changes how a seeded playlist's tokens combine.
//
// It does not re-derive membership: changing the mode changes what the
// recipe means, and applying that meaning is `playlist reseed`. Doing both
// here would make a mode switch a silent rewrite of membership.
func (s *PlaylistService) SetMatchMode(ctx context.Context, name string, mode MatchMode) error {
	p, err := s.repo.ByName(ctx, strings.TrimSpace(name))
	if err != nil {
		return fmt.Errorf("find playlist %q: %w", name, err)
	}

	if !p.HasRecipe() {
		return &NoRecipeError{Playlist: p.Name}
	}

	if p.MatchMode == mode {
		return nil
	}

	return s.repo.SetMode(ctx, p.ID, mode)
}

// SetExcludes replaces a recipe's exclusion list.
//
// Changing the exclusions does not alter membership; it changes what the
// recipe means. The caller is expected to follow with a reseed, which shows
// the resulting diff, because the two together are the point: a person
// decides what belongs, then reviews what that means for membership.
func (s *PlaylistService) SetExcludes(ctx context.Context, name string, excludes []TrackID) error {
	p, err := s.repo.ByName(ctx, strings.TrimSpace(name))
	if err != nil {
		return fmt.Errorf("find playlist %q: %w", name, err)
	}

	if !p.HasRecipe() {
		return &NoRecipeError{Playlist: p.Name}
	}

	encoded, err := encodeExcludes(excludes)
	if err != nil {
		return err
	}

	return s.repo.SetExcludes(ctx, p.ID, encoded)
}

// exportPaths returns the old and new .m3u locations, or empty when the two
// names sanitize to the same filename.
func (s *PlaylistService) exportPaths(oldName, newName string) (string, string) {
	dir := filepath.Join(s.paths.LibraryDir(), ExportDir)

	oldPath := filepath.Join(dir, SanitizeForExport(oldName)+".m3u")
	newPath := filepath.Join(dir, SanitizeForExport(newName)+".m3u")

	if oldPath == newPath {
		return "", ""
	}
	return oldPath, newPath
}

// orphanedExport reports an old .m3u that a rename could not follow, or "".
func (s *PlaylistService) orphanedExport(oldName, newName string) string {
	oldPath, _ := s.exportPaths(oldName, newName)
	if oldPath == "" {
		return ""
	}
	if _, err := os.Stat(oldPath); err != nil {
		return ""
	}
	return oldPath
}
