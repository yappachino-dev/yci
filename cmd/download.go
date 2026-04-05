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

		var urls []string
		for _, arg := range args {
			cleanArg := strings.TrimSpace(arg)
			if cleanArg != "" {
				urls = append(urls, cleanArg)
			}
		}

		filePath, _ := cmd.Flags().GetString("file")
		if filePath != "" {
			fileUrls, err := readUrlsFromFile(filePath)
			if err != nil {
				return fmt.Errorf("failed to read urls from file: %w", err)
			}
			urls = append(urls, fileUrls...)
		}

		if len(urls) == 0 {
			return fmt.Errorf("no URLs provided. Pass links as arguments or use the --file flag")
		}

		fmt.Printf("Starting download queue for %d tracks...\n\n", len(urls))

		targetPath := filepath.Join(lib.Paths.Staging, "%(id)s.opus")
		successCount := 0

		for i, url := range urls {
			fmt.Printf("--- [Track %d / %d] ---\n", i+1, len(urls))
			fmt.Printf("Target: %s\n", url)

			if err := downloader.Download(targetPath, url); err != nil {
				fmt.Printf(" Error downloading %s: %v\n\n", url, err)
				continue
			}

			successCount++
			fmt.Println()
		}

		fmt.Printf(" Download complete! Successfully retrieved %d/%d tracks.\n", successCount, len(urls))
		if successCount > 0 {
			fmt.Println("▶ Run `yci process staging` to import them into your library.")
		}

		return nil
	},
}

func init() {
	rootCmd.AddCommand(downloadCmd)

	downloadCmd.Flags().StringP("file", "f", "", "Read a list of URLs from a text file (one per line)")
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
