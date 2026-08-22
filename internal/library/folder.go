package library

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/yappachino-dev/yci/internal/appctx"
	"github.com/yappachino-dev/yci/internal/db"
	"github.com/yappachino-dev/yci/internal/fs"
	"github.com/yappachino-dev/yci/internal/store"
)

const RootID string = "root"

// Sentinel errors for folder operations.
var (
	ErrFolderNotFound      = errors.New("folder not found")
	ErrParentNotFound      = errors.New("parent folder not found")
	ErrFolderAlreadyExists = errors.New("folder already exists")
	ErrCyclicParent        = errors.New("new parent cannot be a child of current folder")
)

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

type Folder struct {
	ID       FolderID
	Name     FolderName
	ParentID FolderID
}

type FolderRepository interface {
	Create(ctx context.Context, f Folder) (Folder, error)
	ByID(ctx context.Context, id FolderID) (Folder, error)
	All(ctx context.Context) ([]Folder, error)
	Update(ctx context.Context, oldID FolderID, a Folder) (Folder, error)
	Delete(ctx context.Context, id FolderID) error

	Ancestors(ctx context.Context, id FolderID) ([]string, error)
	AncestorIDs(ctx context.Context, id FolderID) ([]string, error)
	HasChildren(ctx context.Context, id FolderID) (bool, error)
	Exists(ctx context.Context, id FolderID) (bool, error)
}

type TracksPathWriter interface {
	UpdatePathsByPrefix(ctx context.Context, oldPath, newPath string) error
}

type PhysicalMover interface {
	MoveDirectory(ctx context.Context, oldRootPath, newRootPath string) error
}

// FolderService manages folder entities and their physical directories.
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

	draft := Folder{
		ID:       id,
		Name:     name,
		ParentID: parentID,
	}

	saved, err := s.repo.Create(ctx, draft)
	if err != nil {
		return Folder{}, fmt.Errorf("create folder %q: %w", draft.ID, err)
	}

	return saved, nil
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

