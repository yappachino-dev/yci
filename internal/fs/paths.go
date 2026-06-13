package fs

import (
	"fmt"
	"os"
	"path/filepath"
)

type AppPaths struct {
	config  string
	library string
	staging string
}

func GetPaths() (AppPaths, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return AppPaths{}, fmt.Errorf("resolve home directory: %w", err)
	}

	conf, err := os.UserConfigDir()
	if err != nil {
		return AppPaths{}, fmt.Errorf("resolve config directory: %w", err)
	}

	return AppPaths{
		config:  filepath.Join(conf, "yci"),
		library: filepath.Join(home, "Music", "Managed"),
		staging: filepath.Join(home, "Music", "Staging"),
	}, nil
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
