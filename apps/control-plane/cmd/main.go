package main

import (
	"log"
)

func main() {
	err := Execute()
	if err != nil {
		log.Fatalf("failed to start: %v", err)
	}
}
