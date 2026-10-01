package main

import (
	"log"
	"os"
)

func main() {
	err := rootCmd.Execute()
	if err != nil {
		log.Fatalf("failed to start: %v", err)
		os.Exit(1)
	}
}
