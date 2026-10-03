package cli

import (
	"strings"
	"testing"

	"github.com/yappachino-dev/yci/internal/library"
)

// TestFolderListColumnOrder pins the human layout: parent sits in the second
// column and the tree in the third, so the hierarchy reads right of its parent
// rather than being buried in the name column.
func TestFolderListColumnOrder(t *testing.T) {
	c := newTestContainer(t)
	ctx := t.Context()

	if _, err := c.FolderService.Create(ctx, "vt", "", library.FolderName("VT")); err != nil {
		t.Fatalf("create parent: %v", err)
	}

	child, err := c.FolderService.Create(ctx, "leaf", "vt", library.FolderName("Leaf"))
	if err != nil {
		t.Fatalf("create child: %v", err)
	}

	cmd, out := newListCmd(t, c)
	if err := listFolders(cmd.Context(), cmd); err != nil {
		t.Fatalf("folder list: %v", err)
	}

	lines := strings.Split(strings.TrimRight(out.String(), "\n"), "\n")
	if len(lines) < 3 {
		t.Fatalf("got %d lines, want a header, a rule and at least one row:\n%s", len(lines), out.String())
	}

	header := []string{"ID", "PARENT", "NAME"}
	for i, want := range header {
		if !strings.Contains(lines[0], want) {
			t.Fatalf("header %q does not name column %d: %q", want, i, lines[0])
		}

		if i > 0 && strings.Index(lines[0], want) < strings.Index(lines[0], header[i-1]) {
			t.Errorf("column %q appears before %q: %q", want, header[i-1], lines[0])
		}
	}

	var row string

	for _, line := range lines[2:] {
		if strings.Contains(line, "leaf") {
			row = line

			break
		}
	}

	if row == "" {
		t.Fatalf("no row for the leaf folder:\n%s", out.String())
	}

	parentAt := strings.Index(row, string(child.ParentID))
	if parentAt < 0 {
		t.Fatalf("row does not show the parent id %q: %q", child.ParentID, row)
	}

	treeAt := strings.Index(row, "Leaf")
	if treeAt < 0 {
		t.Fatalf("row does not show the folder name: %q", row)
	}

	if parentAt > treeAt {
		t.Errorf("parent (%d) should precede the tree entry (%d): %q", parentAt, treeAt, row)
	}
}
