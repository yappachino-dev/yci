package library

import "fmt"

func printTreeNode(id string, prefix string, childrenOf map[string][]Folder) {
	children := childrenOf[id]

	for i, child := range children {
		isLast := (i + 1) == len(children)

		connector := "├─ "
		if isLast {
			connector = "└─ "
		}

		fmt.Printf("%s%s%s\n", prefix, connector, child.DisplayName)

		newPrefix := prefix + "│  "
		if isLast {
			newPrefix = prefix + "   "
		}

		printTreeNode(child.ID, newPrefix, childrenOf)
	}
}

func (l *Library) treePrint() error {
	childrenOf := make(map[string][]Folder)

	folders, err := l.GetFolders()
	if err != nil {
		return err
	}

	for _, f := range folders {
		childrenOf[f.ParentID] = append(childrenOf[f.ParentID], f)
	}

	fmt.Printf("%s\n", root)
	printTreeNode(root, "", childrenOf)

	return nil
}
