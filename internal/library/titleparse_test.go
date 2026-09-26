package library

import "testing"

func TestParseTitle(t *testing.T) {
	tests := []struct {
		name       string
		raw        string
		song       string
		performer  string
		version    string
		confidence TitleConfidence
	}{
		{
			name:       "slash separated with version suffix",
			raw:        "月に向かって撃て / 星街すいせい(official)",
			song:       "月に向かって撃て",
			performer:  "星街すいせい",
			version:    "official",
			confidence: ConfidenceHigh,
		},
		{
			name:       "leading cover decoration and trailing performer",
			raw:        "【COVER】SOS (Nerissa Ravencroft)",
			song:       "SOS",
			performer:  "Nerissa Ravencroft",
			version:    "cover",
			confidence: ConfidenceHigh,
		},
		{
			name:       "dash separated",
			raw:        "『TAKO∞TAKOVER』 - Ninomae Ina'nis",
			song:       "TAKO∞TAKOVER",
			performer:  "Ninomae Ina'nis",
			confidence: ConfidenceHigh,
		},
		{
			name:       "decoration inside bracket is a version, kana middle is a reading",
			raw:        "「Cover」Dilemma / ジレンマ - Nanashi Mumei",
			song:       "Dilemma / ジレンマ",
			performer:  "Nanashi Mumei",
			version:    "cover",
			confidence: ConfidenceLow,
		},
		{
			name:       "collab chain stays one credit",
			raw:        "U＆I / 猫又おかゆ × 戌神ころね(cover)",
			song:       "U＆I",
			performer:  "猫又おかゆ × 戌神ころね",
			version:    "cover",
			confidence: ConfidenceHigh,
		},
		{
			name:       "spaced parentheses",
			raw:        "キュートなカノジョ / 猫又おかゆ( cover )",
			song:       "キュートなカノジョ",
			performer:  "猫又おかゆ",
			version:    "cover",
			confidence: ConfidenceHigh,
		},
		{
			name:       "trailing bracket is channel, lyric video is the version",
			raw:        "Oh Princess - Lyric Video [CECILIA IMMERGREEN]",
			song:       "Oh Princess",
			performer:  "CECILIA IMMERGREEN",
			version:    "lyric video",
			confidence: ConfidenceHigh,
		},
		{
			name:       "leading channel decoration becomes performer when none is split",
			raw:        "【MV】Orpheus - Mori Calliope",
			song:       "Orpheus",
			performer:  "Mori Calliope",
			version:    "mv",
			confidence: ConfidenceHigh,
		},
		{
			name:       "series and event tags are dropped",
			raw:        "【#RequiemforLove3D】 Bocca della Verità feat @MoriCalliope",
			song:       "Bocca della Verità",
			performer:  "MoriCalliope",
			confidence: ConfidenceHigh,
		},
		{
			name:       "bare trailing version token",
			raw:        "POP/STARS - K/DA || Hakos Baelz ・Ninomae Ina'nis COVER",
			song:       "POP/STARS",
			performer:  "K/DA || Hakos Baelz ・Ninomae Ina'nis",
			version:    "cover",
			confidence: ConfidenceHigh,
		},
		{
			name:       "solidus inside words is not a separator",
			raw:        "the boy is mine",
			song:       "the boy is mine",
			confidence: ConfidenceNone,
		},
		{
			name:       "version with trailing context",
			raw:        "OVER//RIDE - Mori Calliope × Nerissa Ravencroft (Original Song : Chaos Zero Nightmare OST)",
			song:       "OVER//RIDE",
			performer:  "Mori Calliope × Nerissa Ravencroft",
			version:    "original",
			confidence: ConfidenceHigh,
		},
		{
			name:       "version only, no performer",
			raw:        "【MV】Odyssey - Hololive English (Original Song)",
			song:       "Odyssey",
			performer:  "Hololive English",
			version:    "original",
			confidence: ConfidenceHigh,
		},
		{
			name:       "unbalanced delimiters are tolerated",
			raw:        "【ORIGINAL SONG】めくるめくランデヴー／Mekurumeku Rendezvous (TVアニメ『自動販売機に生まれ変わった俺は迷宮を彷徨う3rd season』主題歌【FUWAMOCO]",
			song:       "めくるめくランデヴー／Mekurumeku Rendezvous (TVアニメ『自動販売機に生まれ変わった俺は迷宮を彷徨う3rd season』主題歌【FUWAMOCO]",
			performer:  "",
			version:    "original",
			confidence: ConfidenceLow,
		},
		{
			name:       "closing bracket against the slash is a separator",
			raw:        "\u3010cover\u3011Guilty kiss\u300c\u30b3\u30ef\u30ec\u30e4\u30b9\u30ad\u300d/ \u51fa\u96f2\u9711\u00d7\u661f\u8857\u3059\u3044\u305b\u3063\u00d7\u5830\u4ee3\u30df\u30b3",
			song:       "Guilty kiss\u300c\u30b3\u30ef\u30ec\u30e4\u30b9\u30ad\u300d",
			performer:  "\u51fa\u96f2\u9711\u00d7\u661f\u8857\u3059\u3044\u305b\u3063\u00d7\u5830\u4ee3\u30df\u30b3",
			version:    "cover",
			confidence: ConfidenceLow,
		},
		{
			name:       "empty input",
			raw:        "",
			song:       "",
			confidence: ConfidenceNone,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseTitle(tt.raw)

			if got.Song != tt.song {
				t.Errorf("Song = %q, want %q", got.Song, tt.song)
			}
			if got.Performer != tt.performer {
				t.Errorf("Performer = %q, want %q", got.Performer, tt.performer)
			}
			if got.Version != tt.version {
				t.Errorf("Version = %q, want %q", got.Version, tt.version)
			}
			if got.Confidence != tt.confidence {
				t.Errorf("Confidence = %v, want %v", got.Confidence, tt.confidence)
			}
			if got.Raw != tt.raw {
				t.Errorf("Raw = %q, want it preserved verbatim", got.Raw)
			}
		})
	}
}

