package library

import (
	"context"
	"strings"

	"github.com/yappachino-dev/yci/internal/fs"
)

// Transactor runs fn inside a database transaction, joining any transaction
// already active on ctx.
type Transactor interface {
	WithinTransaction(ctx context.Context, fn func(ctx context.Context) error) error
}

// nameToSlug derives an entity ID from a display name, stripping characters
// that are unsafe in filesystem paths.
func nameToSlug(name string) string {
	return strings.ToLower(strings.ReplaceAll(fs.SanitizeFilename(name), " ", "-"))
}

func newIfPresent[T ~string](new, old T) T {
	if new != "" {
		return new
	} else {
		return old
	}
}
