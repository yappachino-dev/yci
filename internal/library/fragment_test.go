package library

import (
	"strings"
	"testing"
)

// corpus returns the real 341-title sample, stripped the same way the
// matcher does, so derivation and matching see identical input.
func fragmentCorpus(t *testing.T) []string {
	t.Helper()
	return loadTitleCorpus(t)
}

// TestDeriveFragmentOnLabelledSeeds pins the measured strategy. The expected
// tokens and recurrence counts come from running the extraction over the real
// corpus, not from intuition.
func TestDeriveFragmentOnLabelledSeeds(t *testing.T) {
	corpus := fragmentCorpus(t)

	tests := []struct {
		name       string
		seed       string
		wantTokens []string
		// wantRecurrence is the measured family size; -1 skips the check
		// for seeds whose family is genuinely ambiguous.
		wantRecurrence int
		wantCertain    bool
	}{
		{
			name:           "dominant Song / Performer shape",
			seed:           "Cry Baby / 星街すいせい(Cover)",
			wantTokens:     []string{"星街すいせい"},
			wantRecurrence: 59,
			wantCertain:    true,
		},
		{
			// Two tokens, not the joined string, so the run-together
			// "MoriCalliope" spelling is reached: 11, not the 10 a
			// substring match finds.
			name:           "multi-word latin performer becomes a token set",
			seed:           "BRAIN - Mori Calliope (cover)",
			wantTokens:     []string{"Mori", "Calliope"},
			wantRecurrence: 11,
			wantCertain:    true,
		},
		{
			name:           "collaboration falls through to the recurring name",
			seed:           "Guilty kiss「コワレヤスキ」/ 出雲霞×星街すいせい×堰代ミコ【歌ってみた】",
			wantTokens:     []string{"星街すいせい"},
			wantRecurrence: 59,
			wantCertain:    true,
		},
		{
			// The Western "Artist - Song" shape puts the song on the right,
			// where it recurs once and is rejected; the fallback recovers
			// the artist. Without the recurrence check this returns the
			// song and matches 1 of 13.
			name:           "western Artist - Song shape recovers via fallback",
			seed:           "IRyS - Carbonated Love【Original Song 3D MV】",
			wantTokens:     []string{"IRyS"},
			wantRecurrence: 14,
			wantCertain:    true,
		},
		{
			name:           "single track family is still certain enough to use",
			seed:           "IRIS OUT - 尾丸ポルカ(cover)",
			wantTokens:     []string{"尾丸ポルカ"},
			wantRecurrence: 2,
			wantCertain:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DeriveFragment(tt.seed, corpus)

			if strings.Join(got.Tokens, "|") != strings.Join(tt.wantTokens, "|") {
				t.Errorf("tokens = %q, want %q", got.Tokens, tt.wantTokens)
			}
			if tt.wantRecurrence >= 0 && got.Recurrence != tt.wantRecurrence {
				t.Errorf("recurrence = %d, want %d", got.Recurrence, tt.wantRecurrence)
			}
			if got.Certain != tt.wantCertain {
				t.Errorf("certain = %v, want %v", got.Certain, tt.wantCertain)
			}
		})
	}
}

// TestDeriveFragmentRejectsUninformative is the guard the measurement bought.
// Before the stopword and length rules, these produced sets that looked
// meaningful and were not: "the" matched 11 tracks, "it" matched 16.
func TestDeriveFragmentRejectsUninformative(t *testing.T) {
	corpus := fragmentCorpus(t)

	for _, tt := range []struct{ name, seed string }{
		{"english function words", "From The Start"},
		{"japanese particle", "Bad Apple!! but it's hololive"},
		{"punctuated single word", "Good Luck, Babe!"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got := DeriveFragment(tt.seed, corpus)

			for _, tok := range got.Tokens {
				if fragmentStopwords[foldFragment(tok)] {
					t.Errorf("selected stopword %q from %q", tok, tt.seed)
				}
				if len([]rune(tok)) < fragmentMinLen {
					t.Errorf("selected %d-rune token %q from %q", len([]rune(tok)), tok, tt.seed)
				}
			}
		})
	}
}