// TestParseTitleCorpus guards properties every real title must satisfy:
// parsing never empties the song and never panics.
func TestParseTitleCorpus(t *testing.T) {
	stats := map[TitleConfidence]int{}

	for _, raw := range loadTitleCorpus(t) {
		got := ParseTitle(raw)
		if got.Song == "" {
			t.Errorf("empty song for %q", raw)
		}
		stats[got.Confidence]++
	}

	t.Logf("parsed %d titles: %d high, %d low, %d none (hints only; nothing gates on these)",
		len(loadTitleCorpus(t)), stats[ConfidenceHigh], stats[ConfidenceLow], stats[ConfidenceNone])
}

// TestParseTitleNoStructureStaysHonest pins the failure mode that matters
// most: a bare title must not acquire fabricated metadata.
func TestParseTitleNoStructureStaysHonest(t *testing.T) {
	for _, raw := range []string{"the boy is mine", "Melt"} {
		got := ParseTitle(raw)

		if got.Performer != "" {
			t.Errorf("%q gained performer %q", raw, got.Performer)
		}
		if got.Version != "" {
			t.Errorf("%q gained version %q", raw, got.Version)
		}
		if got.Confidence != ConfidenceNone {
			t.Errorf("%q reported %v confidence with no structure: %+v", raw, got.Confidence, got)
		}
		if got.Song != raw {
			t.Errorf("%q was altered to %q", raw, got.Song)
		}
	}

	// A series number decorates the title without naming anyone.
	got := ParseTitle("【歌ってみた】メルティランドナイトメア【#07】")
	if got.Performer != "" {
		t.Errorf("series tag became a performer: %q", got.Performer)
	}
	if got.Song != "メルティランドナイトメア" {
		t.Errorf("Song = %q, want the title without decorations", got.Song)
	}
}
