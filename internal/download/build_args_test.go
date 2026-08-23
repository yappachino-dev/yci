package download

import (
	"slices"
	"testing"
)

func TestBuildArgsWithoutCookies(t *testing.T) {
	args := buildArgs(Config{
		AbsoluteOutputPath: "/staging/%(id)s.opus",
		URL:                "dQw4w9WgXcQ",
		MinSleep:           "5",
		MaxSleep:           "30",
	})

	if slices.Contains(args, "--cookies") {
		t.Error("cookies flag present without CookiesFile")
	}
	if !slices.Contains(args, "--no-cookies") {
		t.Error("--no-cookies missing without CookiesFile")
	}

	want := []string{"--sleep-interval", "5", "--max-sleep-interval", "30"}
	for _, w := range want {
		if !slices.Contains(args, w) {
			t.Errorf("expected %q in args, got %v", w, args)
		}
	}
}

func TestBuildArgsWithCookies(t *testing.T) {
	args := buildArgs(Config{
		URL:         "dQw4w9WgXcQ",
		MinSleep:    "5",
		MaxSleep:    "30",
		CookiesFile: "/home/user/cookies.txt",
	})

	idx := slices.Index(args, "--cookies")
	if idx == -1 {
		t.Fatalf("--cookies missing; args = %v", args)
	}
	if idx+1 >= len(args) || args[idx+1] != "/home/user/cookies.txt" {
		t.Errorf("--cookies not followed by path; args = %v", args)
	}
	if slices.Contains(args, "--no-cookies") {
		t.Error("--no-cookies must be absent when CookiesFile is set")
	}
}
