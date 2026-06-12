package fs

import (
	"context"
	"os"
	"path/filepath"
)

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
