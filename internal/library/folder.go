package library

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"playlist-manager/internal/appctx"
	"playlist-manager/internal/db"
	"playlist-manager/internal/store"
	"playlist-manager/internal/utils"
)

type Folder struct {
	ID          string
	DisplayName string
	ParentID    string
}

type (
	FolderID   string
	FolderName string
)

func (n FolderName) ToSlugID() FolderID {
	return FolderID(utils.NameToSlug(string(n)))
}

func (n FolderName) Validate() error {
	if strings.TrimSpace(string(n)) == "" {
		return fmt.Errorf("folder name cannot be empty")
	}
	return nil
}

func (id FolderID) FromDB(rootID FolderID) FolderID {
	if string(id) == "" {
		return rootID
	}
	return id
}

func (id FolderID) ToDB(rootID FolderID) string {
	if id == rootID {
		return ""
	}
	return string(id)
}

type NFolder struct {
	ID       FolderID
	Name     FolderName
	ParentID FolderID
}

func scanFolder(s scannable) (Folder, error) {
	var f Folder
	var parentID sql.NullString

	if err := s.Scan(&f.ID, &f.DisplayName, &parentID); err != nil {
		return Folder{}, err
	}

	f.ParentID = fromNull(parentID, RootID)

	return f, nil
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

	_, err := l.db.Exec(
		query,
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

type FolderRepository interface {
	Create(ctx context.Context, f NFolder) error
	ByID(ctx context.Context, id FolderID) (NFolder, error)
	All(ctx context.Context) ([]NFolder, error)
	Update(ctx context.Context, oldID FolderID, a NFolder) error
	Delete(ctx context.Context, id FolderID) error

	Ancestors(ctx context.Context, id FolderID) ([]string, error)
	HasChildren(ctx context.Context, id FolderID) (bool, error)
}

type TracksPathWriter interface {
	UpdatePathsByPrefix(ctx context.Context, oldPath, newPath string) error
}

type PhysicalMover interface {
	MoveDirectory(ctx context.Context, oldRootPath, newRootPath string) error
}

type FolderService struct {
	repo        FolderRepository
	trackWriter TracksPathWriter
	diskMover   PhysicalMover
	transactor  Transactor
	rootFolder  FolderID
	config      StagingConfig
}

func NewFolderService(r FolderRepository, tm TracksPathWriter, dm PhysicalMover, t Transactor, root FolderID, cfg StagingConfig) *FolderService {
	return &FolderService{
		repo:        r,
		trackWriter: tm,
		diskMover:   dm,
		transactor:  t,
		rootFolder:  root,

		config: cfg,
	}
}

func (s *FolderService) Create(ctx context.Context, id FolderID, parentID FolderID, name FolderName) error {
	if err := name.Validate(); err != nil {
		return fmt.Errorf("create folder: %w", err)
	}

	if id == "" {
		id = name.ToSlugID()
	}

	if parentID == "" {
		parentID = s.rootFolder
	}

	f := NFolder{
		ID:       id,
		Name:     name,
		ParentID: parentID,
	}

	if err := s.repo.Create(ctx, f); err != nil {
		return fmt.Errorf("create folder %q: %w", f.ID, err)
	}

	return nil
}

func (s *FolderService) ByID(ctx context.Context, id FolderID) (NFolder, error) {
	f, err := s.repo.ByID(ctx, id)
	if err != nil {
		return NFolder{}, fmt.Errorf("get folder by id %q: %w", id, err)
	}

	return f, nil
}

func (s *FolderService) All(ctx context.Context) ([]NFolder, error) {
	f, err := s.repo.All(ctx)
	if err != nil {
		return nil, fmt.Errorf("get all folders: %w", err)
	}

	return f, nil
}

func (s *FolderService) IsEmpty(ctx context.Context, id FolderID) (bool, error) {
	hasChildren, err := s.repo.HasChildren(ctx, id)
	if err != nil {
		return false, fmt.Errorf("check folder protection safety: %w", err)
	}
	return !hasChildren, nil
}

type FolderUpdateParams struct {
	NewID  FolderID
	Name   FolderName
	Parent FolderID
}

func (s *FolderService) Update(ctx context.Context, oldID FolderID, params FolderUpdateParams) error {
	oldFolder, err := s.repo.ByID(ctx, oldID)
	if err != nil {
		return fmt.Errorf("update folder %q: %w", oldID, err)
	}

	newID := newIfPresent(params.NewID, oldID)
	newName := newIfPresent(params.Name, oldFolder.Name)
	newParent := newIfPresent(params.Parent, oldFolder.ParentID)

	newFolder := NFolder{
		ID:       newID,
		Name:     newName,
		ParentID: newParent,
	}

	oldPath, err := s.ResolvePath(ctx, oldID)
	if err != nil {
		return fmt.Errorf("update folder %q: %w", oldID, err)
	}

	newPath, err := s.PredictPath(ctx, newParent, newName)
	if err != nil {
		return fmt.Errorf("update folder %q: %w", oldID, err)
	}

	if oldPath != newPath {
		txErr := s.transactor.WithinTransaction(ctx, func(txCtx context.Context) error {
			oldPrefix := oldPath + "/"
			newPrefix := newPath + "/"

			if err := s.trackWriter.UpdatePathsByPrefix(txCtx, oldPrefix, newPrefix); err != nil {
				return err
			}

			if err := s.repo.Update(txCtx, oldID, newFolder); err != nil {
				return err
			}

			oldAbs := filepath.Join(s.config.LibraryDir(), filepath.FromSlash(oldPath))
			newAbs := filepath.Join(s.config.LibraryDir(), filepath.FromSlash(newPath))

			if err := s.diskMover.MoveDirectory(txCtx, oldAbs, newAbs); err != nil {
				return err
			}

			return nil
		})

		if txErr != nil {
			return fmt.Errorf("update folder %q: %w", oldID, txErr)
		}

		return nil
	}

	if err := s.repo.Update(ctx, oldID, newFolder); err != nil {
		return fmt.Errorf("update folder %q: %w", oldID, err)
	}

	return nil
}

func (s *FolderService) Delete(ctx context.Context, id FolderID) error {
	if err := s.repo.Delete(ctx, id); err != nil {
		return fmt.Errorf("delete folder: %w", err)
	}

	return nil
}

func (s *FolderService) ResolvePath(ctx context.Context, id FolderID) (string, error) {
	if id == s.rootFolder || id == "" {
		return "", nil
	}

	names, err := s.repo.Ancestors(ctx, id)
	if err != nil {
		return "", fmt.Errorf("resolve path %q: %w", id, err)
	}

	if len(names) == 0 {
		return "", fmt.Errorf("resolve path %q: %w", id, err)
	}

	segments := make([]string, 0, len(names))
	for _, name := range names {
		segments = append(segments, name)
	}

	return filepath.ToSlash(filepath.Join(segments...)), nil
}

func (s *FolderService) PredictPath(ctx context.Context, parent FolderID, name FolderName) (string, error) {
	parentPath, err := s.ResolvePath(ctx, parent)
	if err != nil {
		return "", fmt.Errorf("predict path %q: %w", parent, err)
	}

	if parentPath == "" {
		return string(name), nil
	}

	return filepath.ToSlash(filepath.Join(parentPath, string(name))), nil
}

type SQLiteFolderRepository struct {
	q      *store.Queries
	rootID FolderID
}

func NewSQLiteFolderRepository(q *store.Queries, rootID FolderID) *SQLiteFolderRepository {
	return &SQLiteFolderRepository{
		q:      q,
		rootID: rootID,
	}
}

func (r *SQLiteFolderRepository) Create(ctx context.Context, a NFolder) error {
	return r.db(ctx).CreateFolder(ctx, store.CreateFolderParams{
		ID:          string(a.ID),
		DisplayName: string(a.Name),
		ParentID:    db.ToNullString(a.ParentID.ToDB(r.rootID)),
	})
}

func (r *SQLiteFolderRepository) ByID(ctx context.Context, id FolderID) (NFolder, error) {
	row, err := r.db(ctx).GetFolder(ctx, string(id))
	if err != nil {
		return NFolder{}, err
	}

	return NFolder{
		ID:       FolderID(row.ID),
		Name:     FolderName(row.DisplayName),
		ParentID: FolderID(db.FromNullString(row.ParentID)).FromDB(r.rootID),
	}, nil
}

func (r *SQLiteFolderRepository) All(ctx context.Context) ([]NFolder, error) {
	rows, err := r.db(ctx).GetFolders(ctx)
	if err != nil {
		return nil, err
	}

	folders := make([]NFolder, 0, len(rows))
	for _, row := range rows {
		folders = append(folders, NFolder{
			ID:       FolderID(row.ID),
			Name:     FolderName(row.DisplayName),
			ParentID: FolderID(db.FromNullString(row.ParentID)).FromDB(r.rootID),
		})
	}

	return folders, nil
}

func (r *SQLiteFolderRepository) Ancestors(ctx context.Context, id FolderID) ([]string, error) {
	return r.db(ctx).GetFolderAncestors(ctx, string(id))
}

func (r *SQLiteFolderRepository) HasChildren(ctx context.Context, id FolderID) (bool, error) {
	return r.db(ctx).CheckFolderHasChildren(ctx, db.ToNullString(id.ToDB(r.rootID)))
}

func (r *SQLiteFolderRepository) Update(ctx context.Context, id FolderID, a NFolder) error {
	return r.db(ctx).UpdateFolder(ctx, store.UpdateFolderParams{
		OldID:       string(id),
		NewID:       string(a.ID),
		DisplayName: string(a.Name),
		ParentID:    db.ToNullString(a.ParentID.ToDB(r.rootID)),
	})
}

func (r *SQLiteFolderRepository) Delete(ctx context.Context, id FolderID) error {
	return r.db(ctx).DeleteFolder(ctx, string(id))
}

func (r *SQLiteFolderRepository) Exists(ctx context.Context, id FolderID) (bool, error) {
	return r.db(ctx).CheckFolderExists(ctx, string(id))
}

func (r *SQLiteFolderRepository) db(ctx context.Context) *store.Queries {
	if tx, ok := appctx.TxFromContext[*sql.Tx](ctx); ok {
		return r.q.WithTx(tx)
	}

	return r.q
}
