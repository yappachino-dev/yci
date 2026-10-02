package cli

import (
	"io"
	"strings"
)

// Table rendering.
//
// One dialect for every list command: upper-case headers, a dash rule sized
// to the header, and rows given as pre-formatted cells.
//
// Alignment is computed from display width, not delegated to text/tabwriter,
// which measures a cell in bytes and misaligns any column of CJK text. See
// style-guide 7.

// tablePadding is the gap between columns, in terminal cells.
const tablePadding = 2

// Table draws one aligned table with a consistent header.
type Table struct {
	w       io.Writer
	headers []string
	// rows accumulated before Flush, for the callers that build the body
	// before deciding whether there is anything to show.
	rows [][]string
	// offsets holds the display-cell offset of each column after Flush, so
	// the alignment contract can be checked rather than re-derived by a
	// test that has to guess how the padding was built.
	offsets []int
}

// NewTable starts a table with the given column headers.
func NewTable(out io.Writer, headers ...string) *Table {
	return &Table{
		w:       out,
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
	if len(t.headers) == 0 {
		return
	}

	// The widest cell sets each column's width. The scan covers the union of
	// headers and rows rather than trusting the header count.
	widths := make([]int, len(t.headers))
	for i, h := range t.headers {
		widths[i] = displayWidth(h)
	}
	for _, row := range t.rows {
		for i, cell := range row {
			if i >= len(widths) {
				widths = append(widths, 0)
			}
			if w := displayWidth(cell); w > widths[i] {
				widths[i] = w
			}
		}
	}

	writeRow(t.w, t.headers, widths)
	writeRow(t.w, dashRules(t.headers, widths), widths)
	for _, row := range t.rows {
		writeRow(t.w, row, widths)
	}

	t.offsets = columnOffsets(widths)
}

// columnOffsets is where each column starts, in display cells.
func columnOffsets(widths []int) []int {
	offsets := make([]int, len(widths))
	at := 0
	for i, w := range widths {
		offsets[i] = at
		at += w + tablePadding
	}
	return offsets
}

// ColumnOffsets reports where each column starts, in display cells. It is
// populated by Flush; before that it is empty.
func (t *Table) ColumnOffsets() []int {
	return t.offsets
}

// writeRow emits one padded line. Trailing padding is dropped so a line does
// not end in invisible whitespace, which matters when output is piped and
// when a test compares against a fixture.
func writeRow(w io.Writer, cells []string, widths []int) {
	var b strings.Builder

	for i, cell := range cells {
		if i > 0 {
			b.WriteString(strings.Repeat(" ", tablePadding))
		}
		b.WriteString(cell)

		// The last populated column is not padded, but interior ones are,
		// so the next column still starts in the same place on every row.
		if i < len(cells)-1 && i < len(widths) {
			b.WriteString(strings.Repeat(" ", widths[i]-displayWidth(cell)))
		}
	}

	b.WriteString("\n")
	_, _ = io.WriteString(w, b.String())
}

// dashRules builds the underline, sized to each header.
func dashRules(headers []string, _ []int) []string {
	rules := make([]string, len(headers))
	for i, h := range headers {
		// One dash per visible cell, so the rule spans the header and not
		// the whole column. Headers are ASCII today; measuring anyway keeps
		// that a property rather than an accident.
		rules[i] = strings.Repeat("-", displayWidth(h))
	}
	return rules
}
