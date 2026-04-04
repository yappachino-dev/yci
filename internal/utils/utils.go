// Package utils provides common utils
package utils

import (
	"fmt"
	"os/exec"
	"strings"
)

func CheckHealth() {
	err := exec.Command("yt-dlp", "--version").Run()
	if err != nil {
		fmt.Printf(" yt-dlp not found\n%s", err)
	}
}

func SanitizeFilename(name string) string {
	illegalChars := []string{"/", "\\", "?", "%", "*", ":", "|", "\"", "<", ">"}

	for _, char := range illegalChars {
		name = strings.ReplaceAll(name, char, "_")
	}

	return strings.TrimSpace(name)
}