func (s *FolderService) AllIDs(ctx context.Context) ([]FolderID, error) {
	a, err := s.repo.All(ctx)
	if err != nil {
		return nil, fmt.Errorf("all folders: %w", err)
	}

	ids := make([]FolderID, 0, len(a))
	for _, i := range a {
		ids = append(ids, i.ID)
	}

	return ids, nil
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

func (s *FolderService) Update(ctx context.Context, oldID FolderID, params FolderUpdateParams) (Folder, error) {
	current, err := s.repo.ByID(ctx, oldID)
	if err != nil {
		return Folder{}, fmt.Errorf("update folder %q: %w", oldID, err)
	}

	newID := newIfPresent(params.NewID, oldID)
	newName := newIfPresent(params.Name, current.Name)
	newParent := newIfPresent(params.Parent, current.ParentID)

	if params.RegenerateSlugID {
		newID = FolderID(nameToSlug(string(newName)))
	}

	if newParent == oldID {
		return Folder{}, fmt.Errorf("update folder %q: %w", oldID, ErrCyclicParent)
	}

	if newParent != current.ParentID && newParent != s.rootFolder {
		parentExists, err := s.repo.Exists(ctx, newParent)
		if err != nil {
			return Folder{}, fmt.Errorf("update folder %q: %w", oldID, err)
		}
		if !parentExists {
			return Folder{}, fmt.Errorf("update folder %q: %w", oldID, ErrParentNotFound)
		}

		ancestorIDs, err := s.repo.AncestorIDs(ctx, newParent)
		if err != nil {
			return Folder{}, fmt.Errorf("update folder %q: %w", oldID, err)
		}

		if slices.Contains(ancestorIDs, string(oldID)) {
			return Folder{}, fmt.Errorf("update folder %q: %w", oldID, ErrCyclicParent)
		}
	}

	draft := Folder{
		ID:       newID,
		Name:     newName,
		ParentID: newParent,
	}

	currentPath, err := s.ResolvePath(ctx, oldID)
	if err != nil {
		return Folder{}, fmt.Errorf("update folder %q: %w", oldID, err)
	}

	draftPath, err := s.PredictPath(ctx, newParent, newName)
	if err != nil {
		return Folder{}, fmt.Errorf("update folder %q: %w", oldID, err)
	}

	if currentPath != draftPath {
		var saved Folder
		txErr := s.transactor.WithinTransaction(ctx, func(txCtx context.Context) error {
			currentPrefix := currentPath + "/"
			draftPrefix := draftPath + "/"

			if err := s.trackWriter.UpdatePathsByPrefix(txCtx, currentPrefix, draftPrefix); err != nil {
				return err
			}

			updated, err := s.repo.Update(txCtx, oldID, draft)
			if err != nil {
				return err
			}

			saved = updated

			currentAbs := filepath.Join(s.config.LibraryDir(), filepath.FromSlash(currentPath))
			draftAbs := filepath.Join(s.config.LibraryDir(), filepath.FromSlash(draftPath))

			if err := s.diskMover.MoveDirectory(txCtx, currentAbs, draftAbs); err != nil {
				return err
			}

			return nil
		})

		if txErr != nil {
			return Folder{}, fmt.Errorf("update folder %q: %w", oldID, txErr)
		}

		return saved, nil
	}

	saved, err := s.repo.Update(ctx, oldID, draft)
	if err != nil {
		return Folder{}, fmt.Errorf("update folder %q: %w", oldID, err)
	}

	return saved, nil
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

	segments := make([]string, len(names))
	for i, name := range names {
		segments[i] = fs.SanitizeFilename(name)
	}

	return filepath.ToSlash(filepath.Join(segments...)), nil
}

func (s *FolderService) PredictPath(ctx context.Context, parent FolderID, name FolderName) (string, error) {
	parentPath, err := s.ResolvePath(ctx, parent)
	if err != nil {
		return "", fmt.Errorf("predict path %q: %w", parent, err)
	}

	safeName := fs.SanitizeFilename(string(name))
	if parentPath == "" {
		return safeName, nil
	}

	return filepath.ToSlash(filepath.Join(parentPath, safeName)), nil
}

// SQLiteFolderRepository implements FolderRepository over sqlc queries.
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

func (r *SQLiteFolderRepository) Create(ctx context.Context, a Folder) (Folder, error) {
	row, err := r.db(ctx).CreateFolder(ctx, store.CreateFolderParams{
		ID:          string(a.ID),
		DisplayName: string(a.Name),
		ParentID:    db.ToNullString(string(a.ParentID)),
	})
	if err != nil {
		return Folder{}, r.mapError(err)
	}

	return Folder{
		ID:       FolderID(row.ID),
		Name:     FolderName(row.DisplayName),
		ParentID: FolderID(db.FromNullString(row.ParentID)),
	}, nil
}

func (r *SQLiteFolderRepository) ByID(ctx context.Context, id FolderID) (Folder, error) {
	row, err := r.db(ctx).GetFolder(ctx, string(id))
	if err != nil {
		return Folder{}, r.mapError(err)
	}

	return Folder{
		ID:       FolderID(row.ID),
		Name:     FolderName(row.DisplayName),
		ParentID: FolderID(db.FromNullString(row.ParentID)),
	}, nil
}

func (r *SQLiteFolderRepository) All(ctx context.Context) ([]Folder, error) {
	rows, err := r.db(ctx).GetFolders(ctx)
	if err != nil {
		return nil, r.mapError(err)
	}

	folders := make([]Folder, 0, len(rows))
	for _, row := range rows {
		folders = append(folders, Folder{
			ID:       FolderID(row.ID),
			Name:     FolderName(row.DisplayName),
			ParentID: FolderID(db.FromNullString(row.ParentID)),
		})
	}

	return folders, nil
}

func (r *SQLiteFolderRepository) Ancestors(ctx context.Context, id FolderID) ([]string, error) {
	return r.db(ctx).GetFolderAncestors(ctx, string(id))
}

// AncestorIDs returns folder IDs from the folder up to the root. Unlike
// Ancestors (display names), these are stable identifiers and safe for
// membership checks.
func (r *SQLiteFolderRepository) AncestorIDs(ctx context.Context, id FolderID) ([]string, error) {
	return r.db(ctx).GetFolderAncestorIDs(ctx, string(id))
}

func (r *SQLiteFolderRepository) HasChildren(ctx context.Context, id FolderID) (bool, error) {
	return r.db(ctx).CheckFolderHasChildren(ctx, db.ToNullString(string(id)))
}

func (r *SQLiteFolderRepository) Update(ctx context.Context, id FolderID, a Folder) (Folder, error) {
	row, err := r.db(ctx).UpdateFolder(ctx, store.UpdateFolderParams{
		OldID:       string(id),
		NewID:       string(a.ID),
		DisplayName: string(a.Name),
		ParentID:    db.ToNullString(string(a.ParentID)),
	})
	if err != nil {
		return Folder{}, r.mapError(err)
	}

	return Folder{
		ID:       FolderID(row.ID),
		Name:     FolderName(row.DisplayName),
		ParentID: FolderID(db.FromNullString(row.ParentID)),
	}, nil
}

func (r *SQLiteFolderRepository) Delete(ctx context.Context, id FolderID) error {
	rows, err := r.db(ctx).DeleteFolder(ctx, string(id))
	if err != nil {
		return r.mapError(err)
	}

	if rows == 0 {
		return ErrFolderNotFound
	}

	return nil
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

// mapError translates storage-level failures into folder sentinels.
func (r *SQLiteFolderRepository) mapError(err error) error {
	return db.MapSQLError(err, ErrFolderNotFound, ErrFolderAlreadyExists)
}
