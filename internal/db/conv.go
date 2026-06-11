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
