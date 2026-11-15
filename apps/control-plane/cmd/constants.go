package main

// The build stamps these with -ldflags "-X main.<name>=<value>" (see the
// Makefile and apps/control-plane/Dockerfile). A rebuild of one commit is
// stamped with the same values.
var (
	// VERSION is the release version, a SemVer. The API's description and the
	// onboarding guide read it, so it stays free of decoration.
	VERSION = "dev"
	// COMMIT is the short commit the binary was built from.
	COMMIT = "unknown"
	// BUILDTIME is that commit's time, RFC 3339.
	BUILDTIME = "unknown"
)
