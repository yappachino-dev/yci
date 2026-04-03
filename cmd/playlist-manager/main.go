package main

import (
	"playlist-manager/internal/library"
	"playlist-manager/internal/utils"
)

func main() {
	utils.CheckHealth()

	ft := library.FolderTree{
		{"artists", "root", "Artists"},
		{"ado", "artists", "Ado"},
		{"vt", "artists", "VT"},
		{"hololive", "vt", "Hololive"},
	}

	library.Print(ft.ToMap())
}
