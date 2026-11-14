package main

import (
	"os"

	"github.com/ivaldibuild/ivaldi-k8s-agent/internal/agent"
)

// VERSION is the build's version, stamped with -ldflags "-X main.VERSION=…".
var VERSION = "dev"

func main() {
	if err := agent.New(VERSION).Execute(); err != nil {
		os.Exit(1)
	}
}
