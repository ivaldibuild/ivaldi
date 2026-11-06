package main

import (
	"os"

	"github.com/ivaldibuild/ivaldi-cli/internal/cli"
)

func main() {
	// Cobra prints the error; the exit status tells CI the command failed.
	if err := cli.NewCLI().Execute(); err != nil {
		os.Exit(1)
	}
}
