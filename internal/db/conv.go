package db

import "database/sql"

func FromNullString(s sql.NullString) string {
	if s.Valid {
		return s.String
	} else {
		return ""
	}
}

func ToNullString(s string) sql.NullString {
	if s == "" {
		return sql.NullString{Valid: false}
	} else {
		return sql.NullString{String: s, Valid: true}
	}
}

func FromNullInt64(n sql.NullInt64) int64 {
	if n.Valid {
		return n.Int64
	} else {
		return 0
	}
}

// ToNullInt64 stores 0 as 0 rather than NULL. Unlike strings there is no
// "absent" representation of a number, and a zero duration means "unknown",
// which a query can already select on.
func ToNullInt64(n int64) sql.NullInt64 {
	return sql.NullInt64{Int64: n, Valid: true}
}
