// Package downloader uses yt-dlp to download music from youtube
package downloader

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

func Download(dir string, url string) {
	outputPath := filepath.Join(dir, "%(title)s.%(ext)s")

	cmd := exec.Command("yt-dlp",
		"--ignore-config",        // ignore user config
		"--no-cookies",           // don't use cookies to avoid account ban
		"--extract-audio",        // download only audio
		"--audio-format", "opus", // use opus audio-format
		"--audio-quality", "0", // best sound quality
		"--add-metadata",              // stitch metadata
		"--embed-thumbnail",           // stitch thumbnail
		"--convert-thumbnails", "jpg", // convert thumbnail to jpg
		"--sleep-interval", "5",
		"--max-sleep-interval", "30",
		"--output", outputPath,
		url,
	)

	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	err := cmd.Run()
	if err != nil {
		fmt.Print(err)
	}
}
