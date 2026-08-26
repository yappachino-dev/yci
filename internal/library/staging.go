package library

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/yappachino-dev/yci/internal/fs"

	"github.com/google/uuid"
)

var ErrProcessAborted = errors.New("processing aborted by user")

// StagingAction is the disposition chosen for a staged file.
type StagingAction int

const (
	ActionImport StagingAction = iota
	ActionSkip
	ActionAbort
)

// StagingResult communicates the handler's decision back to ProcessStaging.
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

// MetadataReader reads tag metadata from an audio file.
type MetadataReader interface {
	Read(path string) (TrackMetadata, error)
}

// StagingService imports finished downloads from the staging directory
// into the library.
type StagingService struct {
	trackCreator   TrackCreator
	artistReader   ArtistReader
	folderResolver FolderResolver
	linker         ChannelLinker
	metaReader     MetadataReader
	diskMover      PhysicalTrackMover
	transactor     Transactor
	paths          PathsProvider
}

type StagingServiceConfig struct {
	TrackCreator   TrackCreator
	ArtistReader   ArtistReader
	FolderResolver FolderResolver
	Linker         ChannelLinker
	MetaReader     MetadataReader
	DiskMover      PhysicalTrackMover
	Transactor     Transactor
	Paths          PathsProvider
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
		paths:          cfg.Paths,
	}
}

// TrackMetadata holds tag values read from an audio file.
type TrackMetadata struct {
	Title  string
	Album  string
	Artist string
}

// LinkHandler decides the fate of a file whose channel has no linked artist.
type LinkHandler func(channelName string, title string) (StagingResult, error)

func (s *StagingService) ProcessStaging(ctx context.Context, onUnknown LinkHandler) error {
	stagingDir := s.paths.StagingDir()

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

// stagingImportPlan is the resolved outcome of the import decision phase:
// everything needed to register and relocate one staged file.
type stagingImportPlan struct {
	params    TrackCreateParams
	sourceAbs string
	destAbs   string
}

// importDecision is the chosen artist/folder target for one staged file.
type importDecision struct {
	artistID ArtistID
	folder   FolderID
}

// resolveImportDecision maps staged-file metadata to an artist/folder target,
// consulting onUnknown when the channel has no linked artist. Returns a nil
// decision when the handler chose to skip the file.
func (s *StagingService) resolveImportDecision(ctx context.Context, channelName ChannelName, m TrackMetadata, onUnknown LinkHandler) (*importDecision, error) {
	artistID, ok, err := s.linker.ResolveArtist(ctx, channelName)
	if err != nil {
		return nil, err
	}

	var trackFolder FolderID

	if !ok {
		res, err := onUnknown(string(channelName), strings.TrimSpace(m.Title))
		if err != nil {
			return nil, err
		}

		switch res.Action {
		case ActionSkip:
			return nil, nil
		case ActionAbort:
			return nil, ErrProcessAborted
		case ActionImport:
			artistID = res.Artist
			trackFolder = res.Folder
		}

		if artistID != "" && res.LinkArtist {
			if err := s.linker.Link(ctx, artistID, channelName); err != nil {
				return nil, fmt.Errorf("failed linking unknown channel to artist: %w", err)
			}
		}
	}

	var decision importDecision
	decision.artistID = artistID

	switch {
	case trackFolder != "":
		decision.folder = trackFolder
	case artistID != "":
		folder, err := s.artistReader.FolderID(ctx, artistID)
		if err != nil {
			return nil, fmt.Errorf("failed tracking artist folder target: %w", err)
		}
		decision.folder = folder
	default:
		decision.folder = FolderID(RootID)
	}

	return &decision, nil
}

// sourceIDPattern matches YouTube video IDs — the only staging filename
// convention yci writes (<id>.opus). Anything else fails loudly instead of
// being imported with the whole filename as its source ID.
var sourceIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{11}$`)

// planImport performs the decision phase for one staged file and computes
// its destination paths.
func (s *StagingService) planImport(ctx context.Context, fileName string, onUnknown LinkHandler) (*stagingImportPlan, error) {
	oldPathNative := filepath.Join(s.paths.StagingDir(), fileName)

	m, err := s.metaReader.Read(oldPathNative)
	if err != nil {
		return nil, fmt.Errorf("extract track metadata: %w", err)
	}

	channelName := ChannelName(strings.TrimSpace(m.Artist))
	title := strings.TrimSpace(m.Title)
	sourceID := strings.TrimSuffix(fileName, ".opus")

	if !sourceIDPattern.MatchString(sourceID) {
		return nil, fmt.Errorf("file name is not a staged download (expected <video-id>.opus); move or rename it out of staging")
	}

	decision, err := s.resolveImportDecision(ctx, channelName, m, onUnknown)
	if err != nil {
		return nil, err
	}
	if decision == nil {
		return nil, nil // skipped by handler
	}

	folderPath, err := s.folderResolver.ResolvePath(ctx, decision.folder)
	if err != nil {
		return nil, fmt.Errorf("failed resolving destination path: %w", err)
	}

	safeTitle := fs.SanitizeFilename(title)
	newFilename := safeTitle + ".opus"

	var relPath string
	if folderPath == "" {
		relPath = newFilename
	} else {
		relPath = path.Join(folderPath, newFilename)
	}

	absPathNative := filepath.Join(s.paths.LibraryDir(), filepath.FromSlash(relPath))

	return &stagingImportPlan{
		params: TrackCreateParams{
			ID:           TrackID(uuid.New().String()),
			Title:        title,
			Album:        m.Album,
			ArtistID:     decision.artistID,
			FolderID:     decision.folder,
			RelativePath: TrackPath(relPath),
			SourceID:     sourceID,
		},
		sourceAbs: oldPathNative,
		destAbs:   absPathNative,
	}, nil
}

func (s *StagingService) processSingleStagingFile(ctx context.Context, fileName string, onUnknown LinkHandler) error {
	plan, err := s.planImport(ctx, fileName, onUnknown)
	if err != nil {
		return err
	}
	if plan == nil {
		return nil // skipped
	}

	txErr := s.transactor.WithinTransaction(ctx, func(txCtx context.Context) error {
		if _, err := s.trackCreator.Create(txCtx, plan.params); err != nil {
			return err
		}

		if err := s.diskMover.MoveFile(txCtx, plan.sourceAbs, plan.destAbs); err != nil {
			return fmt.Errorf("move staging file to library: %w", err)
		}

		return nil
	})

	return txErr
}
