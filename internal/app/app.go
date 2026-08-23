package app

import (
	"database/sql"

	"github.com/yappachino-dev/yci/internal/audit"
	"github.com/yappachino-dev/yci/internal/db"
	"github.com/yappachino-dev/yci/internal/download"
	"github.com/yappachino-dev/yci/internal/fs"
	"github.com/yappachino-dev/yci/internal/library"
	"github.com/yappachino-dev/yci/internal/store"
)

type Container struct {
	DB              *sql.DB
	FolderService   *library.FolderService
	ArtistService   *library.ArtistService
	TrackService    *library.TrackService
	ChannelService  *library.ChannelService
	StagingService  *library.StagingService
	DownloadService *download.Service
	AuditService    *audit.Service
	FileActions     *db.FileActionExecutor
	Paths           fs.AppPaths
}

func NewContainer(dbConn *sql.DB, paths fs.AppPaths) (*Container, error) {
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

	diskReader := fs.NewOSDiskReader()
	metaReader := library.NewTagMetadataReader()

	channelService := library.NewChannelService(channelRepo)

	folderService := library.NewFolderService(library.FolderServiceConfig{
		FolderRepo:  folderRepo,
		TrackWriter: trackRepo,
		DiskMover:   journaledMover,
		Transactor:  transactor,
		RootFolder:  rootID,
		Config:      paths,
	})

	trackService := library.NewTrackService(library.TrackServiceConfig{
		TrackRepo:      trackRepo,
		FolderResolver: folderService,
		DiskMover:      journaledMover,
		Transactor:     transactor,
		Config:         paths,
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
		Config:         paths,
	})

	downloadService := download.NewService(download.ServiceConfig{
		QueueRepo:    queueRepo,
		TrackChecker: trackService,
		DirReader:    diskReader,
		Transactor:   transactor,
		Config:       paths,
	})

	auditService := audit.NewService(audit.Config{
		TrackProvider: trackService,
		Config:        paths,
	})

	return &Container{
		DB:              dbConn,
		FolderService:   folderService,
		ArtistService:   artistService,
		TrackService:    trackService,
		ChannelService:  channelService,
		StagingService:  stagingService,
		DownloadService: downloadService,
		AuditService:    auditService,
		FileActions:     fileActionExecutor,
		Paths:           paths,
	}, nil
}
