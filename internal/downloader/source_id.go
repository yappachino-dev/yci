package downloader

import "regexp"

var ytRegex = regexp.MustCompile(`(?:youtube\.com\/(?:[^\/]+\/.+\/|(?:v|e(?:mbed)?)\/|.*[?&]v=)|youtu\.be\/)([^"&?\/\s]{11})`)

func ExtractSourceID(url string) string {
	matches := ytRegex.FindStringSubmatch(url)
	if len(matches) > 1 {
		return matches[1]
	}
	return ""
}
