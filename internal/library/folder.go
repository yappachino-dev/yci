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

func scanFolder(s scannable) (Folder, error) {
	var f Folder
	var parentID sql.NullString

	err := s.Scan(&f.ID, &f.DisplayName, &parentID)
	f.ParentID = fromNull(parentID, RootID)

	return f, err
}

func (l *Library) GetFolder(id string) (Folder, error) {
	query := `SELECT id, display_name, parent_id FROM folders WHERE id = ?`

	f, err := scanFolder(l.db.QueryRow(query, id))

	return f, l.wrapErr(err, "folder", id)
}

func (l *Library) GetFolders() ([]Folder, error) {
	query := `SELECT id, display_name, parent_id FROM folders`

	rows, err := l.db.Query(query)
	if err != nil {
		return nil, fmt.Errorf("failed to query folders: %w", err)
	}
	defer rows.Close()

	var folders []Folder
	for rows.Next() {
		f, err := scanFolder(rows)
		if err != nil {
			return nil, err
		}
		folders = append(folders, f)
	}

	return folders, nil
}

func (l *Library) ResolveFolderPath(folderID string) (string, error) {
	var segments []string
	currentID := folderID

	for currentID != RootID && currentID != "" {
		f, err := l.GetFolder(currentID)
		if err != nil {
			return "", fmt.Errorf("path resolution failed: %w", err)
		}

		segments = append(segments, f.DisplayName)
		currentID = f.ParentID
	}

	slices.Reverse(segments)

	return filepath.ToSlash(filepath.Join(segments...)), nil
}

func (l *Library) PredictFolderPath(f Folder) (string, error) {
	parentPath, err := l.ResolveFolderPath(f.ParentID)
	if err != nil {
		return "", fmt.Errorf("could not resolve proposed parent: %w", err)
	}

	if parentPath == "" {
		return f.DisplayName, nil
	}

	return filepath.ToSlash(filepath.Join(parentPath, f.DisplayName)), nil
}

func (l *Library) IsFolderEmpty(folderID string) (bool, error) {
	if _, err := l.GetFolder(folderID); err != nil {
		return false, err
	}

	query := `
		SELECT EXISTS (
			SELECT 1 FROM folders WHERE parent_id = ?
			UNION ALL
			SELECT 1 FROM artists WHERE folder_id = ?
		)`

	var hasChildren bool
	err := l.db.QueryRow(query, folderID, folderID).Scan(&hasChildren)
	if err != nil {
		return false, fmt.Errorf("failed to check folder contents for %q: %w", folderID, err)
	}

	return !hasChildren, nil
}

func (l *Library) AddFolder(f Folder) error {
	query := `INSERT INTO folders (id, display_name, parent_id) VALUES (?, ?, ?)`

	_, err := l.db.Exec(query,
		strings.TrimSpace(f.ID),
		strings.TrimSpace(f.DisplayName),
		toNull(f.ParentID),
	)
	if err != nil {
		return fmt.Errorf("failed to add folder %q: %w", f.ID, err)
	}

	return nil
}

func (l *Library) RemoveFolder(id string) error {
	res, err := l.db.Exec("DELETE FROM folders WHERE id = ?", id)
	if err != nil {
		return l.wrapErr(err, "folder", id)
	}

	if rows, _ := res.RowsAffected(); rows == 0 {
		return l.wrapErr(sql.ErrNoRows, "folder", id)
	}

	return nil
}

func (l *Library) UpdateFolder(oldID string, updated Folder) error {
	oldRel, err := l.ResolveFolderPath(oldID)
	if err != nil {
		return err
	}

	newRel, err := l.PredictFolderPath(updated)
	if err != nil {
		return err
	}

	tx, err := l.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	query := `UPDATE folders SET id = ?, display_name = ?, parent_id = ? WHERE id = ?`
	if _, err := tx.Exec(query, strings.TrimSpace(updated.ID), updated.DisplayName, toNull(updated.ParentID), oldID); err != nil {
		return fmt.Errorf("failed to update folder in database: %w", err)
	}

	if oldRel != newRel {
		trackQuery := `UPDATE tracks
	               SET relative_path = REPLACE(relative_path, ?, ?)
	               WHERE relative_path LIKE ?`

		oldPrefix := oldRel + "/"
		newPrefix := newRel + "/"

		if _, err := tx.Exec(trackQuery, oldPrefix, newPrefix, oldPrefix+"%"); err != nil {
			return fmt.Errorf("folder moved, but failed to update nested track paths: %w", err)
		}

		oldAbs := filepath.Join(l.Paths.Library, filepath.FromSlash(oldRel))
		newAbs := filepath.Join(l.Paths.Library, filepath.FromSlash(newRel))

		if err := os.MkdirAll(filepath.Dir(newAbs), 0o755); err != nil {
			return fmt.Errorf("failed to create destination directory: %w", err)
		}
		if err := os.Rename(oldAbs, newAbs); err != nil {
			return fmt.Errorf("failed to move folder on disk: %w", err)
		}
	}

	return tx.Commit()
}
