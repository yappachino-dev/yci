package library

import (
	"fmt"
)

type Library struct {
	Paths AppPaths

	index folderMap
}

func NewLibrary() *Library {
	// TODO: hardcoded
	ft := folderTree{
		{ID: "artists", ParentID: "root", DisplayName: "Artists"},
		{ID: "ado", ParentID: "artists", DisplayName: "Ado"},
		{ID: "vt", ParentID: "artists", DisplayName: "VT"},
		{ID: "hololive", ParentID: "vt", DisplayName: "Hololive"},
	}

	paths, err := GetPaths()
	if err != nil {
		fmt.Println("error :( %w", err)
	}

	return &Library{
		Paths: paths,
		index: ft.toMap(),
	}
}

func (l *Library) TreePrint() {
	treePrint(l.index)
}

func (l *Library) HasChildren(id string) bool {
	for _, f := range l.index {
		if f.ParentID == id {
			return true
		}
	}

	return false
}

func (l *Library) AddFolder(id string, parent string, name string) error {
	if _, exists := l.index[id]; exists {
		return fmt.Errorf("folder %q already exists", id)
	}

	if _, exists := l.index[parent]; !exists {
		return fmt.Errorf("folder %q does not exist", parent)
	}

	l.index[id] = Folder{id, parent, name}

	return nil
}

func (l *Library) RemoveFolder(id string) error {
	if _, exists := l.index[id]; !exists {
		return fmt.Errorf("folder %q does not exist", id)
	}

	if l.HasChildren(id) {
		return fmt.Errorf("folder %q has children", id)
	}

	// TODO: file check

	delete(l.index, id)

	return nil
}
