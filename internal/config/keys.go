package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

// keySpec describes one user-configurable key: its dotted TOML path, how to
// validate a candidate value, and how to read it back from a Config.
type keySpec struct {
	name     string
	validate func(string) error
	resolve  func(Config) string
}

var keyRegistry = []keySpec{
	{
		name:     "library_dir",
		validate: validateAbsPath,
		resolve:  func(c Config) string { return c.LibraryDir },
	},
	{
		name:     "staging_dir",
		validate: validateAbsPath,
		resolve:  func(c Config) string { return c.StagingDir },
	},
	{
		name:     "download.delay",
		validate: validateDelay,
		resolve:  func(c Config) string { return c.DownloadDelay },
	},
	{
		name:     "download.cookies_file",
		validate: validateAbsPath,
		resolve:  func(c Config) string { return c.CookiesFile },
	},
}

// Keys returns the names of all configurable keys.
func Keys() []string {
	names := make([]string, 0, len(keyRegistry))
	for _, k := range keyRegistry {
		names = append(names, k.name)
	}

	return names
}

func lookupKey(name string) (keySpec, bool) {
	for _, k := range keyRegistry {
		if k.name == name {
			return k, true
		}
	}

	return keySpec{}, false
}

// Resolved returns the effective value of a key and which layer supplied it.
func (c Config) Resolved(key string) (value string, source Source, err error) {
	spec, ok := lookupKey(key)
	if !ok {
		return "", SourceUnset, fmt.Errorf("unknown key %q (see `yci config list`)", key)
	}

	return spec.resolve(c), c.sources[key], nil
}

// Set validates value for key and persists it to the resolved config file,
// creating the file (and its directory) if needed. The write preserves keys
// this package does not model, but drops comments — TOML marshaling cannot
// represent them.
func Set(env LookupFunc, key, value string) error {
	spec, ok := lookupKey(key)
	if !ok {
		return fmt.Errorf("unknown key %q (see `yci config list`)", key)
	}

	if err := spec.validate(value); err != nil {
		return err
	}

	path, _, err := DiscoverPath(env)
	if err != nil {
		return err
	}

	doc := map[string]any{}
	if bytes, readErr := os.ReadFile(path); readErr == nil {
		if _, decodeErr := toml.Decode(string(bytes), &doc); decodeErr != nil {
			return fmt.Errorf("parse existing config file %q: %w", path, decodeErr)
		}
	} else if !os.IsNotExist(readErr) {
		return fmt.Errorf("read config file %q: %w", path, readErr)
	}

	parts := strings.Split(spec.name, ".")
	table := doc
	for _, p := range parts[:len(parts)-1] {
		next, ok := table[p].(map[string]any)
		if !ok {
			next = map[string]any{}
			table[p] = next
		}
		table = next
	}
	table[parts[len(parts)-1]] = value

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}

	var buf strings.Builder
	if err := toml.NewEncoder(&buf).Encode(doc); err != nil {
		return fmt.Errorf("encode config file %q: %w", path, err)
	}

	if err := os.WriteFile(path, []byte(buf.String()), 0o600); err != nil {
		return fmt.Errorf("write config file %q: %w", path, err)
	}

	return nil
}

func validateAbsPath(v string) error {
	if !filepath.IsAbs(filepath.FromSlash(v)) {
		return fmt.Errorf("%q must be an absolute path", v)
	}

	return nil
}

func validateDelay(v string) error {
	if _, _, err := ParseDelay(v); err != nil {
		return err
	}

	return nil
}
