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
	DB            *sql.DB
	FolderService *library.FolderService
	ArtistService *library.ArtistService
	TrackService  *library.TrackService
	ChannelLinker *library.SQLiteChannelLinker
	Paths         library.AppPaths
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
	linker := library.NewSQLiteChannelLinker(queries, rootID)

	diskMover := library.NewOSDiskMover()
	metaReader := library.NewTagMetadataReader()

	folderService := library.NewFolderService(folderRepo, trackRepo, diskMover, transactor, rootID, paths)

	trackService := library.NewTrackService(
		trackRepo,
		folderService,
		linker,
		metaReader,
		diskMover,
		transactor,
		paths,
	)

	artistService := library.NewArtistService(artistRepo, folderRepo, trackService, transactor, rootID)

	return &Container{
		DB:            dbConn,
		FolderService: folderService,
		ArtistService: artistService,
		TrackService:  trackService,
		ChannelLinker: linker,
		Paths:         paths,
	}, nil
}
