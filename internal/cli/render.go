package cli

import (
	"io"
	"text/tabwriter"
)

// newTabWriter returns the aligned column writer shared by every list
// command, so table output looks the same across entity groups.
func newTabWriter(w io.Writer) *tabwriter.Writer {
	return tabwriter.NewWriter(w, 0, 0, 3, ' ', 0)
}
