package library

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"playlist-manager/internal/appctx"
	"playlist-manager/internal/db"
	"playlist-manager/internal/store"
)

const RootID string = "root"

type (
	FolderID   string
	FolderName string
)

func (n FolderName) ToSlugID() FolderID {
	return FolderID(nameToSlug(string(n)))
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

type Folder struct {
	ID       FolderID
	Name     FolderName
	ParentID FolderID
}

type FolderRepository interface {
	Create(ctx context.Context, f Folder) error
	ByID(ctx context.Context, id FolderID) (Folder, error)
	All(ctx context.Context) ([]Folder, error)
	Update(ctx context.Context, oldID FolderID, a Folder) error
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

type FolderServiceConfig struct {
	FolderRepo  FolderRepository
	TrackWriter TracksPathWriter
	DiskMover   PhysicalMover
	Transactor  Transactor
	RootFolder  FolderID
	Config      StagingConfig
}

func NewFolderService(cfg FolderServiceConfig) *FolderService {
	return &FolderService{
		repo:        cfg.FolderRepo,
		trackWriter: cfg.TrackWriter,
		diskMover:   cfg.DiskMover,
		transactor:  cfg.Transactor,
		rootFolder:  cfg.RootFolder,
		config:      cfg.Config,
	}
}

func (s *FolderService) Create(ctx context.Context, id FolderID, parentID FolderID, name FolderName) (Folder, error) {
	if err := name.Validate(); err != nil {
		return Folder{}, fmt.Errorf("create folder: %w", err)
	}

	if id == "" {
		id = name.ToSlugID()
	}

	if parentID == "" {
		parentID = s.rootFolder
	}

	f := Folder{
		ID:       id,
		Name:     name,
		ParentID: parentID,
	}

	if err := s.repo.Create(ctx, f); err != nil {
		return Folder{}, fmt.Errorf("create folder %q: %w", f.ID, err)
	}

	return f, nil
}

func (s *FolderService) ByID(ctx context.Context, id FolderID) (Folder, error) {
	f, err := s.repo.ByID(ctx, id)
	if err != nil {
		return Folder{}, fmt.Errorf("folder by id %q: %w", id, err)
	}

	return f, nil
}

func (s *FolderService) All(ctx context.Context) ([]Folder, error) {
	f, err := s.repo.All(ctx)
	if err != nil {
		return nil, fmt.Errorf("all folders: %w", err)
	}

	return f, nil
}

func (s *FolderService) IsEmpty(ctx context.Context, id FolderID) (bool, error) {
	hasChildren, err := s.repo.HasChildren(ctx, id)
	if err != nil {
		return false, fmt.Errorf("is folder empty: %w", err)
	}
	return !hasChildren, nil
}

type FolderUpdateParams struct {
	NewID            FolderID
	Name             FolderName
	Parent           FolderID
	RegenerateSlugID bool
}

func (s *FolderService) Update(ctx context.Context, oldID FolderID, params FolderUpdateParams) error {
	oldFolder, err := s.repo.ByID(ctx, oldID)
	if err != nil {
		return fmt.Errorf("update folder %q: %w", oldID, err)
	}

	newID := newIfPresent(params.NewID, oldID)
	newName := newIfPresent(params.Name, oldFolder.Name)
	newParent := newIfPresent(params.Parent, oldFolder.ParentID)

	if params.RegenerateSlugID {
		newID = FolderID(nameToSlug(string(newName)))
	}

	ancestors, err := s.repo.Ancestors(ctx, newParent)
	if err != nil {
		return fmt.Errorf("update folder %q: %w", oldID, err)
	}

	if slices.Contains(ancestors, string(oldID)) {
		return fmt.Errorf("update folder %q: new parent cannot be a child of current folder", oldID)
	}

	newFolder := Folder{
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
		return "", fmt.Errorf("resolve path %q: no ancestors found", id)
	}

	return filepath.ToSlash(filepath.Join(names...)), nil
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

func (r *SQLiteFolderRepository) Create(ctx context.Context, a Folder) error {
	return r.db(ctx).CreateFolder(ctx, store.CreateFolderParams{
		ID:          string(a.ID),
		DisplayName: string(a.Name),
		ParentID:    db.ToNullString(a.ParentID.ToDB(r.rootID)),
	})
}

func (r *SQLiteFolderRepository) ByID(ctx context.Context, id FolderID) (Folder, error) {
	row, err := r.db(ctx).GetFolder(ctx, string(id))
	if err != nil {
		return Folder{}, err
	}

	return Folder{
		ID:       FolderID(row.ID),
		Name:     FolderName(row.DisplayName),
		ParentID: FolderID(db.FromNullString(row.ParentID)).FromDB(r.rootID),
	}, nil
}

func (r *SQLiteFolderRepository) All(ctx context.Context) ([]Folder, error) {
	rows, err := r.db(ctx).GetFolders(ctx)
	if err != nil {
		return nil, err
	}

	folders := make([]Folder, 0, len(rows))
	for _, row := range rows {
		folders = append(folders, Folder{
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

func (r *SQLiteFolderRepository) Update(ctx context.Context, id FolderID, a Folder) error {
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
