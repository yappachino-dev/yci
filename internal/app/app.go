package app

import (
	"context"
	"database/sql"
	"fmt"

	"playlist-manager/internal/db"
	"playlist-manager/internal/library"
	"playlist-manager/internal/store"
)

type Container struct {
	DB             *sql.DB
	FolderService  *library.FolderService
	ArtistService  *library.ArtistService
	TrackService   *library.TrackService
	ChannelService *library.ChannelService
	StagingService *library.StagingService
	Paths          library.AppPaths
}

func NewContainer(ctx context.Context, dbConn *sql.DB, paths library.AppPaths) (*Container, error) {
	queries, err := store.Prepare(ctx, dbConn)
	if err != nil {
		return nil, fmt.Errorf("prepare database statements: %w", err)
	}

	transactor := db.NewSQLiteTransactor(dbConn)
	rootID := library.FolderID(library.RootID)

	folderRepo := library.NewSQLiteFolderRepository(queries, rootID)
	artistRepo := library.NewSQLiteArtistRepository(queries, rootID)
	trackRepo := library.NewSQLiteTrackRepository(queries)
	channelRepo := library.NewSQLiteChannelRepository(queries)

	diskMover := library.NewOSDiskMover()
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

	return &Container{
		DB:             dbConn,
		FolderService:  folderService,
		ArtistService:  artistService,
		TrackService:   trackService,
		ChannelService: channelService,
		StagingService: stagingService,
		Paths:          paths,
	}, nil
}
