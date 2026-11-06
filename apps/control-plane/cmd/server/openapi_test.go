package server

import (
	"os"
	"path/filepath"
	"testing"
)

// committed is the API description the CLI and the web app generate their
// clients from, at the repository root.
var committed = filepath.Join("..", "..", "..", "..", "api", "openapi.json")

// The committed description must be what the server serves, or the clients
// generated from it drift from the API. Regenerate it with make openapi.
func TestTheCommittedOpenAPIIsCurrent(t *testing.T) {
	want, err := OpenAPI()
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(committed)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Fatalf("%s is out of date with the API; run make openapi", committed)
	}
}

// Every operation the server serves is described, onboarding included.
func TestTheDescriptionCoversEveryRoute(t *testing.T) {
	served := services{}.routes(false)
	described := services{}.routes(true)
	if len(described) != len(served)+2 {
		t.Fatalf("describing adds onboarding and sign-in to the %d served controllers, got %d", len(served), len(described))
	}
}
