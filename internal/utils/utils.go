// Package utils provides common utils
package utils

import (
	"fmt"
	"os/exec"
	"strings"
)

func CheckDependencies() error {
	if _, err := exec.LookPath("yt-dlp"); err != nil {
		return fmt.Errorf("yt-dlp is not installed or not in PATH")
	}
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		return fmt.Errorf("ffmpeg is not installed or not in PATH")
	}

	return nil
}

func SanitizeFilename(name string) string {
	illegalChars := []string{"/", "\\", "?", "%", "*", ":", "|", "\"", "<", ">"}

	for _, char := range illegalChars {
		name = strings.ReplaceAll(name, char, "_")
	}

	return strings.TrimSpace(name)
}
