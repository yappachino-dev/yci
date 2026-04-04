package library

import (
	"fmt"
	"os"
	"path/filepath"
)

type AppPaths struct {
	Config  string
	Library string
	Temp    string
}

func GetPaths() (AppPaths, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return AppPaths{}, fmt.Errorf("couldn't find home directory: %w", err)
	}

	conf, err := os.UserConfigDir()
	if err != nil {
		return AppPaths{}, fmt.Errorf("couldn't find config directory %w", err)
	}

	return AppPaths{
		Config:  filepath.Join(conf, "yappachino", "playlist-manager"),
		Library: filepath.Join(home, "Music", "Managed"),
		Temp:    filepath.Join(home, "Music", "tmp"),
	}, nil
}