// TestTokenSetMatchingIgnoresSpacing is why selection is a token set and not
// a joined substring. A joined fragment for "Mori Calliope" would be
// "Mori Calliope" and would miss "MoriCalliope".
func TestTokenSetMatchingIgnoresSpacing(t *testing.T) {
	corpus := []string{
		"BRAIN - Mori Calliope (cover)",
		"IN_MY_HEAD - Mori Calliope (cover)",
		"【MV】Orpheus - Mori Calliope",
		"【#RequiemforLove3D】 Bocca della Verità feat @MoriCalliope",
		"GETCHA! / calliope × suisei(cover)",
		"KICK BACK / 鷹嶺ルイ(cover)",
	}

	tokens := []string{"Mori", "Calliope"}

	var matched []string
	for _, title := range corpus {
		if MatchesFragment(title, tokens) {
			matched = append(matched, title)
		}
	}

	// The joined substring "Mori Calliope" would miss the run-together
	// spelling; token-set matching must not.
	if len(matched) != 4 {
		t.Errorf("matched %d tracks, want 4:\n%v", len(matched), matched)
	}
}

func TestMatchesFragmentRequiresAllTokens(t *testing.T) {
	corpus := []string{
		"GETCHA! / calliope × suisei(cover)",
		"BRAIN - Mori Calliope (cover)",
		"KICK BACK / 鷹嶺ルイ(cover)",
	}

	tests := []struct {
		name   string
		tokens []string
		want   int
	}{
		{"single token", []string{"calliope"}, 2},
		{"both tokens present", []string{"GETCHA", "calliope"}, 1},
		// Known limitation: the corpus spells this performer in kanji in
		// some titles and latin in others, and folding does not unify the
		// two, so an AND across both forms matches nothing.
		{"romanised and kanji aliases do not unify", []string{"calliope", "星街すいせい"}, 0},
		{"no tokens matches nothing", nil, 0},
		{"token absent everywhere", []string{"nonexistent"}, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			n := 0
			for _, title := range corpus {
				if MatchesFragment(title, tt.tokens) {
					n++
				}
			}
			if n != tt.want {
				t.Errorf("matched %d, want %d", n, tt.want)
			}
		})
	}
}

func TestFoldFragmentNormalizesWidthAndCase(t *testing.T) {
	tests := []struct{ in, want string }{
		{"Mori Calliope", "mori calliope"},
		{"ＭＯＲＩ", "mori"},
		{"Ｓｕｉｓｅｉ", "suisei"},
		{"１０８", "108"},
		{"星街すいせい", "星街すいせい"},
	}

	for _, tt := range tests {
		if got := foldFragment(tt.in); got != tt.want {
			t.Errorf("foldFragment(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

// TestTokenizeKeepsJapaneseNamesWhole guards the CJK case: Japanese does not
// use spaces, so a naive splitter would shred a performer name into single
// characters.
func TestTokenizeKeepsJapaneseNamesWhole(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want []string
	}{
		{
			name: "japanese performer stays one token",
			in:   "Cry Baby / 星街すいせい(Cover)",
			want: []string{"Cry", "Baby", "星街すいせい", "Cover"},
		},
		{
			name: "latin words split on spaces",
			in:   "BRAIN - Mori Calliope (cover)",
			want: []string{"BRAIN", "Mori", "Calliope", "cover"},
		},
		{
			name: "decorations are trimmed",
			in:   "『MV』Overkill - Mori Calliope",
			want: []string{"MV", "Overkill", "Mori", "Calliope"},
		},
		{
			name: "empty input yields nothing",
			in:   "!!! ???",
			want: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Tokenize(tt.in)
			if strings.Join(got, "|") != strings.Join(tt.want, "|") {
				t.Errorf("Tokenize(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// TestDeriveFragmentOnWholeCorpus is a stability check rather than a quality
// assertion: every title in the corpus must produce a usable selection rather
// than an empty one or a stopword.
func TestDeriveFragmentOnWholeCorpus(t *testing.T) {
	corpus := fragmentCorpus(t)

	for _, title := range corpus {
		got := DeriveFragment(title, corpus)

		if len(got.Tokens) == 0 {
			t.Errorf("%q produced no tokens", title)
			continue
		}
		if got.Recurrence < 1 {
			t.Errorf("%q selected %q, which matches nothing", title, got.Tokens)
		}
		for _, tok := range got.Tokens {
			if fragmentStopwords[foldFragment(tok)] {
				t.Errorf("%q selected stopword %q", title, tok)
			}
		}
	}
}
