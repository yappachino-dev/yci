package library

import (
	"database/sql"
	"fmt"
	"slices"
	"strings"
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
			f.ParentID = RootID
		}

		folders = append(folders, f)
	}

	return folders, nil
}

func (l *Library) GetFolderPath(folderID string) (string, error) {
	var segments []string
	currentID := folderID

	for currentID != RootID && currentID != "" {
		var name string
		var parentID sql.NullString

		err := l.db.QueryRow("SELECT display_name, parent_id FROM folders WHERE id = ?", currentID).Scan(&name, &parentID)
		if err != nil {
			if err == sql.ErrNoRows {
				return "", fmt.Errorf("folder %q not found in hierarchy", currentID)
			}
			return "", fmt.Errorf("database error resolving path for %q: %w", currentID, err)
		}

		segments = append(segments, name)

		if parentID.Valid {
			currentID = parentID.String
		} else {
			currentID = RootID
		}
	}

	slices.Reverse(segments)

	return strings.Join(segments, "/"), nil
}

func (l *Library) AddFolder(f Folder) error {
	query := "INSERT INTO folders (id, display_name, parent_id) VALUES (?, ?, ?)"

	var parentID any = f.ParentID
	if f.ParentID == "" || f.ParentID == RootID {
		parentID = nil
	}

	if _, err := l.db.Exec(query, f.ID, f.DisplayName, parentID); err != nil {
		return fmt.Errorf("failed to add %q folder: %w", f.ID, err)
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
	for currentParent != RootID && currentParent != "" {
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
			currentParent = RootID
		}
	}

	var parent any = newParentID
	if newParentID == "" || newParentID == RootID {
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
