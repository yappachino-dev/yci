package download

import "testing"

func TestParseURL(t *testing.T) {
	tests := []struct {
		name         string
		rawURL       string
		wantType     URLType
		wantVideoID  string
		wantPlaylist string
		wantErr      bool
	}{
		{
			name:        "bare video id",
			rawURL:      "dQw4w9WgXcQ",
			wantType:    URLTypeVideo,
			wantVideoID: "dQw4w9WgXcQ",
		},
		{
			name:     "bare id wrong length",
			rawURL:   "shortid123456",
			wantType: URLTypeUnknown,
		},
		{
			name:     "bare id invalid characters",
			rawURL:   "dQw4w9WgXc$",
			wantType: URLTypeUnknown,
		},
		{
			name:     "empty string",
			rawURL:   "",
			wantType: URLTypeUnknown,
		},
		{
			name:        "watch url",
			rawURL:      "https://www.youtube.com/watch?v=dQw4w9WgXcQ",
			wantType:    URLTypeVideo,
			wantVideoID: "dQw4w9WgXcQ",
		},
		{
			name:        "watch url with tracking params",
			rawURL:      "https://www.youtube.com/watch?v=dQw4w9WgXcQ&si=abc&feature=share",
			wantType:    URLTypeVideo,
			wantVideoID: "dQw4w9WgXcQ",
		},
		{
			name:        "mobile watch url",
			rawURL:      "https://m.youtube.com/watch?v=dQw4w9WgXcQ",
			wantType:    URLTypeVideo,
			wantVideoID: "dQw4w9WgXcQ",
		},
		{
			name:        "music watch url",
			rawURL:      "https://music.youtube.com/watch?v=dQw4w9WgXcQ",
			wantType:    URLTypeVideo,
			wantVideoID: "dQw4w9WgXcQ",
		},
		{
			name:        "youtu.be short link",
			rawURL:      "https://youtu.be/dQw4w9WgXcQ",
			wantType:    URLTypeVideo,
			wantVideoID: "dQw4w9WgXcQ",
		},
		{
			name:         "playlist url",
			rawURL:       "https://www.youtube.com/playlist?list=PLabcdef1234567890",
			wantType:     URLTypePlaylist,
			wantPlaylist: "PLabcdef1234567890",
		},
		{
			name:         "hybrid url",
			rawURL:       "https://www.youtube.com/watch?v=dQw4w9WgXcQ&list=PLabcdef1234567890",
			wantType:     URLTypeHybrid,
			wantVideoID:  "dQw4w9WgXcQ",
			wantPlaylist: "PLabcdef1234567890",
		},
		{
			name:     "foreign host is not trusted",
			rawURL:   "https://evil.example.com/watch?v=dQw4w9WgXcQ&list=PLabcdef1234567890",
			wantType: URLTypeUnknown,
		},
		{
			name:     "scheme-less url has no trusted host",
			rawURL:   "youtube.com/watch?v=dQw4w9WgXcQ",
			wantType: URLTypeUnknown,
		},
		{
			name:     "trusted host without ids",
			rawURL:   "https://www.youtube.com/feed/subscriptions",
			wantType: URLTypeUnknown,
		},
		{
			name:    "malformed url",
			rawURL:  "http://[::1",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseURL(tt.rawURL)

			if tt.wantErr {
				if err == nil {
					t.Fatalf("ParseURL(%q) expected error, got %+v", tt.rawURL, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseURL(%q) returned error: %v", tt.rawURL, err)
			}
			if got.Type != tt.wantType {
				t.Errorf("Type = %v, want %v", got.Type, tt.wantType)
			}
			if got.VideoID != tt.wantVideoID {
				t.Errorf("VideoID = %q, want %q", got.VideoID, tt.wantVideoID)
			}
			if got.PlaylistID != tt.wantPlaylist {
				t.Errorf("PlaylistID = %q, want %q", got.PlaylistID, tt.wantPlaylist)
			}
		})
	}
}

func TestParsedURLCleanURL(t *testing.T) {
	tests := []struct {
		name          string
		parsed        ParsedURL
		forcePlaylist bool
		want          string
	}{
		{
			name:   "video",
			parsed: ParsedURL{Type: URLTypeVideo, VideoID: "dQw4w9WgXcQ"},
			want:   "https://www.youtube.com/watch?v=dQw4w9WgXcQ",
		},
		{
			name:   "playlist",
			parsed: ParsedURL{Type: URLTypePlaylist, PlaylistID: "PLabcdef1234567890"},
			want:   "https://www.youtube.com/playlist?list=PLabcdef1234567890",
		},
		{
			name:          "hybrid as video",
			parsed:        ParsedURL{Type: URLTypeHybrid, VideoID: "dQw4w9WgXcQ", PlaylistID: "PLabcdef1234567890"},
			forcePlaylist: false,
			want:          "https://www.youtube.com/watch?v=dQw4w9WgXcQ",
		},
		{
			name:          "hybrid forced to playlist",
			parsed:        ParsedURL{Type: URLTypeHybrid, VideoID: "dQw4w9WgXcQ", PlaylistID: "PLabcdef1234567890"},
			forcePlaylist: true,
			want:          "https://www.youtube.com/playlist?list=PLabcdef1234567890",
		},
		{
			name:   "unknown yields empty string",
			parsed: ParsedURL{Type: URLTypeUnknown},
			want:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.parsed.CleanURL(tt.forcePlaylist); got != tt.want {
				t.Errorf("CleanURL(%v) = %q, want %q", tt.forcePlaylist, got, tt.want)
			}
		})
	}
}
