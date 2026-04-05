package cmd

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"playlist-manager/internal/downloader"
	"playlist-manager/internal/utils"

	"github.com/spf13/cobra"
)

var downloadCmd = &cobra.Command{
	Use:     "download [urls...]",
	Aliases: []string{"dl", "get"},
	Short:   "Download YouTube audio directly to the staging area",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := utils.CheckDependencies(); err != nil {
			return err
		}

		delayFlag, _ := cmd.Flags().GetString("delay")
		delayParts := strings.Split(delayFlag, ":")
		if len(delayParts) != 2 {
			return fmt.Errorf("invalid delay format. Expected min:max (e.g., 5:30)")
		}
		minSleep, maxSleep := delayParts[0], delayParts[1]

		var rawUrls []string
		for _, arg := range args {
			if cleanArg := strings.TrimSpace(arg); cleanArg != "" {
				rawUrls = append(rawUrls, cleanArg)
			}
		}

		filePath, _ := cmd.Flags().GetString("file")
		if filePath != "" {
			fileUrls, err := readUrlsFromFile(filePath)
			if err != nil {
				return fmt.Errorf("failed to read urls from file: %w", err)
			}
			rawUrls = append(rawUrls, fileUrls...)
		}

		if len(rawUrls) == 0 {
			return fmt.Errorf("no URLs provided. Pass links as arguments or use the --file flag")
		}

		seenUrls := make(map[string]bool)
		var uniqueUrls []string

		for _, u := range rawUrls {
			if !seenUrls[u] {
				seenUrls[u] = true
				uniqueUrls = append(uniqueUrls, u)
			}
		}

		queueSkipped := len(rawUrls) - len(uniqueUrls)

		fmt.Printf("Starting queue for %d unique tracks... (Skipped %d duplicates in input)\n\n", len(uniqueUrls), queueSkipped)

		targetPath := filepath.Join(lib.Paths.Staging, "%(id)s.opus")
		successCount := 0
		dbSkippedCount := 0

		for i, url := range uniqueUrls {
			fmt.Printf("--- [Track %d / %d] ---\n", i+1, len(rawUrls))

			if sourceID := lib.ExtractSourceID(url); sourceID != "" {
				exists, err := lib.TrackExistsBySourceID(sourceID)
				if err != nil {
					fmt.Printf(" Warning: DB check failed for %s: %v\n", sourceID, err)
				} else if exists {
					fmt.Printf(" Skipped: Track already exists in library (ID: %s)\n\n", sourceID)
					dbSkippedCount++
					continue
				}
			}

			fmt.Printf("Target: %s\n", url)

			if err := downloader.Download(targetPath, url, minSleep, maxSleep); err != nil {
				fmt.Printf(" Error downloading %s: %v\n\n", url, err)
				continue
			}

			successCount++
			fmt.Println()
		}

		fmt.Printf(" Queue complete! Downloaded: %d | DB Skipped: %d | Failed: %d\n",
			successCount,
			dbSkippedCount,
			len(uniqueUrls)-successCount-dbSkippedCount,
		)

		if successCount > 0 {
			fmt.Println("▶ Run `yci process staging` to import them into your library.")
		}

		return nil
	},
}

func init() {
	rootCmd.AddCommand(downloadCmd)

	downloadCmd.Flags().StringP("file", "f", "", "Read a list of URLs from a text file (one per line)")
	downloadCmd.Flags().StringP("delay", "d", "5:30", "Randomized delay between downloads in seconds (format min:max)")
}

// --- Helper Functions ---

func readUrlsFromFile(path string) ([]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var urls []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" && !strings.HasPrefix(line, "#") {
			urls = append(urls, line)
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return urls, nil
}
