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
	folderService := library.NewFolderService(folderRepo, trackRepo, diskMover, transactor, rootID, paths)

	trackService := library.NewTrackService(
		trackRepo,
		artistRepo,
		folderService,
		channelService,
		metaReader,
		diskMover,
		transactor,
		paths,
	)

	artistService := library.NewArtistService(artistRepo, folderRepo, trackService, transactor, rootID)

	return &Container{
		DB:             dbConn,
		FolderService:  folderService,
		ArtistService:  artistService,
		TrackService:   trackService,
		ChannelService: channelService,
		Paths:          paths,
	}, nil
}
