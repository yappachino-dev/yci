package db

import (
	"database/sql"
	"errors"

	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

func IsUniqueConstraintError(err error) bool {
	if sqliteErr, ok := errors.AsType[*sqlite.Error](err); ok {
		return sqliteErr.Code() == sqlite3.SQLITE_CONSTRAINT_UNIQUE ||
			sqliteErr.Code() == sqlite3.SQLITE_CONSTRAINT_PRIMARYKEY
	}

	return false
}

// MapSQLError translates driver errors into domain sentinels:
// sql.ErrNoRows becomes notFound, unique violations become conflict.
// Other errors pass through unchanged. A nil notFound or conflict disables
// that translation.
func MapSQLError(err error, notFound, conflict error) error {
	if err == nil {
		return nil
	}

	switch {
	case notFound != nil && errors.Is(err, sql.ErrNoRows):
		return notFound
	case conflict != nil && IsUniqueConstraintError(err):
		return conflict
	default:
		return err
	}
}
