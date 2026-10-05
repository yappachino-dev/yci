package fs

import (
	"fmt"
	"os"
)

type AppPaths struct {
	config  string
	library string
	staging string
}

// NewAppPaths builds application paths from resolved configuration values.
func NewAppPaths(configDir, libraryDir, stagingDir string) AppPaths {
	return AppPaths{config: configDir, library: libraryDir, staging: stagingDir}
}

func (p AppPaths) ConfigDir() string {
	return p.config
}

func (p AppPaths) StagingDir() string {
	return p.staging
}

func (p AppPaths) LibraryDir() string {
	return p.library
}

// DirPerm is the mode for directories the library owns. AGENTS.md fixes this
// at 0755: readable by Syncthing on another device, and no group write.
const DirPerm = 0o755

// EnsureDirs creates the library and staging directories.
//
// The services assume both exist and read them on ordinary paths -- Enqueue
// lists staging to spot videos already downloaded -- so a fresh install with
// nothing created yet otherwise fails with a raw path error on the first
// download rather than on the first setup.
func EnsureDirs(paths AppPaths) error {
	for _, dir := range []string{paths.LibraryDir(), paths.StagingDir()} {
		if err := os.MkdirAll(dir, DirPerm); err != nil {
			return fmt.Errorf("create %s: %w", dir, err)
		}
	}

	return nil
}
