// Package utils provides common utils
package utils

import (
	"fmt"
	"os/exec"
	"regexp"
	"strings"
)

var ytRegex = regexp.MustCompile(`(?:youtube\.com\/(?:[^\/]+\/.+\/|(?:v|e(?:mbed)?)\/|.*[?&]v=)|youtu\.be\/)([^"&?\/\s]{11})`)

func ExtractSourceID(url string) string {
	matches := ytRegex.FindStringSubmatch(url)
	if len(matches) > 1 {
		return matches[1]
	}
	return ""
}

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

func NameToSlug(name string) string {
	return strings.ToLower(strings.ReplaceAll(name, " ", "-"))
}
