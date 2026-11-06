package credentials

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestFileStore(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "ivaldi")
	store := NewFileStore(dir)
	if _, err := store.Load("https://a.example.com"); !errors.Is(err, ErrNoSession) {
		t.Fatalf("an empty store has no session, got %v", err)
	}
	s := Session{AccessToken: "at", RefreshToken: "rt", ExpiresAt: time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)}
	if err := store.Save("https://a.example.com", s); err != nil {
		t.Fatal(err)
	}
	if err := store.Save("https://b.example.com", Session{AccessToken: "other"}); err != nil {
		t.Fatal(err)
	}
	got, err := store.Load("https://a.example.com")
	if err != nil || got != s {
		t.Fatalf("load: %+v %v", got, err)
	}

	info, err := os.Stat(filepath.Join(dir, "credentials.json"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("the file holds refresh tokens, so only its owner may read it: %v", info.Mode().Perm())
	}
	if d, _ := os.Stat(dir); d.Mode().Perm() != 0o700 {
		t.Errorf("directory mode %v", d.Mode().Perm())
	}

	if err := store.Delete("https://a.example.com"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Load("https://a.example.com"); !errors.Is(err, ErrNoSession) {
		t.Errorf("deleted, got %v", err)
	}
	if got, _ := store.Load("https://b.example.com"); got.AccessToken != "other" {
		t.Error("deleting one server's session keeps the others")
	}
}

func TestDefaultDir(t *testing.T) {
	dir, err := DefaultDir(func(k string) string {
		if k == "IVALDI_CONFIG_DIR" {
			return "/tmp/ivaldi-test"
		}
		return ""
	})
	if err != nil || dir != "/tmp/ivaldi-test" {
		t.Errorf("IVALDI_CONFIG_DIR wins: %q %v", dir, err)
	}
}

func TestExpired(t *testing.T) {
	now := time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)
	s := Session{ExpiresAt: now.Add(time.Minute)}
	if s.Expired(now, 30*time.Second) {
		t.Error("a minute left is not expired with 30s of skew")
	}
	if !s.Expired(now, 2*time.Minute) {
		t.Error("within the skew counts as expired")
	}
}
