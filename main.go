package main

import (
	"github.com/r0n9/camkeep/internal/app"
)

var Version string = "dev"

func main() {
	app.Run(Version)
}
