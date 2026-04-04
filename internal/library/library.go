// Package library handles the music distribution
package library

import (
	"database/sql"
	_ "embed"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

const RootID string = "root"

type Library struct {
	Paths AppPaths

	db *sql.DB
}

func NewLibrary() (*Library, error) {
	paths, err := GetPaths()
	if err != nil {
		return nil, err
	}

	if err := os.MkdirAll(paths.Config, 0o755); err != nil {
		return nil, err
	}

	db, err := sql.Open("sqlite", filepath.Join(paths.Config, "data.db"))
	if err != nil {
		return nil, err
	}

	return &Library{
		Paths: paths,

		db: db,
	}, nil
}

//go:embed init.sql
var initSQL string

func (l *Library) Init() error {
	if _, err := l.db.Exec("PRAGMA foreign_keys = ON;"); err != nil {
		return err
	}

	if _, err := l.db.Exec(initSQL); err != nil {
		return err
	}

	return nil
}

func (l *Library) Close() error {
	return l.db.Close()
}

func (l *Library) TreePrint() {
	l.treePrint()
}
