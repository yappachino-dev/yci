package db

import (
	"database/sql"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"

	// Imported here rather than relied on transitively: this is the file
	// that opens the database, so this is where the driver belongs.
	_ "modernc.org/sqlite"
)

// Pragma settings every connection must agree on.
//
// These belong in the DSN and nowhere else: SQLite scopes a pragma to the
// connection that ran it, and database/sql hands out a pool, so a one-time
// db.Exec leaves every later connection unenforced. See style-guide 5.
const (
	pragmaForeignKeys = "foreign_keys(1)"
	pragmaBusyTimeout = "busy_timeout(5000)"
	pragmaJournalMode = "journal_mode(WAL)"
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

	// url.Values, not a hand-built string: the driver loops over the
	// repeated _pragma key and runs each as its own statement, which is
	// the only way to set several (SQLite has no "pragma a(1),b(2)").
	q := url.Values{}
	q.Add("_pragma", pragmaForeignKeys)
	q.Add("_pragma", pragmaBusyTimeout)
	q.Add("_pragma", pragmaJournalMode)

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
