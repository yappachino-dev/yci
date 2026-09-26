package cli

import (
	"os"
	"path/filepath"
	"strings"
)

// packageSources reads this package's own .go files so a test can inspect the
// literals embedded in command bodies. RunE closures are not reachable by
// reflection, which is why the hint strings had no coverage at all.
func packageSources(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	var sources []string
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}

		body, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return nil, err
		}
		sources = append(sources, string(body))
	}

	return sources, nil
}
