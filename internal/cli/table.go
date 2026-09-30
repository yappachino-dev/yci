package cli

import (
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
)

// Table rendering.
//
// Every list command draws an aligned table, and until this type existed each
// one invented its own header dialect: some wrote a row of dashes beneath the
// header, some wrote one dash per column and some wrote nothing at all. A
// user moving between `track list` and `playlist show` was reading two
// different-looking tables with two different header styles, which is a small
// tax paid on every command.
//
// The dialect is therefore defined once, here: upper-case column names
// separated by two spaces in the source, a rule of dashes sized to each
// header, and rows supplied as pre-formatted cells.

// tabwriterPadding is the gap between columns. Three spaces reads as a column
// separator at a glance and is not so wide that short tables waste a terminal.
const tabwriterPadding = 3

// newTabWriter returns the aligned column writer shared by every list
// command.
func newTabWriter(w io.Writer) *tabwriter.Writer {
	return tabwriter.NewWriter(w, 0, 0, tabwriterPadding, ' ', 0)
}

// Table draws one aligned table with a consistent header.
type Table struct {
	w       *tabwriter.Writer
	headers []string
	// rows accumulated before Start, for the callers that build the body
	// before deciding whether there is anything to show.
	rows [][]string
}

// NewTable starts a table with the given column headers.
func NewTable(out io.Writer, headers ...string) *Table {
	return &Table{
		w:       newTabWriter(out),
		headers: headers,
	}
}

// Row appends a row. Cells are taken as given; a caller wanting truncation
// or whitespace collapsing asks for it explicitly, because silently mangling
// a value to fit a column is how a title becomes unreadable.
func (t *Table) Row(cells ...string) {
	t.rows = append(t.rows, cells)
}

// Flush writes the header, the rule, and the accumulated rows.
func (t *Table) Flush() {
	t.write(t.w)
	t.w.Flush()
}

// Write renders straight through without buffering, for callers that stream
// a row at a time.
func (t *Table) Write(rowFn func(w io.Writer)) {
	t.write(t.w)
	rowFn(t.w)
	t.w.Flush()
}

func (t *Table) write(w io.Writer) {
	if len(t.headers) == 0 {
		return
	}

	fmt.Fprintln(w, strings.Join(t.headers, "\t"))

	rules := make([]string, len(t.headers))
	for i, h := range t.headers {
		rules[i] = strings.Repeat("-", len(h))
	}
	fmt.Fprintln(w, strings.Join(rules, "\t"))

	for _, row := range t.rows {
		fmt.Fprintln(w, strings.Join(row, "\t"))
	}
}
