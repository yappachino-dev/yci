package cmd

import (
	"bufio"
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"playlist-manager/internal/downloader"
	"playlist-manager/internal/utils"

	"github.com/spf13/cobra"
)

const (
	flagFile = "file"
)

var downloadCmd = &cobra.Command{
	Use:   "download [urls...]",
	Short: "Download YouTube audio directly to the staging area",
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()

		if err := utils.CheckDependencies(); err != nil {
			return err
		}

		delayFlag, _ := cmd.Flags().GetString(flagDelay)
		delayParts := strings.Split(delayFlag, ":")
		if len(delayParts) != 2 {
			return fmt.Errorf("invalid delay format. Expected min:max (e.g., 5:30)")
		}
		filePath, _ := cmd.Flags().GetString(flagFile)

		uniqueUrls, queueSkipped, err := filterUniqueURLs(args, filePath)
		if err != nil {
			return err
		}

		fmt.Printf("Starting queue for %d unique tracks... (Skipped %d duplicates in input)\n\n", len(uniqueUrls), queueSkipped)

		stats := processQueue(ctx, uniqueUrls, delayParts[0], delayParts[1])

		fmt.Printf(
			"Queue complete! Downloaded: %d | DB Skipped: %d | Failed: %d\n",
			stats.success, stats.skipped, stats.failed,
		)

		if stats.success > 0 {
			fmt.Println("▶ Run `yci process staging` to import them into your library.")
		}

		return nil
	},
}

func init() {
	rootCmd.AddCommand(downloadCmd)

	downloadCmd.Flags().String(flagFile, "", "Read a list of URLs from a text file (one per line)")
	downloadCmd.Flags().String(flagDelay, "5:30", "Randomized delay between downloads in seconds (format min:max)")
}

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

		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		u, err := url.Parse(line)
		if err == nil {
			queryParams := u.Query()

			queryParams.Del("si")
			queryParams.Del("feature")

			u.RawQuery = queryParams.Encode()

			line = u.String()
		}

		urls = append(urls, line)
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return urls, nil
}

func filterUniqueURLs(args []string, filePath string) ([]string, int, error) {
	var rawUrls []string
	for _, arg := range args {
		if cleanArg := strings.TrimSpace(arg); cleanArg != "" {
			rawUrls = append(rawUrls, cleanArg)
		}
	}

	if filePath != "" {
		fileUrls, err := readUrlsFromFile(filePath)
		if err != nil {
			return nil, 0, fmt.Errorf("failed to read urls from file: %w", err)
		}
		rawUrls = append(rawUrls, fileUrls...)
	}

	if len(rawUrls) == 0 {
		return nil, 0, fmt.Errorf("no URLs provided. Pass links as arguments or use the --file flag")
	}

	stagedIDs := make(map[string]bool)

	if stagedFiles, err := os.ReadDir(appl.Paths.StagingDir()); err != nil {
		return nil, 0, fmt.Errorf("failed to read staged dir: %w", err)
	} else {
		for _, f := range stagedFiles {
			stagedIDs[strings.TrimSuffix(f.Name(), ".opus")] = true
		}
	}

	seenUrls := make(map[string]bool)
	var uniqueUrls []string

	for _, u := range rawUrls {
		if !seenUrls[u] {
			seenUrls[u] = true

			if stagedIDs[utils.ExtractSourceID(u)] {
				continue
			}

			uniqueUrls = append(uniqueUrls, u)
		}
	}

	return uniqueUrls, len(rawUrls) - len(uniqueUrls), nil
}

type processResult struct {
	success int
	skipped int
	failed  int
}

func processQueue(ctx context.Context, urls []string, minSleep string, maxSleep string) processResult {
	targetPath := filepath.Join(appl.Paths.StagingDir(), "%(id)s.opus")

	successCount := 0
	dbSkippedCount := 0

	for i, url := range urls {
		fmt.Printf("--- [Track %d / %d] ---\n", i+1, len(urls))

		if sourceID := utils.ExtractSourceID(url); sourceID != "" {
			exists, err := appl.TrackService.ExistsBySourceID(ctx, sourceID)
			if err != nil {
				fmt.Printf("Warning: DB check failed for %s: %v\n", sourceID, err)
				continue
			} else if exists {
				fmt.Printf("Skipped: Track already exists in library (ID: %s)\n\n", sourceID)
				dbSkippedCount++
				continue
			}
		}

		fmt.Printf("Target: %s\n", url)

		cfg := downloader.Config{
			AbsoluteOutputPath: targetPath,
			URL:                url,
			MinSleep:           minSleep,
			MaxSleep:           maxSleep,
		}

		if err := downloader.Download(ctx, cfg); err != nil {
			fmt.Printf("Error downloading %s: %v\n\n", url, err)
			continue
		}

		successCount++
		fmt.Println()
	}

	return processResult{
		success: successCount,
		skipped: dbSkippedCount,
		failed:  len(urls) - successCount - dbSkippedCount,
	}
}
