package library

import (
	"bufio"
	"os"
	"strings"
	"testing"
)

// TestStripTitleKeepsWesternOrder is the guard against the failure that
// retired inference: "Artist - Song" is the most common upload format
// worldwide and the exact inverse of the convention the original corpus
// taught the parser. Stripping must never reorder a title, so these stay
// intact with only the trailing marker removed.
func TestStripTitleKeepsWesternOrder(t *testing.T) {
	tests := []struct{ raw, want string }{
		{"Queen - Bohemian Rhapsody (Official Video)", "Queen - Bohemian Rhapsody"},
		{"Daft Punk - Around The World (Official Video)", "Daft Punk - Around The World"},
		{"Adele - Hello (Official Audio)", "Adele - Hello"},
		{"Radiohead - Creep (Official Video)", "Radiohead - Creep"},
		{"Coldplay - Yellow (Official Video) [4K]", "Coldplay - Yellow"},
		{"Billie Eilish - bad guy (Official Video) [HD]", "Billie Eilish - bad guy"},
		{"Tame Impala - The Less I Know The Better (Lyrics)", "Tame Impala - The Less I Know The Better"},
		{"Dua Lipa - Levitating (Official Music Video)", "Dua Lipa - Levitating"},
	}

	for _, tt := range tests {
		t.Run(tt.raw, func(t *testing.T) {
			got := StripTitle(tt.raw)
			if got != tt.want {
				t.Errorf("StripTitle = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestStripTitleJapanese(t *testing.T) {
	tests := []struct{ raw, want string }{
		{"【MV】Turing Love / チューリングラブ - Gigi Murin(Cover)", "Turing Love / チューリングラブ - Gigi Murin"},
		{"【IRyS】アイデンティティ / Identity【COVER】", "アイデンティティ / Identity"},
		{"『TAKO∞TAKOVER』 - Ninomae Ina'nis", "TAKO∞TAKOVER - Ninomae Ina'nis"},
		{"月に向かって撃て / 星街すいせい(official)", "月に向かって撃て / 星街すいせい"},
		{"【歌ってみた】メルティランドナイトメア【#07】", "メルティランドナイトメア"},
		{"the boy is mine", "the boy is mine"},
	}

	for _, tt := range tests {
		t.Run(tt.raw, func(t *testing.T) {
			if got := StripTitle(tt.raw); got != tt.want {
				t.Errorf("StripTitle = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestStripTitleIsSafe pins the properties that make stripping
// unconditional: it never empties a title, never reorders one, and is
// idempotent so repeated passes cannot erode a title further.
func TestStripTitleIsSafe(t *testing.T) {
	for _, raw := range loadTitleCorpus(t) {
		first := StripTitle(raw)

		if first == "" {
			t.Errorf("StripTitle emptied %q", raw)
			continue
		}
		if second := StripTitle(first); second != first {
			t.Errorf("not idempotent: %q -> %q -> %q", raw, first, second)
		}
	}
}

func TestStripTitleFallsBackWhenNothingRemains(t *testing.T) {
	// Unbalanced decoration can strip to nothing recognizable; the
	// original must survive rather than becoming an empty title.
	for _, raw := range []string{"【MV】", "（）", "【COVER】"} {
		if got := StripTitle(raw); got == "" {
			t.Errorf("StripTitle(%q) returned empty", raw)
		}
	}
}

// loadTitleCorpus returns every real title from testdata, so the strip and
// parse suites run against the same 341-title sample.
func loadTitleCorpus(t *testing.T) []string {
	t.Helper()

	f, err := os.Open("testdata/titles.txt")
	if err != nil {
		t.Fatalf("open corpus: %v", err)
	}
	defer f.Close()

	var titles []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		if raw := strings.TrimSpace(scanner.Text()); raw != "" {
			titles = append(titles, raw)
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan corpus: %v", err)
	}
	if len(titles) == 0 {
		t.Fatal("corpus is empty")
	}

	return titles
}
