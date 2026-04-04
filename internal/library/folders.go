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
	ParentID    string
	DisplayName string
}

type (
	folderTree []Folder
	folderMap  map[string]Folder
)

func (ft folderTree) toMap() folderMap {
	fMap := make(folderMap, len(ft))

	for _, f := range ft {
		fMap[f.ID] = f
	}

	return fMap
}

func (ft folderTree) validate() error {
	fm := ft.toMap()

	for _, f := range ft {
		if f.ParentID != root {
			if _, exists := fm[f.ParentID]; !exists {
				return fmt.Errorf("folder %q has a non-existing parent %q", f.ID, f.ParentID)
			}
		}
	}

	return nil
}

func (fm folderMap) getPath(targetID string) (string, error) {
	var segments []string
	currentID := targetID

	for currentID != root {
		f, exists := fm[currentID]

		if !exists {
			return "", fmt.Errorf("folder %q does not exist", currentID)
		}

		segments = append(segments, f.DisplayName)
		currentID = f.ParentID
	}

	slices.Reverse(segments)

	return strings.Join(segments, "/"), nil
}
