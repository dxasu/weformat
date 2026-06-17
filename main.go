package main

import (
	"os"

	"weformat/internal/app"
)

func main() {
	os.Exit(app.Run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}
