package library

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/yappachino-dev/yci/internal/fs"
	"github.com/yappachino-dev/yci/internal/videoid"

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
	Link(ctx context.Context, artistID ArtistID, name ChannelName, id ChannelID) error
	ResolveArtist(ctx context.Context, name ChannelName) (ArtistID, bool, error)
	All(ctx context.Context) ([]Channel, error)

	// ResolveArtistByID and the rest carry the immutable channel identity,
	// recorded at download time. They are the path a rename does not break.
	ResolveArtistByID(ctx context.Context, id ChannelID) (ArtistID, bool, error)
	RefreshChannelName(ctx context.Context, id ChannelID, artistID ArtistID, name ChannelName) error
	AttachChannelID(ctx context.Context, artistID ArtistID, name ChannelName, id ChannelID) error
	VideoChannel(ctx context.Context, videoID VideoID) (ChannelID, bool, error)
	ForgetVideoChannel(ctx context.Context, videoID VideoID) error
}

// MetadataReader reads tag metadata from an audio file.
//
// It takes a context because extraction is no longer purely in-process: the
// production reader also probes the stream, which forks a decoder and must be
// cancellable.
type MetadataReader interface {
	Read(ctx context.Context, path string) (TrackMetadata, error)
}

// supportedImportExtensions are the containers the import path accepts.
//
// Measured against real encoded files, dhowden/tag reads tags from all of
// these. WAV is deliberately absent: it is a raw PCM container and dhowden
// returns "no tags found" for it, so importing one would fail with no title
// rather than importing it. Supporting WAV means writing a LIST/INFO reader;
// until then, a staged .wav is reported instead of being silently skipped.
var supportedImportExtensions = []string{".opus", ".ogg", ".flac", ".mp3", ".m4a"}

// SupportedImportExtensions exposes the accepted set for error messages and
// tests.
func SupportedImportExtensions() []string {
	out := make([]string, len(supportedImportExtensions))
	copy(out, supportedImportExtensions)
	return out
}

func isSupportedImportExtension(ext string) bool {
	for _, candidate := range supportedImportExtensions {
		if ext == candidate {
			return true
		}
	}
	return false
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
	dirReader      fs.DirReader
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

	// DirReader lists the staging directory. Optional: nil uses
	// fs.NewOSDiskReader(), so wiring code need not supply it.
	DirReader fs.DirReader
}

func NewStagingService(cfg StagingServiceConfig) *StagingService {
	dirReader := cfg.DirReader
	if dirReader == nil {
		dirReader = fs.NewOSDiskReader()
	}

	return &StagingService{
		trackCreator:   cfg.TrackCreator,
		artistReader:   cfg.ArtistReader,
		folderResolver: cfg.FolderResolver,
		linker:         cfg.Linker,
		metaReader:     cfg.MetaReader,
		diskMover:      cfg.DiskMover,
		transactor:     cfg.Transactor,
		paths:          cfg.Paths,
		dirReader:      dirReader,
	}
}

// TrackMetadata holds tag values read from an audio file.
type TrackMetadata struct {
	Title  string
	Album  string
	Artist string

	// Media holds derived stream properties. Zero when unavailable.
	Media TrackMediaInfo
	// MediaUnavailable records that no prober was installed, so an absent
	// Media is explained rather than silently "a file with no length".
	MediaUnavailable bool
	// MediaErr records a probe failure against an otherwise readable file.
	MediaErr error
}

// LinkHandler decides the fate of a file whose channel has no linked artist.
type LinkHandler func(channelName string, title string) (StagingResult, error)

// StagingStatus is what became of one staged file.
type StagingStatus string

const (
	// StagingImported: registered and moved into the library.
	StagingImported StagingStatus = "imported"
	// StagingBlocked: the channel has no linked artist and the handler
	// declined, so the file is still waiting in staging.
	StagingBlocked StagingStatus = "blocked"
	// StagingSkipped: not a format this importer handles.
	StagingSkipped StagingStatus = "skipped"
	// StagingFailed: this one file errored. The rest still processed.
	StagingFailed StagingStatus = "failed"
)

