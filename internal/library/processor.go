package library

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"playlist-manager/internal/utils"

	"github.com/dhowden/tag"
	"github.com/google/uuid"
)

//	uiHandler := func(channelName string, title string) (string, error) {
//	            reader := bufio.NewReader(os.Stdin)
//
//	            fmt.Printf("\n[Unknown Channel] %q\n", channelName)
//	            fmt.Printf("   Track: %q\n", title)
//	            fmt.Print("   Enter Artist ID to link (or 'skip'): ")
//
//	            input, err := reader.ReadString('\n')
//	            if err != nil {
//	                return "", err
//	            }
//
//	            input = strings.TrimSpace(input)
//	            if input == "" || input == "skip" {
//	                return "skip", nil
//	            }
//
//	            return input, nil
//	        }

type LinkHandler func(channelName string, title string) (artistID string, err error)

func (l *Library) ProcessStaging(onUnknown LinkHandler) error {
	files, err := os.ReadDir(l.Paths.Staging)
	if err != nil {
		return fmt.Errorf("could not read staging dir: %w", err)
	}

	for _, file := range files {
		if file.IsDir() || filepath.Ext(file.Name()) != ".opus" {
			continue
		}

		if err := l.processSingleFile(file.Name(), onUnknown); err != nil {
			fmt.Printf("Error processing %s: %v\n", file.Name(), err)
			continue
		}
	}

	return nil
}

func (l *Library) processSingleFile(fileName string, onUnknown LinkHandler) error {
	oldPathNative := filepath.Join(l.Paths.Staging, fileName)

	m, err := l.readMetadata(oldPathNative)
	if err != nil {
		return err
	}

	channelName := strings.TrimSpace(m.Artist())
	title := strings.TrimSpace(m.Title())
	sourceID := strings.TrimSuffix(fileName, ".opus")

	artistID, err := l.GetArtistByChannel(channelName)
	if err != nil {
		return err
	}

	if artistID == "" {
		artistID, err = onUnknown(channelName, title)
		if err != nil {
			return err
		}

		if artistID == "skip" || artistID == "" {
			return nil
		}

		if err := l.AddChannel(artistID, channelName); err != nil {
			return err
		}
	}

	artist, err := l.GetArtist(artistID)
	if err != nil {
		return fmt.Errorf("failed to fetch mapped artist: %w", err)
	}

	folderPath, err := l.ResolveFolderPath(artist.FolderID)
	if err != nil {
		return fmt.Errorf("failed to resolve folder path: %w", err)
	}

	safeTitle := utils.SanitizeFilename(title)
	newFilename := safeTitle + ".opus"

	var relPath string
	if folderPath == "" {
		relPath = newFilename
	} else {
		relPath = folderPath + "/" + newFilename
	}

	absPathNative := filepath.Join(l.Paths.Library, filepath.FromSlash(relPath))

	return l.finalizeTrackImport(Track{
		ID:           uuid.New().String(),
		Title:        title,
		Album:        m.Album(),
		ArtistID:     artistID,
		RelativePath: relPath,
		SourceID:     sourceID,
	}, oldPathNative, absPathNative)
}

func (l *Library) readMetadata(path string) (tag.Metadata, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	return tag.ReadFrom(f)
}

func (l *Library) finalizeTrackImport(t Track, oldPath, newPath string) error {
	tx, err := l.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	query := `INSERT INTO tracks (id, title, album, artist_id, duration_sec, relative_path, source_id) 
	          VALUES (?, ?, ?, ?, ?, ?, ?)`

	_, err = tx.Exec(query, t.ID, t.Title, toNull(t.Album), toNull(t.ArtistID), t.DurationSec, t.RelativePath, toNull(t.SourceID))
	if err != nil {
		return fmt.Errorf("db error: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(newPath), 0o755); err != nil {
		return err
	}
	if err := os.Rename(oldPath, newPath); err != nil {
		return err
	}

	return tx.Commit()
}
