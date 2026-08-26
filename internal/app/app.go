package app

import (
	"database/sql"

	"github.com/yappachino-dev/yci/internal/audit"
	"github.com/yappachino-dev/yci/internal/config"
	"github.com/yappachino-dev/yci/internal/db"
	"github.com/yappachino-dev/yci/internal/download"
	"github.com/yappachino-dev/yci/internal/fs"
	"github.com/yappachino-dev/yci/internal/library"
	"github.com/yappachino-dev/yci/internal/store"
)

type Container struct {
	DB              *sql.DB
	Config          config.Config
	Paths           fs.AppPaths
	FolderService   *library.FolderService
	ArtistService   *library.ArtistService
	TrackService    *library.TrackService
	ChannelService  *library.ChannelService
	StagingService  *library.StagingService
	PlaylistService *library.PlaylistService
	DownloadService *download.Service
	AuditService    *audit.Service
	FileActions     *db.FileActionExecutor
}

// NewContainer wires all services from a single resolved config. The
// database connection must already be open and migrated.
func NewContainer(dbConn *sql.DB, cfg config.Config) (*Container, error) {
	paths := fs.NewAppPaths(cfg.ConfigDir(), cfg.LibraryDir, cfg.StagingDir)

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
	metaReader := library.NewTagMetadataReader()

	channelService := library.NewChannelService(channelRepo)

	folderService := library.NewFolderService(library.FolderServiceConfig{
		FolderRepo:  folderRepo,
		TrackWriter: trackRepo,
		DiskMover:   journaledMover,
		Transactor:  transactor,
		RootFolder:  rootID,
		Paths:       paths,
	})

	trackService := library.NewTrackService(library.TrackServiceConfig{
		TrackRepo:      trackRepo,
		FolderResolver: folderService,
		DiskMover:      journaledMover,
		Transactor:     transactor,
		Paths:          paths,
	})

	artistService := library.NewArtistService(library.ArtistServiceConfig{
		ArtistRepo:    artistRepo,
		FolderChecker: folderRepo,
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
	})

	playlistService := library.NewPlaylistService(library.PlaylistServiceConfig{
		Repo:   playlistRepo,
		Tracks: trackService,
		Paths:  paths,
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
		AuditService:    auditService,
		FileActions:     fileActionExecutor,
		Config:          cfg,
		Paths:           paths,
	}, nil
}
