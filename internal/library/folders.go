// Package library does manage
package library

import (
	"fmt"
	"slices"
	"strings"
)

const (
	root string = "root"
)

type Folder struct {
	id          string
	parent      string
	displayName string
}

type (
	FolderTree []Folder
	FolderMap  map[string]Folder
)

func (ft FolderTree) ToMap() FolderMap {
	fMap := make(map[string]Folder, len(ft))

	for _, f := range ft {
		fMap[f.id] = f
	}

	return fMap
}

func (ft FolderTree) Validate() error {
	fm := ft.ToMap()

	for _, f := range ft {
		if f.parent != root {
			if _, exists := fm[f.parent]; !exists {
				return fmt.Errorf("folder %q has a non-existing parent %q", f.id, f.parent)
			}
		}
	}

	return nil
}

func (fm FolderMap) GetPath(targetID string) (string, error) {
	var segments []string
	currentID := targetID

	for currentID != root {
		f, ok := fm[currentID]

		if !ok {
			return "", fmt.Errorf("folder %q does not exist", currentID)
		}

		segments = append(segments, f.displayName)
		currentID = f.parent
	}

	slices.Reverse(segments)

	return strings.Join(segments, "/"), nil
}
