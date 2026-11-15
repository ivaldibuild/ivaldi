package main

import (
	"os"

	"github.com/ivaldibuild/ivaldi-k8s-agent/internal/agent"
)

// VERSION is the build's version, stamped with -ldflags "-X main.VERSION=…".
// It is a bare SemVer because the agent also sends it to the control plane in
// its heartbeats and in its User-Agent; the image's labels carry the commit.
var VERSION = "dev"

func main() {
	if err := agent.New(VERSION).Execute(); err != nil {
		os.Exit(1)
	}
}
