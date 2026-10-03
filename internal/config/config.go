// Package config resolves application settings by layering environment
// variables over an optional TOML file over built-in defaults, and provides
// validated read/write access to individual keys.
package config

import (
	"cmp"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"
)

// Env var names; changing these is a user-facing breaking change.
const (
	EnvConfigPath      = "YCI_CONFIG_PATH"
	EnvLibraryPath     = "YCI_LIBRARY_PATH"
	EnvStagingPath     = "YCI_STAGING_PATH"
	EnvDownloadDelay   = "YCI_DOWNLOAD_DELAY"
	EnvDownloadCookies = "YCI_DOWNLOAD_COOKIES"
)

// DefaultDelay is the built-in randomized delay between downloads when
// neither config nor environment supply one.
const DefaultDelay = "5:30"

// DownloadSection mirrors the [download] table in the TOML file.
type DownloadSection struct {
	Delay       string `toml:"delay"`
	CookiesFile string `toml:"cookies_file"`
}

// File mirrors the user-editable TOML structure. Empty values mean unset.
type File struct {
	Library  string          `toml:"library_dir"`
	Staging  string          `toml:"staging_dir"`
	Download DownloadSection `toml:"download"`
}

// Source labels which precedence layer supplied a value.
type Source string

const (
	SourceEnv     Source = "env"
	SourceFile    Source = "file"
	SourceDefault Source = "default"
	SourceUnset   Source = ""
)

// Config holds fully resolved application settings.
type Config struct {
	LibraryDir    string
	StagingDir    string
	DownloadDelay string
	CookiesFile   string

	dir     string
	sources map[string]Source
}

// ConfigDir is the directory containing the resolved config file. The
// database lives here too, so pointing YCI_CONFIG_PATH elsewhere relocates
// the whole profile.
func (c Config) ConfigDir() string {
	return c.dir
}

// LookupFunc reports the value of an environment variable, "" if unset.
type LookupFunc func(string) string

// DiscoverPath resolves the config file location and whether it was set
// explicitly via YCI_CONFIG_PATH.
func DiscoverPath(env LookupFunc) (path string, explicit bool, err error) {
	conf, err := os.UserConfigDir()
	if err != nil {
		return "", false, fmt.Errorf("resolve config directory: %w", err)
	}

	envPath := filepath.FromSlash(env(EnvConfigPath))
	path = filepath.Join(conf, "yci", "config.toml")
	if envPath != "" {
		path = envPath
	}

	return path, envPath != "", nil
}

// Load resolves paths: defaults < config file < environment. A missing
// config file at the default location means "no settings"; one explicitly
// named via YCI_CONFIG_PATH must exist.
func Load(env LookupFunc) (Config, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Config{}, fmt.Errorf("resolve home directory: %w", err)
	}

	cfgPath, explicit, err := DiscoverPath(env)
	if err != nil {
		return Config{}, err
	}

	fileCfg, err := readFile(cfgPath, explicit)
	if err != nil {
		return Config{}, err
	}

	c := Config{
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
		DownloadDelay: cmp.Or(
			env(EnvDownloadDelay),
			fileCfg.Download.Delay,
			DefaultDelay,
		),
		CookiesFile: cmp.Or(
			filepath.FromSlash(env(EnvDownloadCookies)),
			filepath.FromSlash(fileCfg.Download.CookiesFile),
		),
		dir:     filepath.Dir(cfgPath),
		sources: sourcesFor(env, fileCfg),
	}

	return c, nil
}

func sourcesFor(env LookupFunc, f File) map[string]Source {
	sources := make(map[string]Source, len(keyRegistry))
	for _, k := range keyRegistry {
		sources[k.name] = k.source(env, f)
	}

	return sources
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
	md, err := toml.Decode(string(bytes), &f)
	if err != nil {
		return File{}, fmt.Errorf("parse config file %q: %w", path, err)
	}

	if undecoded := md.Undecoded(); len(undecoded) > 0 {
		names := make([]string, 0, len(undecoded))
		for _, k := range undecoded {
			names = append(names, k.String())
		}

		return File{}, fmt.Errorf("unknown keys in config file %q: %s (see `yci config list`)", path, strings.Join(names, ", "))
	}

	return f, nil
}

// ParseDelay validates a min:max delay string and returns its halves.
func ParseDelay(s string) (minStr, maxStr string, err error) {
	parts := strings.Split(s, ":")
	if len(parts) != 2 {
		return "", "", fmt.Errorf("invalid delay format. Expected min:max (e.g., 5:30)")
	}

	for _, half := range parts {
		n, parseErr := strconv.Atoi(half)
		if parseErr != nil || n < 0 {
			return "", "", fmt.Errorf("invalid delay %q: both components must be non-negative integers", s)
		}
	}

	return parts[0], parts[1], nil
}