// StagingOutcome is the per-file result of an import pass.
//
// A summary cannot say which files still need attention, which is the only
// thing a caller acting over HTTP has to act on: a client resolves the named
// channels and retries, and a blocked file is the one it must not forget.
type StagingOutcome struct {
	File   string
	Status StagingStatus
	Reason string

	// ChannelName and ChannelID are set when Status is StagingBlocked. The
	// ID is the identity recorded at download time, so a client linking the
	// artist afterwards does not have to guess.
	ChannelName ChannelName
	ChannelID   ChannelID
}

// ProcessStaging imports everything in the staging directory.
//
// It returns one outcome per file it looked at. The error return covers only
// the pass itself: an unreadable staging directory, or a handler that aborted
// the loop. A single bad file is reported in the outcomes and does not stop
// the rest, because the alternative is one corrupt download blocking every
// good one.
func (s *StagingService) ProcessStaging(ctx context.Context, onUnknown LinkHandler) ([]StagingOutcome, error) {
	stagingDir := s.paths.StagingDir()

	files, err := s.dirReader.ReadDir(stagingDir)
	if err != nil {
		return nil, fmt.Errorf("read staging directory: %w", err)
	}

	outcomes := make([]StagingOutcome, 0, len(files))

	// fs.DirReader returns file names only, already without subdirectories.
	for _, file := range files {
		ext := strings.ToLower(filepath.Ext(file))
		if !isSupportedImportExtension(ext) {
			// Recorded rather than dropped: a misconfigured download command
			// would otherwise look like "nothing to import".
			if ext == "" {
				continue
			}

			outcomes = append(outcomes, StagingOutcome{
				File:   file,
				Status: StagingSkipped,
				Reason: fmt.Sprintf("unsupported format %q (supported: %s)",
					ext, strings.Join(supportedImportExtensions, ", ")),
			})
			continue
		}

		outcome, err := s.processSingleStagingFile(ctx, file, onUnknown)
		if err != nil {
			if errors.Is(err, ErrProcessAborted) {
				return outcomes, err
			}

			outcomes = append(outcomes, StagingOutcome{
				File: file, Status: StagingFailed, Reason: err.Error(),
			})
			continue
		}

		outcomes = append(outcomes, outcome)
	}

	return outcomes, nil
}

// stagingImportPlan is the resolved outcome of the import decision phase:
// everything needed to register and relocate one staged file.
type stagingImportPlan struct {
	params    TrackCreateParams
	sourceAbs string
	destAbs   string

	// blocked is set instead of a destination when the file's channel has no
	// linked artist and the handler declined. The file stays in staging.
	blocked *blockedChannel
}

// importDecision is the chosen artist/folder target for one staged file.
type importDecision struct {
	artistID ArtistID
	folder   FolderID

	// blocked records a handler that declined, and which channel it was
	// waiting on. Without it a caller cannot tell "no target" from "this
	// channel needs linking", and the second is actionable.
	blocked *blockedChannel
}

// blockedChannel names the unresolved channel behind a skipped file.
type blockedChannel struct {
	name ChannelName
	id   ChannelID
}

