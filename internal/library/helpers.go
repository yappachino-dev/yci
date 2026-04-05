package library

import (
	"database/sql"
	"fmt"
)

type scannable interface {
	Scan(dest ...any) error
}

func fromNull(ns sql.NullString, defaultValue string) string {
	if ns.Valid {
		return ns.String
	}
	return defaultValue
}

func toNull(s string) any {
	if s == "" || s == RootID {
		return nil
	}
	return s
}

func (l *Library) wrapErr(err error, entity, id string) error {
	if err == nil {
		return nil
	}
	if err == sql.ErrNoRows {
		return fmt.Errorf("%s %q not found", entity, id)
	}
	return fmt.Errorf("database error (%s %q): %w", entity, id, err)
}
