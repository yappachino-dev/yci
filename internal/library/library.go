package library

import (
	"database/sql"
	_ "embed"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

type Library struct {
	Paths AppPaths

	db *sql.DB
}

func NewLibrary() (*Library, error) {
	paths, err := GetPaths()
	if err != nil {
		return nil, err
	}

	if err := os.MkdirAll(paths.Config, 0o755); err != nil {
		return nil, err
	}

	db, err := sql.Open("sqlite", filepath.Join(paths.Config, "data.db"))
	if err != nil {
		return nil, err
	}

	return &Library{
		Paths: paths,

		db: db,
	}, nil
}

//go:embed init.sql
var initSQL string

func (l *Library) Init() error {
	if _, err := l.db.Exec("PRAGMA foreign_keys = ON;"); err != nil {
		return err
	}

	if _, err := l.db.Exec(initSQL); err != nil {
		return err
	}

	return nil
}

func (l *Library) Close() error {
	return l.db.Close()
}

func (l *Library) TreePrint() {
	l.treePrint()
}

func (l *Library) AddFolder(id string, name string, parentID string) error {
	query := "INSERT INTO folders (id, display_name, parent_id) VALUES (?, ?, ?)"

	var parent any = parentID
	if parentID == "" || parentID == "root" {
		parent = nil
	}

	if _, err := l.db.Exec(query, id, name, parent); err != nil {
		return fmt.Errorf("failed to add %q folder: %w", id, err)
	}

	return nil
}

func (l *Library) RemoveFolder(id string, force bool) error {
	if !force {
		var subFolders, subArtists int

		err := l.db.QueryRow("SELECT COUNT(*) FROM folders WHERE parent_id = ?", id).Scan(&subFolders)
		if err != nil {
			return fmt.Errorf("failed to check subfolders for %q: %w", id, err)
		}

		err = l.db.QueryRow("SELECT COUNT(*) FROM artists WHERE folder_id = ?", id).Scan(&subArtists)
		if err != nil {
			return fmt.Errorf("failed to check artists for %q: %w", id, err)
		}

		if subFolders > 0 || subArtists > 0 {
			return fmt.Errorf("folder %q is not empty (%d folders, %d artists). Use -f to force delete", id, subFolders, subArtists)
		}
	}

	// TODO: file check

	res, err := l.db.Exec("DELETE FROM folders WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("failed to remove folder %q: %w", id, err)
	}

	rows, _ := res.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("folder %q does not exist", id)
	}

	return nil
}

func (l *Library) MoveFolder(folderID string, newParentID string) error {
	if folderID == newParentID {
		return fmt.Errorf("cannot move folder %q into itself", folderID)
	}

	currentParent := newParentID
	for currentParent != "root" && currentParent != "" {
		if currentParent == folderID {
			return fmt.Errorf("cycle detected: cannot move folder %q into its own descendant %q", folderID, newParentID)
		}

		var nextParent sql.NullString
		err := l.db.QueryRow("SELECT parent_id FROM folders WHERE id = ?", currentParent).Scan(&nextParent)
		if err != nil {
			if err == sql.ErrNoRows {
				break
			}
			return fmt.Errorf("error checking folder hierarchy: %w", err)
		}

		if nextParent.Valid {
			currentParent = nextParent.String
		} else {
			currentParent = "root"
		}
	}

	var parent any = newParentID
	if newParentID == "" || newParentID == "root" {
		parent = nil
	}

	res, err := l.db.Exec("UPDATE folders SET parent_id = ? WHERE id = ?", parent, folderID)
	if err != nil {
		return fmt.Errorf("failed to move folder %q: %w", folderID, err)
	}

	rows, _ := res.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("folder %q does not exist", folderID)
	}

	return nil
}
