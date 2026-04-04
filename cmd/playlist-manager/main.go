package main

import (
	"flag"
	"fmt"
	"os"
	"text/tabwriter"

	"playlist-manager/internal/library"
	"playlist-manager/internal/utils"
)

func main() {
	utils.CheckHealth()

	lib, err := library.NewLibrary()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error initializing: %v\n", err)
		return
	}
	defer lib.Close()

	if err := lib.Init(); err != nil {
		fmt.Fprintf(os.Stderr, "Database error: %v\n", err)
		return
	}

	argc := len(os.Args)

	if argc < 2 {
		fmt.Println("Usage: cli <command>")
		return
	}

	switch os.Args[1] {
	case "ls":
		cmd := flag.NewFlagSet("ls", flag.ExitOnError)

		showFolders := cmd.Bool("f", false, "List folders")
		showArtists := cmd.Bool("a", false, "List artists")
		showChannels := cmd.Bool("c", false, "List channels")

		cmd.Parse(os.Args[2:])

		if !*showFolders && !*showArtists && !*showChannels {
			*showFolders = true
		}

		w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)

		if *showFolders {
			folders, err := lib.GetFolders()
			if err != nil {
				fmt.Printf("Error fetching folders: %v\n", err)
				return
			}

			childrenOf := make(map[string][]library.Folder)
			for _, f := range folders {
				childrenOf[f.ParentID] = append(childrenOf[f.ParentID], f)
			}

			fmt.Fprintln(w, "ID\tNAME\tPARENT")
			fmt.Fprintln(w, "--\t----\t------")
			fmt.Fprintln(w, "root\tMusic\t")

			var traverse func(parentID string, prefix string)
			traverse = func(parentID string, prefix string) {
				children := childrenOf[parentID]
				for i, child := range children {
					isLast := (i == len(children)-1)

					connector := "├─ "
					if isLast {
						connector = "└─ "
					}

					nameWithTree := prefix + connector + child.DisplayName
					fmt.Fprintf(w, "%s\t%s\t%s\n", child.ID, nameWithTree, child.ParentID)

					newPrefix := prefix + "│  "
					if isLast {
						newPrefix = prefix + "   "
					}

					traverse(child.ID, newPrefix)
				}
			}

			traverse("root", "")
			w.Flush()
			fmt.Println()
		}

		if *showArtists {
			artists, err := lib.GetArtists()
			if err != nil {
				fmt.Printf("Error fetching artists: %v\n", err)
				return
			}

			fmt.Fprintln(w, "ID\tNAME\tFOLDER")
			fmt.Fprintln(w, "--\t----\t------")
			for _, a := range artists {
				fmt.Fprintf(w, "%s\t%s\t%s\n", a.ID, a.Artist, a.FolderID)
			}
			w.Flush()
			fmt.Println()
		}

		if *showChannels {
			channels, err := lib.GetChannels()
			if err != nil {
				fmt.Printf("Error fetching channels: %v\n", err)
				return
			}

			fmt.Fprintln(w, "ID\tARTIST_ID\tCHANNEL_NAME")
			fmt.Fprintln(w, "--\t---------\t------------")
			for _, c := range channels {
				fmt.Fprintf(w, "%d\t%s\t%s\n", c.ID, c.ArtistID, c.ChannelName)
			}
			w.Flush()
			fmt.Println()
		}
	case "mk-folder":
		cmd := flag.NewFlagSet("mk-folder", flag.ExitOnError)

		id := cmd.String("i", "", "Folder ID (slug)")
		name := cmd.String("n", "", "Display Name")
		parent := cmd.String("p", "root", "Parent ID")

		cmd.Parse(os.Args[2:])

		if *id == "" || *name == "" {
			fmt.Printf("Error: both -i (folder id) and -n (name) required\n")
			return
		}

		if err := lib.AddFolder(*id, *name, *parent); err != nil {
			fmt.Printf("Error: %v\n", err)
		} else {
			fmt.Printf("Created { id: %s, name: %s, parent: %s } folder\n", *id, *name, *parent)
		}
	case "rm-folder":
		cmd := flag.NewFlagSet("rm-folder", flag.ExitOnError)

		id := cmd.String("i", "", "Folder ID (slug)")
		force := cmd.Bool("f", false, "Force remove")

		cmd.Parse(os.Args[2:])

		if *id == "" {
			fmt.Println("Error: -i (folder id) is required")
			return
		}

		if err := lib.RemoveFolder(*id, *force); err != nil {
			fmt.Printf("Error: %v\n", err)
		} else {
			fmt.Printf("Removed %q folder\n", *id)
		}
	case "mv-folder":
		cmd := flag.NewFlagSet("mv-folder", flag.ExitOnError)

		id := cmd.String("i", "", "Folder ID")
		parent := cmd.String("p", "root", "Target Parent Folder ID")
		cmd.Parse(os.Args[2:])

		if *id == "" {
			fmt.Println("Error: -i (folder id) is required")
			return
		}

		if err := lib.MoveFolder(*id, *parent); err != nil {
			fmt.Printf("Error: %v\n", err)
		} else {
			fmt.Printf("Moved folder %q to parent %q\n", *id, *parent)
		}
	case "mk-artist":
		cmd := flag.NewFlagSet("mk-artist", flag.ExitOnError)

		id := cmd.String("i", "", "Artist ID (slug)")
		name := cmd.String("n", "", "Display Name")
		folder := cmd.String("f", "root", "Folder ID")
		cmd.Parse(os.Args[2:])

		if *id == "" || *name == "" {
			fmt.Println("Error: both -i (id) and -n (name) are required")
			return
		}

		if err := lib.AddArtist(*id, *name, *folder); err != nil {
			fmt.Printf("Error: %v\n", err)
		} else {
			fmt.Printf("Created { id: %s, name: %s, folder: %s } artist\n", *id, *name, *folder)
		}
	case "rm-artist":
		cmd := flag.NewFlagSet("rm-artist", flag.ExitOnError)

		id := cmd.String("i", "", "Artist ID (slug)")
		cmd.Parse(os.Args[2:])

		if *id == "" {
			fmt.Println("Error: -i (id) is required")
			return
		}

		if err := lib.RemoveArtist(*id); err != nil {
			fmt.Printf("Error: %v\n", err)
		} else {
			fmt.Printf("Removed %q artist\n", *id)
		}
	case "mv-artist":
		cmd := flag.NewFlagSet("mv-artist", flag.ExitOnError)

		id := cmd.String("i", "", "Artist ID")
		folder := cmd.String("f", "root", "Target Folder ID")
		cmd.Parse(os.Args[2:])

		if *id == "" {
			fmt.Println("Error: -i (artist id) is required")
			return
		}

		if err := lib.MapToFolder(*id, *folder); err != nil {
			fmt.Printf("Error: %v\n", err)
		} else {
			fmt.Printf("Moved artist %q to folder %q\n", *id, *folder)
		}
	default:
		fmt.Println("bad args")
	}
}
