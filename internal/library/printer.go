package library

import "fmt"

func printNode(id string, prefix string, childrenOf map[string][]Folder) {
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

		printNode(child.ID, newPrefix, childrenOf)
	}
}

func Print(fm FolderMap) {
	childrenOf := make(map[string][]Folder)

	for _, f := range fm {
		childrenOf[f.Parent] = append(childrenOf[f.Parent], f)
	}

	fmt.Printf("%s\n", root)
	printNode(root, "", childrenOf)
}
