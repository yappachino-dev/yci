package fs

import (
	"context"
	"os"
	"path/filepath"
)

// OSDiskMover applies filesystem operations synchronously using the os
// package. Contexts are accepted to satisfy db.DiskMover but ignored —
// plain filesystem calls cannot be cancelled. Errors are returned as
// returned by the os package (wrapped where noted); see the error
// contract on db.DiskMover.
type OSDiskMover struct{}

func NewOSDiskMover() *OSDiskMover {
	return &OSDiskMover{}
}

func (m *OSDiskMover) MoveFile(ctx context.Context, oldPath, newPath string) error {
	if err := os.MkdirAll(filepath.Dir(newPath), 0o755); err != nil {
		return err
	}

	return os.Rename(oldPath, newPath)
}

func (m *OSDiskMover) MoveDirectory(ctx context.Context, oldRootPath, newRootPath string) error {
	if err := os.MkdirAll(filepath.Dir(newRootPath), 0o755); err != nil {
		return err
	}

	return os.Rename(oldRootPath, newRootPath)
}

func (m *OSDiskMover) DeleteFile(ctx context.Context, path string) error {
	return os.Remove(path)
}
