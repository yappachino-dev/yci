// Package config resolves application settings by layering environment
// variables over an optional TOML file over built-in defaults.
package config

import (
	"cmp"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

// Env var names; changing these is a user-facing breaking change.
const (
	EnvConfigPath  = "YCI_CONFIG_PATH"
	EnvLibraryPath = "YCI_LIBRARY_PATH"
	EnvStagingPath = "YCI_STAGING_PATH"
)

// File mirrors the user-editable TOML structure. Empty values mean unset.
type File struct {
	Library string `toml:"library_dir"`
	Staging string `toml:"staging_dir"`
}

// Config holds fully resolved application paths.
type Config struct {
	LibraryDir string
	StagingDir string

	dir string
}

// ConfigDir is the directory containing the resolved config file. The
// database lives here too, so pointing YCI_CONFIG_PATH elsewhere relocates
// the whole profile.
func (c Config) ConfigDir() string {
	return c.dir
}

// LookupFunc reports the value of an environment variable, "" if unset.
type LookupFunc func(string) string

// Load resolves paths: defaults < config file < environment. A missing
// config file at the default location means "no settings"; one explicitly
// named via YCI_CONFIG_PATH must exist.
func Load(env LookupFunc) (Config, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Config{}, fmt.Errorf("resolve home directory: %w", err)
	}

	conf, err := os.UserConfigDir()
	if err != nil {
		return Config{}, fmt.Errorf("resolve config directory: %w", err)
	}

	envPath := filepath.FromSlash(env(EnvConfigPath))
	cfgPath := cmp.Or(envPath, filepath.Join(conf, "yci", "config.toml"))

	fileCfg, err := readFile(cfgPath, envPath != "")
	if err != nil {
		return Config{}, err
	}

	return Config{
		LibraryDir: cmp.Or(
			filepath.FromSlash(env(EnvLibraryPath)),
			filepath.FromSlash(fileCfg.Library),
			filepath.Join(home, "Music", "Managed"),
		),
		StagingDir: cmp.Or(
			filepath.FromSlash(env(EnvStagingPath)),
			filepath.FromSlash(fileCfg.Staging),
			filepath.Join(home, "Music", "Staging"),
		),
		dir: filepath.Dir(cfgPath),
	}, nil
}

func readFile(path string, explicit bool) (File, error) {
	bytes, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) && !explicit {
		return File{}, nil // first run: no config file yet
	}
	if err != nil {
		return File{}, fmt.Errorf("read config file %q: %w", path, err)
	}

	var f File
	if err := toml.Unmarshal(bytes, &f); err != nil {
		return File{}, fmt.Errorf("parse config file %q: %w", path, err)
	}

	return f, nil
}
