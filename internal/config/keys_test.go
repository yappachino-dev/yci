package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSetCreatesFileAndPersistsValue(t *testing.T) {
	isolate(t)

	env := os.Getenv
	if err := Set(env, "download.delay", "60:180"); err != nil {
		t.Fatalf("Set: %v", err)
	}

	cfg, err := Load(env)
	if err != nil {
		t.Fatalf("Load after Set: %v", err)
	}

	if cfg.DownloadDelay != "60:180" {
		t.Errorf("DownloadDelay = %q, want 60:180", cfg.DownloadDelay)
	}
	if src := cfg.sources["download.delay"]; src != SourceFile {
		t.Errorf("source = %q, want %q", src, SourceFile)
	}
}

func TestSetHonorsExplicitPath(t *testing.T) {
	isolate(t)

	custom := filepath.Join(t.TempDir(), "nested", "custom.toml")
	t.Setenv(EnvConfigPath, custom)

	if err := Set(os.Getenv, "staging_dir", "/tmp/inbox"); err != nil {
		t.Fatalf("Set: %v", err)
	}

	if _, err := os.Stat(custom); err != nil {
		t.Fatalf("config file not created at explicit path: %v", err)
	}

	cfg, err := Load(os.Getenv)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.StagingDir != "/tmp/inbox" {
		t.Errorf("StagingDir = %q, want /tmp/inbox", cfg.StagingDir)
	}
}

// The critical property: writes must not clobber keys this package
// does not model (e.g., keys written by a newer yci version).
func TestSetPreservesUnknownKeysAndSiblings(t *testing.T) {
	isolate(t)

	path := filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "yci", "config.toml")
	writeConfig(t, path,
		"library_dir = \"/keep/me\"\n\n[download]\ndelay = \"1:2\"\ncookies_file = \"/keep/cookies\"\n\n[future]\nsecret_key = \"untouched\"\n")

	if err := Set(os.Getenv, "download.delay", "9:9"); err != nil {
		t.Fatalf("Set: %v", err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	text := string(raw)
	for _, want := range []string{
		`"/keep/me"`,               // sibling top-level key survives
		`delay = "9:9"`,            // the value actually changed
		`"/keep/cookies"`,          // sibling nested key survives
		`secret_key = "untouched"`, // unknown table survives
	} {
		if !strings.Contains(text, want) {
			t.Errorf("rewritten file lost %s;\nfile:\n%s", want, text)
		}
	}
}

func TestSetRejectsInvalidValues(t *testing.T) {
	isolate(t)

	tests := []struct {
		key, value, wantErr string
	}{
		{"library_dir", "relative/path", "absolute"},
		{"staging_dir", "", "absolute"},
		{"download.delay", "5:abc", "non-negative"},
		{"download.delay", "5", "min:max"},
		{"download.cookies_file", "cookies.txt", "absolute"},
	}

	for _, tt := range tests {
		err := Set(os.Getenv, tt.key, tt.value)
		if err == nil {
			t.Errorf("Set(%s, %q) succeeded, want error mentioning %q", tt.key, tt.value, tt.wantErr)
			continue
		}
		if !strings.Contains(err.Error(), tt.wantErr) {
			t.Errorf("Set(%s, %q) error %q does not mention %q", tt.key, tt.value, err, tt.wantErr)
		}
	}
}

func TestSetRejectsUnknownKey(t *testing.T) {
	isolate(t)

	if err := Set(os.Getenv, "nonsense_key", "x"); err == nil {
		t.Fatal("Set succeeded with unknown key")
	}
}

func TestResolvedSources(t *testing.T) {
	isolate(t)

	writeConfig(t, filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "yci", "config.toml"),
		"[download]\ndelay = \"60:180\"\n")

	t.Setenv(EnvLibraryPath, "/env/lib")

	cfg, err := Load(os.Getenv)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	tests := []struct {
		key        string
		wantValue  string
		wantSource Source
	}{
		{"library_dir", "/env/lib", Source("env")},
		{"download.delay", "60:180", SourceFile},
		{"staging_dir", filepath.Join(os.Getenv("HOME"), "Music", "Staging"), SourceDefault},
	}

	for _, tt := range tests {
		value, source, err := cfg.Resolved(tt.key)
		if err != nil {
			t.Fatalf("Resolved(%s): %v", tt.key, err)
		}
		if value != tt.wantValue || source != tt.wantSource {
			t.Errorf("Resolved(%s) = (%q, %q), want (%q, %q)",
				tt.key, value, source, tt.wantValue, tt.wantSource)
		}
	}
}

func TestParseDelayValid(t *testing.T) {
	for _, in := range []string{"5:30", "0:0", "60:180"} {
		minStr, maxStr, err := ParseDelay(in)
		if err != nil {
			t.Errorf("ParseDelay(%q): %v", in, err)
			continue
		}
		parts := strings.Split(in, ":")
		if minStr != parts[0] || maxStr != parts[1] {
			t.Errorf("ParseDelay(%q) = (%q, %q)", in, minStr, maxStr)
		}
	}
}

func TestParseDelayInvalid(t *testing.T) {
	for _, in := range []string{"5", "5:30:60", "a:b", "-1:30", "5:-30", ""} {
		if _, _, err := ParseDelay(in); err == nil {
			t.Errorf("ParseDelay(%q) succeeded, want error", in)
		}
	}
}
