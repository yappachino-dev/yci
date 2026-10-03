package library

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/yappachino-dev/yci/internal/fs"
)

// PathRepair is one track whose stored path was rewritten to match disk.
type PathRepair struct {
	ID      TrackID
	Title   string
	OldPath TrackPath
	NewPath TrackPath
}

// PathReconcileResult reports what a reconciliation pass found.
type PathReconcileResult struct {
	Checked int
	Repairs []PathRepair
	// Missing counts tracks whose file is absent under both normalization
	// forms. Nothing is rewritten for these: the file is genuinely gone,
	// and inventing a path would hide that rather than fix it.
	Missing int
}

// ReconcilePaths rewrites stored track paths whose bytes differ from the file
// on disk.
//
// A stored path is only rewritten when the alternate normalization form is
// provably present, so this repairs normalization drift and never guesses.
// A dry run writes nothing.
func (s *TrackService) ReconcilePaths(ctx context.Context, dryRun bool) (PathReconcileResult, error) {
	tracks, err := s.All(ctx)
	if err != nil {
		return PathReconcileResult{}, fmt.Errorf("list tracks for path reconciliation: %w", err)
	}

	result := PathReconcileResult{Checked: len(tracks)}
	libraryDir := s.paths.LibraryDir()

	for _, t := range tracks {
		abs := filepath.Join(libraryDir, filepath.FromSlash(string(t.RelativePath)))

		found, statErr := fs.ResolveExisting(abs)

		switch {
		case statErr == nil:
		case errors.Is(statErr, os.ErrNotExist):
			result.Missing++

			continue
		default:
			return result, fmt.Errorf("stat %q: %w", abs, statErr)
		}

		if found == abs {
			continue
		}

		rel, err := filepath.Rel(libraryDir, found)
		if err != nil {
			return result, fmt.Errorf("make %q library-relative: %w", found, err)
		}

		result.Repairs = append(result.Repairs, PathRepair{
			ID:      t.ID,
			Title:   t.Title,
			OldPath: t.RelativePath,
			NewPath: TrackPath(filepath.ToSlash(rel)),
		})
	}

	if dryRun || len(result.Repairs) == 0 {
		return result, nil
	}

	moves := make([]TrackPathMove, 0, len(result.Repairs))
	for _, r := range result.Repairs {
		moves = append(moves, TrackPathMove{ID: r.ID, OldPath: r.OldPath, NewPath: r.NewPath})
	}

	if err := s.repo.UpdatePathsBatch(ctx, moves); err != nil {
		return result, fmt.Errorf("apply path repairs: %w", err)
	}

	return result, nil
}
