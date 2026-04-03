// Package library does manage
package library

import (
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

func (ft FolderTree) GetPath(targetID string) string {
	var segments []string
	currentID := targetID

	for currentID != root {
		found := false

		for _, f := range ft {
			if f.id == currentID {
				segments = append(segments, f.displayName)
				currentID = f.parent
				found = true
				break
			}
		}

		if !found {
			break
		}
	}

	slices.Reverse(segments)

	return strings.Join(segments, "/")
}

func (ft FolderTree) ToMap() FolderMap {
	fMap := make(map[string]Folder, len(ft))

	for _, f := range ft {
		fMap[f.id] = f
	}

	return fMap
}

func (fm FolderMap) GetPath(targetID string) string {
	var segments []string
	currentID := targetID

	for currentID != root {
		f, ok := fm[currentID]

		if !ok {
			return strings.Join(segments, "/")
		}

		segments = append(segments, f.displayName)
		currentID = f.parent
	}

	slices.Reverse(segments)

	return strings.Join(segments, "/")
}
