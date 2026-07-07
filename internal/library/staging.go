package library

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/yappachino-dev/yci/internal/fs"

	"github.com/google/uuid"
)

var ErrProcessAborted = errors.New("processing aborted by user")

type StagingAction int

const (
	ActionImport StagingAction = iota
	ActionSkip
	ActionAbort
)

type StagingResult struct {
	Action     StagingAction
	Artist     ArtistID
	Folder     FolderID
	LinkArtist bool
}

type TrackCreator interface {
	Create(ctx context.Context, params TrackCreateParams) (Track, error)
}

type ArtistReader interface {
	FolderID(ctx context.Context, id ArtistID) (FolderID, error)
}

type ChannelLinker interface {
	Link(ctx context.Context, artistID ArtistID, name ChannelName) error
	ResolveArtist(ctx context.Context, name ChannelName) (ArtistID, bool, error)
	All(ctx context.Context) ([]Channel, error)
}

type MetadataReader interface {
	Read(ctx context.Context, path string) (TrackMetadata, error)
}

type StagingService struct {
	trackCreator   TrackCreator
	artistReader   ArtistReader
	folderResolver FolderResolver
	linker         ChannelLinker
	metaReader     MetadataReader
	diskMover      PhysicalTrackMover
	transactor     Transactor
	config         StagingConfig
}

type StagingServiceConfig struct {
	TrackCreator   TrackCreator
	ArtistReader   ArtistReader
	FolderResolver FolderResolver
	Linker         ChannelLinker
	MetaReader     MetadataReader
	DiskMover      PhysicalTrackMover
	Transactor     Transactor
	Config         StagingConfig
}

func NewStagingService(cfg StagingServiceConfig) *StagingService {
	return &StagingService{
		trackCreator:   cfg.TrackCreator,
		artistReader:   cfg.ArtistReader,
		folderResolver: cfg.FolderResolver,
		linker:         cfg.Linker,
		metaReader:     cfg.MetaReader,
		diskMover:      cfg.DiskMover,
		transactor:     cfg.Transactor,
		config:         cfg.Config,
	}
}

type TrackMetadata struct {
	Title  string
	Album  string
	Artist string
}

type LinkHandler func(channelName string, title string) (StagingResult, error)

func (s *StagingService) ProcessStaging(ctx context.Context, onUnknown LinkHandler) error {
	stagingDir := s.config.StagingDir()

	files, err := os.ReadDir(stagingDir)
	if err != nil {
		return fmt.Errorf("read staging directory: %w", err)
	}

	for _, file := range files {
		if file.IsDir() || filepath.Ext(file.Name()) != ".opus" {
			continue
		}

		if err := s.processSingleStagingFile(ctx, file.Name(), onUnknown); err != nil {
			if errors.Is(err, ErrProcessAborted) {
				return err
			}

			fmt.Printf("Error processing staging file %s: %v\n", file.Name(), err)
			continue
		}
	}

	return nil
}

func (s *StagingService) processSingleStagingFile(ctx context.Context, fileName string, onUnknown LinkHandler) error {
	oldPathNative := filepath.Join(s.config.StagingDir(), fileName)

	m, err := s.metaReader.Read(ctx, oldPathNative)
	if err != nil {
		return fmt.Errorf("extract track metadata: %w", err)
	}

	channelName := ChannelName(strings.TrimSpace(m.Artist))
	title := strings.TrimSpace(m.Title)
	sourceID := strings.TrimSuffix(fileName, ".opus")

	artistID, ok, err := s.linker.ResolveArtist(ctx, channelName)
	if err != nil {
		return err
	}

	var trackFolder FolderID

	if !ok {
		res, err := onUnknown(string(channelName), title)
		if err != nil {
			return err
		}

		switch res.Action {
		case ActionSkip:
			return nil
		case ActionAbort:
			return ErrProcessAborted
		case ActionImport:
			artistID = res.Artist
			trackFolder = res.Folder
		}

		if artistID != "" && res.LinkArtist {
			if err := s.linker.Link(ctx, artistID, channelName); err != nil {
				return fmt.Errorf("failed linking unknown channel to artist: %w", err)
			}
		}
	}

	var targetFolder FolderID

	switch {
	case trackFolder != "":
		targetFolder = trackFolder
	case artistID != "":
		targetFolder, err = s.artistReader.FolderID(ctx, artistID)
		if err != nil {
			return fmt.Errorf("failed tracking artist folder target: %w", err)
		}
	default:
		targetFolder = FolderID(RootID)
	}

	folderPath, err := s.folderResolver.ResolvePath(ctx, targetFolder)
	if err != nil {
		return fmt.Errorf("failed resolving destination path: %w", err)
	}

	safeTitle := fs.SanitizeFilename(title)
	newFilename := safeTitle + ".opus"

	var relPath string
	if folderPath == "" {
		relPath = newFilename
	} else {
		relPath = path.Join(folderPath, newFilename)
	}

	absPathNative := filepath.Join(s.config.LibraryDir(), filepath.FromSlash(relPath))

	txErr := s.transactor.WithinTransaction(ctx, func(txCtx context.Context) error {
		params := TrackCreateParams{
			ID:           TrackID(uuid.New().String()),
			Title:        title,
			Album:        m.Album,
			ArtistID:     artistID,
			FolderID:     targetFolder,
			RelativePath: TrackPath(relPath),
			SourceID:     sourceID,
		}

		if _, err := s.trackCreator.Create(txCtx, params); err != nil {
			return err
		}

		if err := s.diskMover.MoveFile(txCtx, oldPathNative, absPathNative); err != nil {
			return fmt.Errorf("move staging file to library: %w", err)
		}

		return nil
	})

	return txErr
}
