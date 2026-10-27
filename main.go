package main

import (
	"os"

	"v/internal/cli"
)

func main() {
	os.Exit(cli.Main(os.Args[1:]))
}
