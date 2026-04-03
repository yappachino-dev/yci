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
	ID          string
	Parent      string
	DisplayName string
}

type (
	FolderTree []Folder
	FolderMap  map[string]Folder
)

func (ft FolderTree) ToMap() FolderMap {
	fMap := make(map[string]Folder, len(ft))

	for _, f := range ft {
		fMap[f.ID] = f
	}

	return fMap
}

func (ft FolderTree) Validate() error {
	fm := ft.ToMap()

	for _, f := range ft {
		if f.Parent != root {
			if _, exists := fm[f.Parent]; !exists {
				return fmt.Errorf("folder %q has a non-existing parent %q", f.ID, f.Parent)
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

		segments = append(segments, f.DisplayName)
		currentID = f.Parent
	}

	slices.Reverse(segments)

	return strings.Join(segments, "/"), nil
}
