// Package utils provides common utils
package utils

import (
	"fmt"
	"os/exec"
)

func CheckHealth() {
	err := exec.Command("yt-dlp", "--version").Run()
	if err != nil {
		fmt.Printf(" yt-dlp not found\n%s", err)
	}
}
