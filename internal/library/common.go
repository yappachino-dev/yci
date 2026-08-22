package library

import "context"

import "strings"

// Transactor runs fn inside a database transaction, joining any transaction
// already active on ctx.
type Transactor interface {
	WithinTransaction(ctx context.Context, fn func(ctx context.Context) error) error
}

func nameToSlug(name string) string {
	return strings.ToLower(strings.ReplaceAll(name, " ", "-"))
}

func newIfPresent[T ~string](new, old T) T {
	if new != "" {
		return new
	} else {
		return old
	}
}
