package app

import (
	"context"
	"database/sql"

	"playlist-manager/internal/db"
	"playlist-manager/internal/downloader"
	"playlist-manager/internal/fs"
	"playlist-manager/internal/library"
	"playlist-manager/internal/store"
)

type Container struct {
	DB              *sql.DB
	FolderService   *library.FolderService
	ArtistService   *library.ArtistService
	TrackService    *library.TrackService
	ChannelService  *library.ChannelService
	StagingService  *library.StagingService
	DownloadService *downloader.Service
	Paths           fs.AppPaths
}

func NewContainer(ctx context.Context, dbConn *sql.DB, paths fs.AppPaths) (*Container, error) {
	queries := store.New(dbConn)

	transactor := db.NewSQLiteTransactor(dbConn)
	rootID := library.FolderID(library.RootID)

	folderRepo := library.NewSQLiteFolderRepository(queries, rootID)
	artistRepo := library.NewSQLiteArtistRepository(queries, rootID)
	trackRepo := library.NewSQLiteTrackRepository(queries)
	channelRepo := library.NewSQLiteChannelRepository(queries)
	queueRepo := downloader.NewSQLiteQueueRepo(queries)

	diskMover := fs.NewOSDiskMover()
	diskReader := fs.NewOSDiskReader()
	metaReader := library.NewTagMetadataReader()

	channelService := library.NewChannelService(channelRepo)

	folderService := library.NewFolderService(library.FolderServiceConfig{
		FolderRepo:  folderRepo,
		TrackWriter: trackRepo,
		DiskMover:   diskMover,
		Transactor:  transactor,
		RootFolder:  rootID,
		Config:      paths,
	})

	trackService := library.NewTrackService(library.TrackServiceConfig{
		TrackRepo:      trackRepo,
		FolderResolver: folderService,
		DiskMover:      diskMover,
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
		DiskMover:      diskMover,
		Transactor:     transactor,
		Config:         paths,
	})

	downloadService := downloader.NewService(downloader.ServiceConfig{
		QueueRepo:    queueRepo,
		TrackChecker: trackService,
		DirReader:    diskReader,
		Transactor:   transactor,
		Config:       paths,
	})

	return &Container{
		DB:              dbConn,
		FolderService:   folderService,
		ArtistService:   artistService,
		TrackService:    trackService,
		ChannelService:  channelService,
		StagingService:  stagingService,
		DownloadService: downloadService,
		Paths:           paths,
	}, nil
}
