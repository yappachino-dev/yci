package library

import (
	"database/sql"
	"fmt"
)

type Folder struct {
	ID          string
	DisplayName string
	ParentID    string
}

func (l *Library) GetFolders() ([]Folder, error) {
	query := "SELECT id, display_name, parent_id FROM folders"

	rows, err := l.db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var folders []Folder

	for rows.Next() {
		var f Folder
		var parent sql.NullString

		if err := rows.Scan(&f.ID, &f.DisplayName, &parent); err != nil {
			return nil, err
		}

		if parent.Valid {
			f.ParentID = parent.String
		} else {
			f.ParentID = root
		}

		folders = append(folders, f)
	}

	return folders, nil
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
