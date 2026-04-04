package library

import (
	"bufio"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"playlist-manager/internal/utils"

	"github.com/dhowden/tag"
	"github.com/google/uuid"
)

func (l *Library) ProcessStaging() error {
	files, err := os.ReadDir(l.Paths.Staging)
	if err != nil {
		return fmt.Errorf("could not read staging dir: %w", err)
	}

	reader := bufio.NewReader(os.Stdin)

	for _, file := range files {
		if file.IsDir() || filepath.Ext(file.Name()) != ".opus" {
			continue
		}

		err := func() error {
			oldPath := filepath.Join(l.Paths.Staging, file.Name())
			f, err := os.Open(oldPath)
			if err != nil {
				return err
			}

			m, err := tag.ReadFrom(f)
			f.Close()

			if err != nil {
				return fmt.Errorf("could not read metadata: %w", err)
			}

			channelName := strings.TrimSpace(m.Artist())
			title := strings.TrimSpace(m.Title())
			sourceID := strings.TrimSuffix(file.Name(), ".opus")

			artistID, err := l.GetArtistByChannel(channelName)
			if err != nil {
				return err
			}

			if artistID == "" {
				fmt.Printf("\n  Unknown Channel: %q\n   Song: %q\n", channelName, title)
				fmt.Print("   Enter an existing Artist ID to link (or type 'skip'): ")

				input, _ := reader.ReadString('\n')
				input = strings.TrimSpace(input)

				if input == "skip" || input == "" {
					fmt.Printf("   Skipping %s...\n", file.Name())
					return nil
				}

				_, err := l.GetArtist(input)
				if err != nil {
					if err == sql.ErrNoRows {
						return fmt.Errorf("artist ID %q does not exist. Create the artist first using mk-artist", input)
					}
					return err
				}

				_, err = l.db.Exec("INSERT INTO channels (artist_id, channel_name) VALUES (?, ?)", input, channelName)
				if err != nil {
					return fmt.Errorf("failed to link channel: %w", err)
				}

				artistID = input
				fmt.Printf("    Linked %q to artist %q\n", channelName, artistID)
			}

			artist, err := l.GetArtist(artistID)
			if err != nil {
				return fmt.Errorf("failed to fetch artist details: %w", err)
			}

			folderPath, err := l.GetFolderPath(artist.FolderID)
			if err != nil {
				return fmt.Errorf("failed to resolve folder path: %w", err)
			}

			safeTitle := utils.SanitizeFilename(title)
			newFilename := safeTitle + ".opus"

			var relativePath string
			if folderPath == "" {
				relativePath = newFilename
			} else {
				relativePath = folderPath + "/" + newFilename
			}

			absoluteNewPath := filepath.Join(l.Paths.Library, relativePath)

			if err := os.MkdirAll(filepath.Dir(absoluteNewPath), 0o755); err != nil {
				return fmt.Errorf("failed to create target directories: %w", err)
			}

			trackID := uuid.New().String()

			tx, err := l.db.Begin()
			if err != nil {
				return err
			}

			insertQuery := `
				INSERT INTO tracks (id, title, album, artist_id, duration_sec, relative_path, source_id) 
				VALUES (?, ?, ?, ?, ?, ?, ?)`

			_, err = tx.Exec(insertQuery, trackID, title, m.Album(), artistID, 0, relativePath, sourceID)
			if err != nil {
				tx.Rollback()
				return fmt.Errorf("database insert failed: %w", err)
			}

			if err := os.Rename(oldPath, absoluteNewPath); err != nil {
				tx.Rollback()
				return fmt.Errorf("failed to move file: %w", err)
			}

			if err := tx.Commit(); err != nil {
				return fmt.Errorf("failed to commit transaction: %w", err)
			}

			fmt.Printf(" Processed: %s -> %s\n", sourceID, relativePath)
			return nil
		}()
		if err != nil {
			fmt.Printf(" Error processing %s: %v\n", file.Name(), err)
		}
	}

	return nil
}
