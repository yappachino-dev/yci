package main

import (
	"fmt"

	"playlist-manager/internal/library"
	"playlist-manager/internal/utils"
)

func main() {
	utils.CheckHealth()

	lib := library.NewLibrary()

	lib.TreePrint()

	funcs := []func() error{
		func() error { return lib.AddFolder("hololive_jp", "hololive", "JP") },
		func() error { return lib.AddFolder("omaru_polka", "hololive_jp", "Omaru Polka") },
		func() error { return lib.AddFolder("hololive_en", "hololive", "EN") },
	}

	for _, f := range funcs {
		if err := f(); err != nil {
			fmt.Printf("fuckass error: %v\n", err)
		}
		lib.TreePrint()
	}

	// downloader.Download(lib.Paths.Temp, "https://youtu.be/TGBWr24_Vp4")

	lib.ProcessTempFolder()
}
