package fs

import "strings"

func SanitizeFilename(name string) string {
	illegalChars := []string{"/", "\\", "?", "%", "*", ":", "|", "\"", "<", ">"}

	for _, char := range illegalChars {
		name = strings.ReplaceAll(name, char, "_")
	}

	return strings.TrimSpace(name)
}
