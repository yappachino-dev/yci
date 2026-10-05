package app

import (
	"context"
	"database/sql"

	"github.com/yappachino-dev/yci/internal/audit"
	"github.com/yappachino-dev/yci/internal/config"
	"github.com/yappachino-dev/yci/internal/db"
	"github.com/yappachino-dev/yci/internal/download"
	"github.com/yappachino-dev/yci/internal/fs"
	"github.com/yappachino-dev/yci/internal/library"
	"github.com/yappachino-dev/yci/internal/probe"
	"github.com/yappachino-dev/yci/internal/store"
)

type Container struct {
	DB     *sql.DB
	Config config.Config
	// Prober reads media properties. Exposed so maintenance passes can be
	// driven with the same prober the import path uses, and so tests can
	// substitute one.
	Prober          probe.Prober
	Paths           fs.AppPaths
	FolderService   *library.FolderService
	ArtistService   *library.ArtistService
	TrackService    *library.TrackService
	ChannelService  *library.ChannelService
	StagingService  *library.StagingService
	PlaylistService *library.PlaylistService
	DownloadService *download.Service
	SourceService   *library.SourceService
	TrackJournal    *library.TrackJournalService
	AuditService    *audit.Service
	FileActions     *db.FileActionExecutor
}

// Close releases the container's database handle.
//
// It exists because initApplication is a function rather than a
// constructor someone can defer on, and because a test that boots the real
// application stack would otherwise leak a pooled connection per case. The
// CLI does not need to call it — the process is about to exit — but every
// path that opens a connection and might not exit should.
func (c *Container) Close() error {
	if c == nil || c.DB == nil {
		return nil
	}

	return c.DB.Close()
}

// NewContainer wires all services from a single resolved config. The
// database connection must already be open and migrated.
func NewContainer(dbConn *sql.DB, cfg config.Config) (*Container, error) {
	paths := fs.NewAppPaths(cfg.ConfigDir(), cfg.LibraryDir, cfg.StagingDir)
	if err := fs.EnsureDirs(paths); err != nil {
		return nil, err
	}

	queries := store.New(dbConn)

	transactor := db.NewSQLiteTransactor(dbConn)
	rootID := library.FolderID(library.RootID)

	fileActionRepo := db.NewFileActionRepository(queries)
	diskMover := fs.NewOSDiskMover()
	fileActionExecutor := db.NewFileActionExecutor(fileActionRepo, diskMover)
	transactor.SetPostCommit(fileActionExecutor.ExecutePending)
	journaledMover := db.NewJournaledMover(fileActionRepo, diskMover)

	folderRepo := library.NewSQLiteFolderRepository(queries, rootID)
	artistRepo := library.NewSQLiteArtistRepository(queries, rootID)
	trackRepo := library.NewSQLiteTrackRepository(queries)
	channelRepo := library.NewSQLiteChannelRepository(queries)
	queueRepo := download.NewSQLiteQueueRepo(queries)
	playlistRepo := library.NewSQLitePlaylistRepository(queries)

	diskReader := fs.NewOSDiskReader()
	prober := probe.NewFFprobe()
	metaReader := library.NewFileMetadataReader(prober)

	channelResolver := download.NewVideoChannelResolver(cfg.CookiesFile)

	folderService := library.NewFolderService(library.FolderServiceConfig{
		FolderRepo:  folderRepo,
		TrackWriter: trackRepo,
		DiskMover:   journaledMover,
		Transactor:  transactor,
		RootFolder:  rootID,
		Paths:       paths,
	})

	trackJournal := library.NewTrackJournalService(library.NewSQLiteTrackJournal(queries))
	sourceService := library.NewSourceService(library.NewSQLiteSourceRepository(queries))

	trackService := library.NewTrackService(library.TrackServiceConfig{
		TrackRepo:      trackRepo,
		FolderResolver: folderService,
		DiskMover:      journaledMover,
		Transactor:     transactor,
		Paths:          paths,
		Journal:        trackJournal,
	})

	// Built after trackService because the channel identity backfill needs
	// one of the artist's videos to ask YouTube about.
	channelService := library.NewChannelService(channelRepo,
		library.WithBackfill(trackService, func(ctx context.Context, videoID string) (string, string, error) {
			id, name, err := channelResolver.ResolveVideoChannel(ctx, videoID)
			return string(id), string(name), err
		}))

	artistService := library.NewArtistService(library.ArtistServiceConfig{
		ArtistRepo:    artistRepo,
		FolderChecker: folderRepo,
		FolderCreator: folderService,
		TrackMover:    trackService,
		Transactor:    transactor,
		RootFolder:    rootID,
	})

	stagingService := library.NewStagingService(library.StagingServiceConfig{
		TrackCreator:   trackService,
		ArtistReader:   artistRepo,
		FolderResolver: folderService,
		Linker:         channelService,
		MetaReader:     metaReader,
		DiskMover:      journaledMover,
		Transactor:     transactor,
		Paths:          paths,
	})

	downloadService := download.NewService(download.ServiceConfig{
		QueueRepo:    queueRepo,
		TrackChecker: trackService,
		DirReader:    diskReader,
		Transactor:   transactor,
		StagingDirs:  paths,
		// Records which channel each downloaded video came from, so import
		// joins on the immutable ID rather than on the artist tag.
		Channels: channelService,
	})

	playlistService := library.NewPlaylistService(library.PlaylistServiceConfig{
		Repo:       playlistRepo,
		Tracks:     trackService,
		Paths:      paths,
		Transactor: transactor,
	})

	auditService := audit.NewService(trackService, paths)

	return &Container{
		DB:              dbConn,
		FolderService:   folderService,
		ArtistService:   artistService,
		TrackService:    trackService,
		ChannelService:  channelService,
		StagingService:  stagingService,
		PlaylistService: playlistService,
		DownloadService: downloadService,
		SourceService:   sourceService,
		TrackJournal:    trackJournal,
		AuditService:    auditService,
		FileActions:     fileActionExecutor,
		Config:          cfg,
		Prober:          prober,
		Paths:           paths,
	}, nil
}
