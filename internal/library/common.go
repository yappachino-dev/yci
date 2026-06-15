package library

import "strings"

func nameToSlug(name string) string {
	return strings.ToLower(strings.ReplaceAll(name, " ", "-"))
}

func newIfPresent[T ~string](new, old T) T {
	if new != "" {
		return new
	} else {
		return old
	}
}
