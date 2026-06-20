package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"

	"playlist-manager/internal/download"

	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"
)

const (
	flagFile          = "file"
	flagQueueOnly     = "queue-only"
	flagInteractive   = "interactive"
	flagForcePlaylist = "force-playlist"
)

var downloadCmd = &cobra.Command{
	Use:   "download [urls...]",
	Short: "Download YouTube audio directly to the staging area",
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		if err := initApplication(cmd.Context()); err != nil {
			return err
		}

		if err := download.CheckDependencies(); err != nil {
			return err
		}

		return nil
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		urls := args

		filePath, _ := cmd.Flags().GetString(flagFile)
		if filePath != "" {
			fileUrls, err := readUrlsFromFile(filePath)
			if err != nil {
				return fmt.Errorf("read urls from file: %w", err)
			}

			urls = append(urls, fileUrls...)
		}

		if len(urls) == 0 {
			return fmt.Errorf("no URLs provided. Pass links as arguments or use the --file flag")
		}

		var selector download.PlaylistSelector
		if interactive, _ := cmd.Flags().GetBool(flagInteractive); interactive {
			selector = &EnqueueSelector{}
		}

		forcePlaylist, _ := cmd.Flags().GetBool(flagForcePlaylist)

		result, err := appl.DownloadService.Enqueue(ctx, download.EnqueueParams{
			URLs:          urls,
			Reporter:      &EnqueueReporter{},
			Selector:      selector,
			ForcePlaylist: forcePlaylist,
		})
		if err != nil {
			if errors.Is(err, huh.ErrUserAborted) {
				fmt.Println("\n[!] Operation cancelled by user.")
				return nil
			}

			return err
		}

		fmt.Printf("Queue complete! Enqueued: %d | Skipped: %d | Failed: %d\n",
			result.Enqueued, result.Skipped, result.Failed)

		queueOnly, _ := cmd.Flags().GetBool(flagQueueOnly)
		if queueOnly {
			fmt.Println("\n[!] Flag --queue-only detected. Items enqueued but download paused")
			return nil
		}

		delayFlag, _ := cmd.Flags().GetString(flagDelay)

		fmt.Print("\nProcessing queue...\n")
		return executeDownloadLoop(ctx, delayFlag)
	},
}

type EnqueueReporter struct{}

func (r *EnqueueReporter) OnHybridURL(url string, forcePlaylist bool) {
	if forcePlaylist {
		fmt.Printf("[!] Extracting every video from hybrid-playlist URL: %s\n", url)
		return
	}

	fmt.Printf("[!] Extracting video from hybrid URL: %s\n", url)
}

func (r *EnqueueReporter) OnPlaylistURL(url string) {
	fmt.Printf("[!] Extracting every video from playlist URL: %s\n", url)
}

func (r *EnqueueReporter) OnEnqueueSkipped(id string, reason string) {
	fmt.Printf("[-] Skipped %s: %s\n", id, reason)
}

type EnqueueSelector struct{}

func (s *EnqueueSelector) SelectChannels(ctx context.Context, channels []string) ([]string, error) {
	var selected []string

	options := huh.NewOptions(channels...)

	form := huh.NewForm(huh.NewGroup(
		huh.NewNote().Title("Select channels that will be downloaded"),
		huh.NewMultiSelect[string]().
			Options(options...).
			Value(&selected),
	)).WithTheme(huh.ThemeBase())

	if err := form.Run(); err != nil {
		return nil, fmt.Errorf("failed to read terminal input: %w", err)
	}

	return selected, nil
}

var downloadResumeCmd = &cobra.Command{
	Use:   "resume",
	Short: "Process all enqueued downloads",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()

		delayFlag, _ := cmd.Flags().GetString(flagDelay)

		fmt.Print("Resuming queue...\n")
		return executeDownloadLoop(ctx, delayFlag)
	},
}

func init() {
	rootCmd.AddCommand(downloadCmd)

	downloadCmd.AddCommand(downloadResumeCmd)
	downloadCmd.PersistentFlags().String(flagDelay, "5:30", "Randomized delay between downloads in seconds (format min:max)")
	downloadCmd.Flags().String(flagFile, "", "Read a list of URLs from a text file (one per line)")
	downloadCmd.Flags().Bool(flagQueueOnly, false, "Add tracks to the queue without starting the download")
	downloadCmd.Flags().Bool(flagInteractive, false, "Dynamically select channels upon downloading playlist")
	downloadCmd.Flags().Bool(flagForcePlaylist, false, "Extract playlist ID instead of video ID from hybrid URLs")
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

type ProcessReporter struct{}

func (r *ProcessReporter) OnQueueStart(totalTracks int) {}

func (r *ProcessReporter) OnTrackStart(index int, total int, sourceID string) {
	fmt.Printf("--- [Track %d / %d] ---\n", index, total)
	fmt.Printf("[!] Target: %s\n", sourceID)
}

func (r *ProcessReporter) OnTrackSuccess(_ string) {
	fmt.Printf("[+] Success!\n\n")
}

func (r *ProcessReporter) OnTrackFailure(_ string, err error) {
	fmt.Printf("[-] Failed: %v\n\n", err)
}

func executeDownloadLoop(ctx context.Context, delayFlag string) error {
	delayParts := strings.Split(delayFlag, ":")
	if len(delayParts) != 2 {
		return fmt.Errorf("invalid delay format. Expected min:max (e.g., 5:30)")
	}

	result, err := appl.DownloadService.ProcessQueue(ctx, delayParts[0], delayParts[1], &ProcessReporter{})
	if err != nil {
		return err
	}

	fmt.Printf("Queue processed! Success: %d | Failed: %d\n", result.Success, result.Failed)

	if result.Success > 0 {
		fmt.Println("▶ Run `yci process staging` to import them into your library.")
	}

	return nil
}
