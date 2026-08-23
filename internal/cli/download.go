package cli

import (
	"bufio"
	"cmp"
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/yappachino-dev/yci/internal/config"
	"github.com/yappachino-dev/yci/internal/download"

	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"
)

const (
	flagFile          = "file"
	flagCookies       = "cookies"
	flagQueueOnly     = "queue-only"
	flagInteractive   = "interactive"
	flagForcePlaylist = "force-playlist"
)

var downloadCmd = &cobra.Command{
	Use:   "download [urls...]",
	Short: "Download YouTube audio directly to the staging area",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := download.CheckDependencies(); err != nil {
			return err
		}

		ctx := cmd.Context()
		c := Container(ctx)
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

		result, err := c.DownloadService.Enqueue(ctx, download.EnqueueParams{
			URLs:          urls,
			Reporter:      newConsoleReporter(cmd),
			Selector:      selector,
			ForcePlaylist: forcePlaylist,
		})
		if err != nil {
			if errors.Is(err, huh.ErrUserAborted) {
				cmd.Println("[!] Operation cancelled by user.")
				return nil
			}

			if errors.Is(err, errNoChannelsSelected) {
				cmd.Println("[!] No channels selected — nothing enqueued.")
				return nil
			}

			return err
		}

		cmd.Printf("Queue complete! Enqueued: %d | Skipped: %d | Failed: %d\n",
			result.Enqueued, result.Skipped, result.Failed)

		queueOnly, _ := cmd.Flags().GetBool(flagQueueOnly)
		if queueOnly {
			cmd.Println()
			cmd.Println("[!] Flag --queue-only detected. Items enqueued but download paused")
			return nil
		}

		cmd.Println()
		cmd.Println("Processing queue...")
		return executeDownloadLoop(cmd)
	},
}

// consoleReporter prints enqueue and download-progress events to the
// command's output. The download package defines the event set; this type
// only formats it.
type consoleReporter struct {
	cmd *cobra.Command
}

func newConsoleReporter(cmd *cobra.Command) *consoleReporter {
	return &consoleReporter{cmd: cmd}
}

func (r *consoleReporter) OnHybridURL(url string, forcePlaylist bool) {
	if forcePlaylist {
		r.cmd.Printf("[!] Extracting every video from hybrid-playlist URL: %s\n", url)
		return
	}

	r.cmd.Printf("[!] Extracting video from hybrid URL: %s\n", url)
}

func (r *consoleReporter) OnPlaylistURL(url string) {
	r.cmd.Printf("[!] Extracting every video from playlist URL: %s\n", url)
}

func (r *consoleReporter) OnEnqueueSkipped(id string, reason string) {
	r.cmd.Printf("[-] Skipped %s: %s\n", id, reason)
}

func (r *consoleReporter) OnQueueStart(totalTracks int) {}

func (r *consoleReporter) OnTrackStart(index int, total int, sourceID string) {
	r.cmd.Printf("--- [Track %d / %d] ---\n", index, total)
	r.cmd.Printf("[!] Target: %s\n", sourceID)
}

func (r *consoleReporter) OnTrackSuccess(_ string) {
	r.cmd.Println("[+] Success!")
	r.cmd.Println()
}

func (r *consoleReporter) OnTrackFailure(_ string, err error) {
	r.cmd.Printf("[-] Failed: %v\n", err)
	r.cmd.Println()
}

// errNoChannelsSelected reports that the user submitted the channel
// picker without selecting anything.
var errNoChannelsSelected = errors.New("no channels selected")

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

	if len(selected) == 0 {
		return nil, errNoChannelsSelected
	}

	return selected, nil
}

var downloadRetryCmd = &cobra.Command{
	Use:   "retry [ids...]",
	Short: "Re-arm failed downloads for another attempt (no IDs: all hard-failed entries)",
	Args:  cobra.ArbitraryArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		c := Container(ctx)

		var ids []int
		for _, a := range args {
			id, err := strconv.Atoi(a)
			if err != nil {
				return fmt.Errorf("invalid queue ID %q: must be an integer (see yci ls queue)", a)
			}

			ids = append(ids, id)
		}

		result, err := c.DownloadService.RetryFailed(ctx, ids)
		if err != nil {
			return err
		}

		cmd.Printf("Re-armed %d failed download(s).\n", result.Retried)
		cmd.Println("▶ Run `yci download resume` to process them.")
		return nil
	},
}

var downloadResumeCmd = &cobra.Command{
	Use:   "resume",
	Short: "Process all enqueued downloads",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := download.CheckDependencies(); err != nil {
			return err
		}

		cmd.Println("Resuming queue...")
		return executeDownloadLoop(cmd)
	},
}

func init() {
	rootCmd.AddCommand(downloadCmd)

	downloadCmd.AddCommand(downloadResumeCmd)
	downloadCmd.AddCommand(downloadRetryCmd)
	downloadCmd.PersistentFlags().String(flagDelay, "", flagDelayComment+" (default: config or "+config.DefaultDelay+")")
	downloadCmd.PersistentFlags().String(flagCookies, "", "Path to a cookies.txt export for age-restricted content (default: config)")
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

// parseDelay validates the min:max delay flag up front so invalid values
// fail with a clear message instead of a cryptic yt-dlp error.
func parseDelay(flagValue string) (minStr, maxStr string, err error) {
	parts := strings.Split(flagValue, ":")
	if len(parts) != 2 {
		return "", "", fmt.Errorf("invalid delay format. Expected min:max (e.g., 5:30)")
	}

	for _, half := range parts {
		n, parseErr := strconv.Atoi(half)
		if parseErr != nil || n < 0 {
			return "", "", fmt.Errorf("invalid delay %q: both components must be non-negative integers", flagValue)
		}
	}

	return parts[0], parts[1], nil
}

// resolveFlagOrConfig prefers an explicitly set flag over the resolved
// config value.
func resolveFlagOrConfig(cmd *cobra.Command, name, cfgValue string) string {
	v, _ := cmd.Flags().GetString(name)
	return cmp.Or(strings.TrimSpace(v), cfgValue)
}

func executeDownloadLoop(cmd *cobra.Command) error {
	c := Container(cmd.Context())

	delay := resolveFlagOrConfig(cmd, flagDelay, c.Config.DownloadDelay)
	minSleep, maxSleep, err := parseDelay(delay)
	if err != nil {
		return err
	}

	cookiesFile := resolveFlagOrConfig(cmd, flagCookies, c.Config.CookiesFile)
	if cookiesFile != "" {
		if _, err := os.Stat(cookiesFile); err != nil {
			return fmt.Errorf("cookies file %q: %w", cookiesFile, err)
		}
	}

	result, err := c.DownloadService.ProcessQueue(cmd.Context(), download.QueueRunConfig{
		MinSleep:    minSleep,
		MaxSleep:    maxSleep,
		CookiesFile: cookiesFile,
	}, newConsoleReporter(cmd))
	if err != nil {
		return err
	}

	cmd.Printf("Queue processed! Success: %d | Failed: %d\n", result.Success, result.Failed)

	if result.Success > 0 {
		cmd.Println("▶ Run `yci import staging` to import them into your library.")
	}

	return nil
}