// resolveImportDecision maps staged-file metadata to an artist/folder target,
// consulting onUnknown when the channel has no linked artist. Returns a nil
// decision when the handler chose to skip the file.
func (s *StagingService) resolveImportDecision(ctx context.Context, videoID VideoID, channelName ChannelName, m TrackMetadata, onUnknown LinkHandler) (*importDecision, error) {
	match, err := s.ResolveChannel(ctx, videoID, channelName)
	if err != nil {
		return nil, err
	}

	artistID := match.ArtistID
	var trackFolder FolderID

	if !match.Found {
		res, err := onUnknown(string(channelName), strings.TrimSpace(m.Title))
		if err != nil {
			return nil, err
		}

		switch res.Action {
		case ActionSkip:
			return &importDecision{blocked: &blockedChannel{
				name: channelName, id: match.ChannelID,
			}}, nil
		case ActionAbort:
			return nil, ErrProcessAborted
		case ActionImport:
			artistID = res.Artist
			trackFolder = res.Folder
		}

		if artistID != "" && res.LinkArtist {
			// The ID comes from the download record, so this link is born
			// complete rather than needing a repair pass to be trusted.
			if err := s.linker.Link(ctx, artistID, channelName, match.ChannelID); err != nil {
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

// ChannelMatch is what import resolved for one staged file.
type ChannelMatch struct {
	// ArtistID is the linked artist, meaningful only when Found.
	ArtistID ArtistID
	// ChannelID is the identity recorded at download time. It is set even
	// when Found is false, so a link the handler creates can be born with it.
	ChannelID ChannelID
	Found     bool
}

// ResolveChannel maps a staged file to an artist, preferring the immutable
// channel ID recorded at download time over the artist tag written into the
// file.
//
// The tag is a display name, and a performer can rename their channel at any
// time. When that happened, the name lookup missed, import stopped resolving
// an artist, and the user was asked again for every file. With the ID the
// rename self-heals: the link still matches and only the stored label needs
// refreshing.
//
// videoID may be empty, in which case only the name path is available.
func (s *StagingService) ResolveChannel(ctx context.Context, videoID VideoID, name ChannelName) (ChannelMatch, error) {
	var match ChannelMatch

	// The recorded identity is returned whether or not a link exists yet:
	// when the handler creates one, it is what makes the new link complete.
	if videoID != "" {
		if id, found, err := s.linker.VideoChannel(ctx, videoID); err == nil && found {
			match.ChannelID = id

			if id != "" {
				artistID, resolved, err := s.linker.ResolveArtistByID(ctx, id)
				if err != nil {
					return ChannelMatch{}, err
				}
				if resolved {
					s.refreshLabel(ctx, id, artistID, name)
					match.ArtistID = artistID
					match.Found = true
					return match, nil
				}
			}
		}
	}

	artistID, ok, err := s.linker.ResolveArtist(ctx, name)
	if err != nil {
		return ChannelMatch{}, err
	}
	if ok {
		s.promoteLabel(ctx, artistID, name, videoID)
		match.ArtistID = artistID
		match.Found = true
	}

	return match, nil
}

// refreshLabel updates a stored channel name that has drifted. Best effort:
// the artist is already resolved, so a failure here costs tidiness, not
// correctness.
func (s *StagingService) refreshLabel(ctx context.Context, id ChannelID, artistID ArtistID, name ChannelName) {
	if name == "" {
		return
	}
	_ = s.linker.RefreshChannelName(ctx, id, artistID, name)
}

// promoteLabel attaches the channel ID to a link that only has a name, so the
// next download resolves by identity instead of by string.
func (s *StagingService) promoteLabel(ctx context.Context, artistID ArtistID, name ChannelName, videoID VideoID) {
	if artistID == "" || videoID == "" {
		return
	}

	id, found, err := s.linker.VideoChannel(ctx, videoID)
	if err != nil || !found || id == "" {
		return
	}

	_ = s.linker.AttachChannelID(ctx, artistID, name, id)
}

// A staged file must be named <video-id>.<ext> — the only convention yci
// writes. Anything else fails loudly instead of being imported with the
// whole filename as its source ID. The rule lives in internal/videoid so it
// cannot drift from the one the download queue validates URLs against.

// planImport performs the decision phase for one staged file and computes
// its destination paths.
func (s *StagingService) planImport(ctx context.Context, fileName string, onUnknown LinkHandler) (*stagingImportPlan, error) {
	oldPathNative := filepath.Join(s.paths.StagingDir(), fileName)

	m, err := s.metaReader.Read(ctx, oldPathNative)
	if err != nil {
		return nil, fmt.Errorf("extract track metadata: %w", err)
	}

	channelName := ChannelName(strings.TrimSpace(m.Artist))
	title := strings.TrimSpace(m.Title)
	// The extension is stripped generically rather than by naming one format:
	// a staged download is <video-id>.<ext>, and the container it arrived in
	// is preserved through to the library.
	ext := strings.ToLower(filepath.Ext(fileName))
	sourceID := strings.TrimSuffix(fileName, ext)

	if !videoid.IsValid(sourceID) {
		return nil, fmt.Errorf("file name is not a staged download (expected <video-id>%s); move or rename it out of staging", ext)
	}

	decision, err := s.resolveImportDecision(ctx, VideoID(sourceID), channelName, m, onUnknown)
	if err != nil {
		return nil, err
	}
	if decision.blocked != nil {
		return &stagingImportPlan{blocked: decision.blocked}, nil
	}

	folderPath, err := s.folderResolver.ResolvePath(ctx, decision.folder)
	if err != nil {
		return nil, fmt.Errorf("failed resolving destination path: %w", err)
	}

	interpreted := InterpretTitle(title)

	relPath, absPathNative, err := s.resolveDestination(folderPath, interpreted.Title, ext)
	if err != nil {
		return nil, err
	}

	return &stagingImportPlan{
		params: TrackCreateParams{
			ID:           TrackID(uuid.New().String()),
			Title:        interpreted.Title,
			Album:        m.Album,
			ArtistID:     decision.artistID,
			RelativePath: TrackPath(relPath),
			SourceID:     sourceID,
			Source:       SourceYouTube,
			RawTitle:     title,
			Performer:    interpreted.Performer,
			Version:      interpreted.Version,
			DurationSec:  int64(m.Media.Duration / time.Second),
			Media:        m.Media,
		},
		sourceAbs: oldPathNative,
		destAbs:   absPathNative,
	}, nil
}

func (s *StagingService) processSingleStagingFile(ctx context.Context, fileName string, onUnknown LinkHandler) (StagingOutcome, error) {
	plan, err := s.planImport(ctx, fileName, onUnknown)
	if err != nil {
		return StagingOutcome{}, err
	}

	if plan.blocked != nil {
		return StagingOutcome{
			File:        fileName,
			Status:      StagingBlocked,
			Reason:      fmt.Sprintf("channel %q is not linked to an artist", plan.blocked.name),
			ChannelName: plan.blocked.name,
			ChannelID:   plan.blocked.id,
		}, nil
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

	if txErr != nil {
		return StagingOutcome{}, txErr
	}

	return StagingOutcome{File: fileName, Status: StagingImported}, nil
}

// resolveDestination computes the library-relative and absolute paths for an
// imported track. Cleaner titles collide more often than decorated ones
// (two uploads of the same song lose the same decoration), so an occupied
// destination gets a numeric suffix rather than overwriting the file that
// is already there.
func (s *StagingService) resolveDestination(folderPath, title, ext string) (string, string, error) {
	stem := fs.SanitizeFilename(title)
	if stem == "" {
		return "", "", fmt.Errorf("import track: title %q has no usable characters for a filename", title)
	}

	for attempt := 1; ; attempt++ {
		candidate := stem
		if attempt > 1 {
			candidate = fmt.Sprintf("%s (%d)", stem, attempt)
		}

		filename := candidate + ext

		var relPath string
		if folderPath == "" {
			relPath = filename
		} else {
			relPath = path.Join(folderPath, filename)
		}

		absPath := filepath.Join(s.paths.LibraryDir(), filepath.FromSlash(relPath))
		if _, err := os.Stat(absPath); errors.Is(err, os.ErrNotExist) {
			return relPath, absPath, nil
		} else if err != nil {
			return "", "", fmt.Errorf("check destination %q: %w", absPath, err)
		}
	}
}
