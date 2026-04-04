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
		func() error { return lib.AddFolder("jp", "holo", "JP") },
		func() error { return lib.AddFolder("jp", "hololive", "JP") },
		func() error { return lib.AddFolder("en", "hololive", "EN") },
		func() error { return lib.RemoveFolder("holo") },
		func() error { return lib.RemoveFolder("hololive") },
		func() error { return lib.RemoveFolder("en") },
	}

	for _, f := range funcs {
		if err := f(); err != nil {
			fmt.Printf("fuckass error: %v\n", err)
		}
		lib.TreePrint()
	}
}
