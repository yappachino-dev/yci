package db

import (
	"database/sql"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"

	// The driver is imported here because this file is what calls
	// sql.Open("sqlite", ...). It happened to be registered transitively
	// via the error types in errors.go, which is a dependency that can
	// disappear without any visible cause.
	_ "modernc.org/sqlite"
)

// Pragma settings every connection must agree on.
//
// foreign_keys is the load-bearing one. yci's correctness depends on it:
// playlist membership cascades on delete, a playlist's seed track goes NULL
// rather than dangling, and folder deletes cascade. Setting it with a plain
// `db.Exec("PRAGMA foreign_keys = ON")` looks correct and is not, because
// SQLite scopes the pragma to the connection that ran it and database/sql
// hands out a pool. Every other connection opened afterwards silently has
// foreign_keys=0 and enforces nothing. The DSN form is applied by the driver
// to each connection as it is created, so there is no connection that can
// miss it.
//
// busy_timeout is a second per-connection setting with the same problem and
// the same fix. Without it a writer that finds the database locked fails
// immediately with SQLITE_BUSY instead of waiting.
//
// journal_mode is deliberately not set. WAL would remove reader/writer
// contention outright, but it is persistent, it adds -wal and -shm sidecar
// files, and `make sandbox` copies only data.db — so a WAL database would
// silently lose recent commits in every sandbox. That trade is real and is
// worth revisiting, but it is not a decision to make silently inside a
// connection string.
const (
	pragmaForeignKeys = "foreign_keys(1)"
	pragmaBusyTimeout = "busy_timeout(5000)"
)

// DSN builds the sqlite connection string for path.
//
// path may be a filesystem path or one of SQLite's in-memory forms
// (":memory:", "file::memory:"). For in-memory databases the pragmas still
// apply; a shared in-memory database is reached by every connection the pool
// opens, and tests rely on FK enforcement just as production does.
func DSN(path string) string {
	base := path

	if path != ":memory:" && !strings.HasPrefix(path, "file:") {
		// A bare path is a filename, and a filename containing "?" or "#"
		// would otherwise be read as a query string. Escaping it keeps
		// such a path a path.
		base = "file:" + (&url.URL{Path: filepath.ToSlash(path)}).EscapedPath()
	} else if !strings.HasPrefix(path, "file:") {
		// ":memory:" must stay bare: "file::memory:" is also in-memory but
		// does not behave identically for every driver path.
		base = path
	}

	// url.Values is used rather than a hand-written query string for two
	// reasons: it escapes what needs escaping, and the repeated _pragma
	// keys survive. The driver loops over q["_pragma"] and runs each value
	// as its own statement, which is the only way to set more than one
	// pragma — "pragma a(1),b(2)" is not valid SQLite.
	q := url.Values{}
	q.Add("_pragma", pragmaForeignKeys)
	q.Add("_pragma", pragmaBusyTimeout)

	sep := "?"
	if strings.Contains(base, "?") {
		sep = "&"
	}

	return base + sep + q.Encode()
}

// Open opens the sqlite database at path with the pragmas every connection
// needs. It is the single entry point for opening the database so production
// and tests cannot disagree about connection-level settings.
func Open(path string) (*sql.DB, error) {
	conn, err := sql.Open("sqlite", DSN(path))
	if err != nil {
		return nil, fmt.Errorf("open sqlite database %q: %w", path, err)
	}

	return conn, nil
}

// ForeignKeysEnabled reports whether the pooled connection currently serving
// queries enforces foreign keys. It exists so a test can assert the property
// that matters rather than trusting that a PRAGMA was issued somewhere.
func ForeignKeysEnabled(db *sql.DB) (bool, error) {
	var enabled int
	if err := db.QueryRow("PRAGMA foreign_keys;").Scan(&enabled); err != nil {
		return false, fmt.Errorf("read foreign_keys pragma: %w", err)
	}

	return enabled == 1, nil
}
