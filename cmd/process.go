package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

// --- Flag variables ---

var (
	skipUnknown    bool
	fallbackArtist string
)

// --- Command Definitions ---

var processCmd = &cobra.Command{
	Use:     "process",
	Aliases: []string{"import", "sync"},
	Short:   "Process and import downloaded files",
}

var processStagingCmd = &cobra.Command{
	Use:     "staging",
	Aliases: []string{"queue", "pending"},
	Short:   "Process the staging directory and import .opus files",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runProcessStaging()
	},
}

// --- Initialization ---

func init() {
	rootCmd.AddCommand(processCmd)
	processCmd.AddCommand(processStagingCmd)

	processStagingCmd.Flags().BoolVar(&skipUnknown, "skip-unknown", false, "Automatically skip tracks with unknown channels")
	processStagingCmd.Flags().StringVarP(&fallbackArtist, "fallback-artist", "f", "", "Automatically link unknown channels to this Artist ID")
}

// --- Helper Functions ---

func runProcessStaging() error {
	if fallbackArtist != "" {
		if _, err := lib.GetArtist(fallbackArtist); err != nil {
			return fmt.Errorf("invalid fallback artist %q: %w", fallbackArtist, err)
		}
	}

	handler := func(channelName string, title string) (string, error) {
		if skipUnknown {
			return "skip", nil
		}
		if fallbackArtist != "" {
			return fallbackArtist, nil
		}

		reader := bufio.NewReader(os.Stdin)

		fmt.Printf("\n[?] Unknown Channel: %q\n", channelName)
		fmt.Printf("    Track: %q\n", title)
		fmt.Print("    Enter an existing Artist ID to link (or type 'skip'/'exit'): ")

		input, err := reader.ReadString('\n')
		if err != nil {
			return "", fmt.Errorf("failed to read terminal input: %w", err)
		}

		input = strings.TrimSpace(input)

		if input == "exit" || input == "quit" {
			fmt.Println("    Aborting process loop...")
			os.Exit(0)
		}

		return input, nil
	}

	fmt.Println("Processing staging directory...")

	if err := lib.ProcessStaging(handler); err != nil {
		return err
	}

	fmt.Println("\nSuccess! Staging queue processed.")
	return nil
}
