package main

import (
	"playlist-manager/cmd"
)

func main() {
	cmd.Execute()
	// // downloader.Download(lib.Paths.Staging, "https://youtu.be/TGBWr24_Vp4")

	// switch os.Args[1] {
	// case "ps":
	// 	lib.ProcessStaging()
	// case "rm-folder":
	// 	cmd := flag.NewFlagSet("rm-folder", flag.ExitOnError)
	//
	// 	id := cmd.String("i", "", "Folder ID (slug)")
	// 	force := cmd.Bool("f", false, "Force remove")
	//
	// 	cmd.Parse(os.Args[2:])
	//
	// 	if *id == "" {
	// 		fmt.Println("Error: -i (folder id) is required")
	// 		return
	// 	}
	//
	// 	if err := lib.RemoveFolder(*id, *force); err != nil {
	// 		fmt.Printf("Error: %v\n", err)
	// 	} else {
	// 		fmt.Printf("Removed %q folder\n", *id)
	// 	}
	// case "mv-folder":
	// 	cmd := flag.NewFlagSet("mv-folder", flag.ExitOnError)
	//
	// 	id := cmd.String("i", "", "Folder ID")
	// 	parent := cmd.String("p", library.RootID, "Target Parent Folder ID")
	//
	// 	cmd.Parse(os.Args[2:])
	//
	// 	if *id == "" {
	// 		fmt.Println("Error: -i (folder id) is required")
	// 		return
	// 	}
	//
	// 	if err := lib.MoveFolder(*id, *parent); err != nil {
	// 		fmt.Printf("Error: %v\n", err)
	// 	} else {
	// 		fmt.Printf("Moved folder %q to parent %q\n", *id, *parent)
	// 	}
	// case "rm-artist":
	// 	cmd := flag.NewFlagSet("rm-artist", flag.ExitOnError)
	//
	// 	id := cmd.String("i", "", "Artist ID (slug)")
	//
	// 	cmd.Parse(os.Args[2:])
	//
	// 	if *id == "" {
	// 		fmt.Println("Error: -i (id) is required")
	// 		return
	// 	}
	//
	// 	if err := lib.RemoveArtist(*id); err != nil {
	// 		fmt.Printf("Error: %v\n", err)
	// 	} else {
	// 		fmt.Printf("Removed %q artist\n", *id)
	// 	}
	// case "mv-artist":
	// 	cmd := flag.NewFlagSet("mv-artist", flag.ExitOnError)
	//
	// 	id := cmd.String("i", "", "Artist ID")
	// 	folder := cmd.String("f", library.RootID, "Target Folder ID")
	//
	// 	cmd.Parse(os.Args[2:])
	//
	// 	if *id == "" {
	// 		fmt.Println("Error: -i (artist id) is required")
	// 		return
	// 	}
	//
	// 	if err := lib.MapToFolder(*id, *folder); err != nil {
	// 		fmt.Printf("Error: %v\n", err)
	// 	} else {
	// 		fmt.Printf("Moved artist %q to folder %q\n", *id, *folder)
	// 	}
	// default:
	// 	fmt.Println("bad args")
	// }
}
