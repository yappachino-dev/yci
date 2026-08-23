package main

import (
	"github.com/yappachino-dev/yci/internal/cli"
)

// version is stamped at build time via
// -ldflags "-X main.version=$(git describe --tags --always --dirty)".
var version = "dev"

func main() {
	cli.Execute(version)
}
