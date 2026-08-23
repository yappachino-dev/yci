package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// isolate points HOME and XDG_CONFIG_HOME at a fresh temp dir so tests
// never see (or touch) real user configuration.
func isolate(t *testing.T) string {
	t.Helper()

	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))

	return home
}

func writeConfig(t *testing.T, path, content string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create config dir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write config file: %v", err)
	}
}

func TestLoadDefaultsWithoutConfigFile(t *testing.T) {
	home := isolate(t)

	cfg, err := Load(os.Getenv)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	wantLib := filepath.Join(home, "Music", "Managed")
	wantStaging := filepath.Join(home, "Music", "Staging")

	if cfg.LibraryDir != wantLib {
		t.Errorf("LibraryDir = %q, want %q", cfg.LibraryDir, wantLib)
	}
	if cfg.StagingDir != wantStaging {
		t.Errorf("StagingDir = %q, want %q", cfg.StagingDir, wantStaging)
	}
	if want := filepath.Join(home, ".config", "yci"); cfg.ConfigDir() != want {
		t.Errorf("ConfigDir = %q, want %q", cfg.ConfigDir(), want)
	}

	if cfg.DownloadDelay != DefaultDelay {
		t.Errorf("DownloadDelay = %q, want %q", cfg.DownloadDelay, DefaultDelay)
	}
	if cfg.CookiesFile != "" {
		t.Errorf("CookiesFile = %q, want empty", cfg.CookiesFile)
	}
}

func TestLoadFromFile(t *testing.T) {
	isolate(t)

	writeConfig(t, filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "yci", "config.toml"),
		"library_dir = \"/media/music\"\nstaging_dir = \"/tmp/inbox\"\n")

	cfg, err := Load(os.Getenv)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.LibraryDir != "/media/music" {
		t.Errorf("LibraryDir = %q, want /media/music", cfg.LibraryDir)
	}
	if cfg.StagingDir != "/tmp/inbox" {
		t.Errorf("StagingDir = %q, want /tmp/inbox", cfg.StagingDir)
	}
}

func TestLoadFromDownloadSection(t *testing.T) {
	isolate(t)

	writeConfig(t, filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "yci", "config.toml"),
		"[download]\ndelay = \"60:180\"\ncookies_file = \"/tmp/cookies.txt\"\n")

	cfg, err := Load(os.Getenv)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.DownloadDelay != "60:180" {
		t.Errorf("DownloadDelay = %q, want 60:180", cfg.DownloadDelay)
	}
	if cfg.CookiesFile != "/tmp/cookies.txt" {
		t.Errorf("CookiesFile = %q, want /tmp/cookies.txt", cfg.CookiesFile)
	}
}

func TestLoadDownloadEnvOverrides(t *testing.T) {
	isolate(t)

	t.Setenv(EnvDownloadDelay, "90:120")
	t.Setenv(EnvDownloadCookies, "/env/cookies.txt")

	writeConfig(t, filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "yci", "config.toml"),
		"[download]\ndelay = \"60:180\"\ncookies_file = \"/file/cookies.txt\"\n")

	cfg, err := Load(os.Getenv)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.DownloadDelay != "90:120" {
		t.Errorf("DownloadDelay = %q, want 90:120", cfg.DownloadDelay)
	}
	if cfg.CookiesFile != "/env/cookies.txt" {
		t.Errorf("CookiesFile = %q, want /env/cookies.txt", cfg.CookiesFile)
	}
}

// Regression: staging must read its own key, not the library's.
func TestLoadFromFileSetsBothKeysIndependently(t *testing.T) {
	isolate(t)

	writeConfig(t, filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "yci", "config.toml"),
		"library_dir = \"/media/music\"\n")

	cfg, err := Load(os.Getenv)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.StagingDir == "/media/music" {
		t.Fatal("StagingDir inherited library_dir value")
	}
}

func TestLoadPrecedence(t *testing.T) {
	writeFileAtDefaultLocation := func(t *testing.T) {
		writeConfig(t, filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "yci", "config.toml"),
			"library_dir = \"/from/file\"")
	}

	tests := []struct {
		name     string
		withFile bool
		env      map[string]string
		wantLib  func() string // evaluated after isolation
	}{
		{
			name:    "defaults win when nothing set",
			wantLib: func() string { return filepath.Join(os.Getenv("HOME"), "Music", "Managed") },
		},
		{
			name:     "file beats default",
			withFile: true,
			wantLib:  func() string { return "/from/file" },
		},
		{
			name:    "env beats file",
			env:     map[string]string{EnvLibraryPath: "/from/env"},
			wantLib: func() string { return "/from/env" },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			isolate(t)
			if tt.withFile {
				writeFileAtDefaultLocation(t)
			}
			for k, v := range tt.env {
				t.Setenv(k, v)
			}

			cfg, err := Load(os.Getenv)
			if err != nil {
				t.Fatalf("Load: %v", err)
			}

			wantLib := tt.wantLib()
			wantStage := filepath.Join(os.Getenv("HOME"), "Music", "Staging")

			if cfg.LibraryDir != wantLib {
				t.Errorf("LibraryDir = %q, want %q", cfg.LibraryDir, wantLib)
			}
			if cfg.StagingDir != wantStage {
				t.Errorf("StagingDir = %q, want %q", cfg.StagingDir, wantStage)
			}
		})
	}
}

func TestLoadExplicitMissingConfigFileFails(t *testing.T) {
	isolate(t)

	missing := filepath.Join(t.TempDir(), "nope", "custom.toml")
	t.Setenv(EnvConfigPath, missing)

	_, err := Load(os.Getenv)
	if err == nil {
		t.Fatal("Load succeeded with explicit but missing config file")
	}
	if !strings.Contains(err.Error(), missing) {
		t.Errorf("error %q does not mention path %q", err, missing)
	}
}

func TestLoadEnvConfigPathRelocatesProfile(t *testing.T) {
	home := isolate(t)

	customDir := filepath.Join(home, "profiles", "main")
	customPath := filepath.Join(customDir, "config.toml")
	writeConfig(t, customPath, "staging_dir = \"/custom/staging\"\n")
	t.Setenv(EnvConfigPath, customPath)

	cfg, err := Load(os.Getenv)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.ConfigDir() != customDir {
		t.Errorf("ConfigDir = %q, want %q", cfg.ConfigDir(), customDir)
	}
	if cfg.StagingDir != "/custom/staging" {
		t.Errorf("StagingDir = %q, want /custom/staging", cfg.StagingDir)
	}
}

func TestLoadUnknownKeyFails(t *testing.T) {
	isolate(t)

	writeConfig(t, filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "yci", "config.toml"),
		"library_dir = \"/ok\"\nstaging_dirr = \"typo\"\n")

	_, err := Load(os.Getenv)
	if err == nil {
		t.Fatal("Load succeeded with unknown key")
	}
	if !strings.Contains(err.Error(), "staging_dirr") {
		t.Errorf("error %q does not name the offending key", err)
	}
}

func TestLoadMalformedConfigFails(t *testing.T) {
	isolate(t)

	writeConfig(t, filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "yci", "config.toml"),
		"library_dir = [not valid toml\n")

	_, err := Load(os.Getenv)
	if err == nil {
		t.Fatal("Load succeeded on malformed TOML")
	}
}
