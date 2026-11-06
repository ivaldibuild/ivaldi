// Package credentials keeps the sessions ivaldictl signed in with, one per
// control plane, in a file only the user can read.
package credentials

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// ErrNoSession means there is no stored session for the control plane.
var ErrNoSession = errors.New("not signed in")

// Session is a signed-in session: an access token, and the refresh token
// that gets the next one.
type Session struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	ExpiresAt    time.Time `json:"expires_at"`
}

// Expired reports whether the access token is expired, or expires within
// skew of now.
func (s Session) Expired(now time.Time, skew time.Duration) bool {
	return !now.Add(skew).Before(s.ExpiresAt)
}

// Store keeps sessions by control plane URL.
type Store interface {
	Load(server string) (Session, error)
	Save(server string, s Session) error
	Delete(server string) error
}

// NewFileStore returns a Store in dir/credentials.json. The file is
// readable only by its owner, as it holds refresh tokens.
func NewFileStore(dir string) Store {
	return &fileStore{path: filepath.Join(dir, "credentials.json")}
}

// DefaultDir is where sessions are kept: IVALDI_CONFIG_DIR, or ivaldi in the
// user's configuration directory.
func DefaultDir(getenv func(string) string) (string, error) {
	if dir := getenv("IVALDI_CONFIG_DIR"); dir != "" {
		return dir, nil
	}
	base, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("find the configuration directory (set IVALDI_CONFIG_DIR): %w", err)
	}
	return filepath.Join(base, "ivaldi"), nil
}

type fileStore struct {
	path string
}

type file struct {
	Sessions map[string]Session `json:"sessions"`
}

func (f *fileStore) read() (file, error) {
	out := file{Sessions: map[string]Session{}}
	b, err := os.ReadFile(f.path)
	if errors.Is(err, fs.ErrNotExist) {
		return out, nil
	}
	if err != nil {
		return out, fmt.Errorf("read %s: %w", f.path, err)
	}
	if err := json.Unmarshal(b, &out); err != nil {
		return out, fmt.Errorf("read %s: %w", f.path, err)
	}
	if out.Sessions == nil {
		out.Sessions = map[string]Session{}
	}
	return out, nil
}

// write replaces the file through a temporary one, so a crash never leaves
// it half-written.
func (f *fileStore) write(contents file) error {
	if err := os.MkdirAll(filepath.Dir(f.path), 0o700); err != nil {
		return fmt.Errorf("create %s: %w", filepath.Dir(f.path), err)
	}
	b, err := json.MarshalIndent(contents, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(f.path), ".credentials-*.json")
	if err != nil {
		return fmt.Errorf("write %s: %w", f.path, err)
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return fmt.Errorf("write %s: %w", f.path, err)
	}
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		return fmt.Errorf("write %s: %w", f.path, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("write %s: %w", f.path, err)
	}
	return os.Rename(tmp.Name(), f.path)
}

func (f *fileStore) Load(server string) (Session, error) {
	contents, err := f.read()
	if err != nil {
		return Session{}, err
	}
	s, ok := contents.Sessions[server]
	if !ok {
		return Session{}, ErrNoSession
	}
	return s, nil
}

func (f *fileStore) Save(server string, s Session) error {
	contents, err := f.read()
	if err != nil {
		return err
	}
	contents.Sessions[server] = s
	return f.write(contents)
}

func (f *fileStore) Delete(server string) error {
	contents, err := f.read()
	if err != nil {
		return err
	}
	delete(contents.Sessions, server)
	return f.write(contents)
}
