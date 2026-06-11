package app

import (
	"database/sql"

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

func NewContainer(dbConn *sql.DB, paths library.AppPaths) *Container {
	queries := store.New(dbConn)
	transactor := db.NewSQLiteTransactor(dbConn)
	rootID := library.FolderID(library.RootID)

	folderRepo := library.NewSQLiteFolderRepository(queries, rootID)
	artistRepo := library.NewSQLiteArtistRepository(queries, rootID)
	trackRepo := library.NewSQLiteTrackRepository(queries)
	linker := library.NewSQLiteChannelLinker(queries, rootID)

	diskMover := library.NewOSDiskMover()
	metaReader := library.NewTagMetadataReader()

	folderService := library.NewFolderService(folderRepo, trackRepo, diskMover, transactor, rootID)

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
	}
}
