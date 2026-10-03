package fs

import (
	"os"

	"golang.org/x/text/unicode/norm"
)

// AlternateForm returns p in the other Unicode normalization form.
//
// NFC and NFD encode visually identical names as different bytes, and ext4
// compares names byte for byte, so the two forms are different files as far
// as the kernel is concerned. A path that differs from the one on disk only
// in normalization cannot be opened, stat-ed or detected as a collision.
func AlternateForm(p string) string {
	if norm.NFC.IsNormalString(p) {
		return norm.NFD.String(p)
	}

	return norm.NFC.String(p)
}

// ResolveExisting returns which of p and its alternate normalization form
// exists on disk, and nil only when neither does.
//
// The returned error comes from the last attempt, so a caller that needs to
// distinguish "missing" from "unreadable" still can.
func ResolveExisting(p string) (string, error) {
	_, err := os.Stat(p)
	if err == nil {
		return p, nil
	}

	alt := AlternateForm(p)
	if alt == p {
		return p, err
	}

	_, altErr := os.Stat(alt)
	if altErr == nil {
		return alt, nil
	}

	// Prefer the original path's error: it is the one the caller asked
	// about, and it is the likelier description of what went wrong.
	if os.IsNotExist(err) {
		return p, err
	}

	return p, altErr
}

// Form names a path's Unicode normalization form, for reporting a difference
// that is real in the bytes but invisible on screen.
func Form(p string) string {
	switch {
	case norm.NFC.IsNormalString(p):
		return "NFC"
	case norm.NFD.IsNormalString(p):
		return "NFD"
	default:
		return "unnormalized"
	}
}
