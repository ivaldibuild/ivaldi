package main

import (
	"log"

	"github.com/ivaldibuild/ivaldi-cli/internal/cli"
)

func main() {
	cmd := cli.NewCLI()
	if err := cmd.Execute(); err != nil {
		log.Fatal(err)
	}
}
