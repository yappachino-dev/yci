package library

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

type Folder struct {
	ID          string
	DisplayName string
	ParentID    string
}

func (l *Library) GetFolder(id string) (Folder, error) {
	var f Folder
	var parentID sql.NullString

	query := `SELECT id, display_name, parent_id FROM folders WHERE id = ?`
	err := l.db.QueryRow(query, id).Scan(&f.ID, &f.DisplayName, &parentID)
	if err != nil {
		if err == sql.ErrNoRows {
			return f, fmt.Errorf("folder %q not found", id)
		}
		return f, fmt.Errorf("database error fetching folder %q: %w", id, err)
	}

	if parentID.Valid {
		f.ParentID = parentID.String
	} else {
		f.ParentID = RootID
	}

	return f, nil
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

func (l *Library) buildFolderPath(f Folder) (string, error) {
	parentPath, err := l.GetFolderPath(f.ParentID)
	if err != nil {
		return "", fmt.Errorf("could not resolve new parent path: %w", err)
	}

	if parentPath == "" {
		return f.DisplayName, nil
	}
	return parentPath + "/" + f.DisplayName, nil
}

func (l *Library) IsFolderEmpty(folderID string) (bool, error) {
	var count int

	err := l.db.QueryRow("SELECT COUNT(*) FROM folders WHERE parent_id = ?", folderID).Scan(&count)
	if err != nil {
		return false, err
	}
	if count > 0 {
		return false, nil
	}

	err = l.db.QueryRow("SELECT COUNT(*) FROM artists WHERE folder_id = ?", folderID).Scan(&count)
	if err != nil {
		return false, err
	}

	return count == 0, nil
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

func (l *Library) RemoveFolder(id string) error {
	res, err := l.db.Exec("DELETE FROM folders WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("failed to delete folder %q: %w", id, err)
	}

	rows, _ := res.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("folder %q not found", id)
	}
	return nil
}

func (l *Library) UpdateFolder(oldID string, newFolder Folder) error {
	oldRelativePath, err := l.GetFolderPath(oldID)
	if err != nil {
		return err
	}

	newRelativePath, err := l.buildFolderPath(newFolder)
	if err != nil {
		return err
	}

	oldAbsPath := filepath.Join(l.Paths.Library, oldRelativePath)
	newAbsPath := filepath.Join(l.Paths.Library, newRelativePath)

	if oldAbsPath != newAbsPath {
		if err := os.Rename(oldAbsPath, newAbsPath); err != nil {
			return fmt.Errorf("failed to move folder on disk from %q to %q: %w", oldAbsPath, newAbsPath, err)
		}
	}

	query := `UPDATE folders SET id = ?, display_name = ?, parent_id = ? WHERE id = ?`
	if _, err := l.db.Exec(query, newFolder.ID, newFolder.DisplayName, newFolder.ParentID, oldID); err != nil {
		// TODO: os rollback
		return fmt.Errorf("failed to update folder in db: %w", err)
	}

	trackQuery := `UPDATE tracks 
	               SET relative_path = REPLACE(relative_path, ?, ?) 
	               WHERE relative_path LIKE ?`

	oldPrefix := oldRelativePath + "/"
	newPrefix := newRelativePath + "/"
	likeTarget := oldPrefix + "%"

	if _, err := l.db.Exec(trackQuery, oldPrefix, newPrefix, likeTarget); err != nil {
		return fmt.Errorf("warning: folder moved, but failed to update track paths: %w", err)
	}

	return nil
}
