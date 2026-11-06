// Package app holds what every ivaldictl command uses: the environment, the
// control plane to call, and how to authenticate to it. Tests replace its
// fields instead of touching the real environment, network, or browser.
package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/ivaldibuild/ivaldi-cli/internal/apiclient"
	"github.com/ivaldibuild/ivaldi-cli/internal/ciinfo"
	"github.com/ivaldibuild/ivaldi-cli/internal/client"
	"github.com/ivaldibuild/ivaldi-cli/internal/credentials"
)

// DefaultServer is the control plane a local stack serves.
const DefaultServer = "http://localhost:8660"

// App is the environment commands run in.
type App struct {
	Getenv func(string) string
	Now    func() time.Time
	HTTP   *http.Client
	// Store keeps signed-in sessions. Nil uses the file in the
	// configuration directory.
	Store       credentials.Store
	OpenBrowser func(url string) error
	Out         io.Writer
}

// Default returns the App for the real process.
func Default() *App {
	return &App{
		Getenv:      os.Getenv,
		Now:         time.Now,
		HTTP:        &http.Client{Timeout: 60 * time.Second},
		OpenBrowser: openBrowser,
		Out:         os.Stdout,
	}
}

// Server returns the control plane's URL: --server-url, IVALDI_SERVER_URL,
// or DefaultServer.
func (a *App) Server(cmd *cobra.Command) (string, error) {
	server, _ := cmd.Flags().GetString("server-url")
	if server == "" {
		server = a.Getenv("IVALDI_SERVER_URL")
	}
	if server == "" {
		server = DefaultServer
	}
	u, err := url.Parse(server)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return "", fmt.Errorf("server URL %q must be an http or https URL", server)
	}
	return strings.TrimRight(server, "/"), nil
}

// CredentialStore returns the session store.
func (a *App) CredentialStore() (credentials.Store, error) {
	if a.Store != nil {
		return a.Store, nil
	}
	dir, err := credentials.DefaultDir(a.Getenv)
	if err != nil {
		return nil, err
	}
	a.Store = credentials.NewFileStore(dir)
	return a.Store, nil
}

// APIToken returns the API token to use: --api-token, or IVALDI_API_TOKEN.
func (a *App) APIToken(cmd *cobra.Command) string {
	if token, _ := cmd.Flags().GetString("api-token"); token != "" {
		return token
	}
	return a.Getenv("IVALDI_API_TOKEN")
}

// ErrNoCredentials means a command in CI found neither an API token nor a
// session, and CI cannot sign in through a browser.
var ErrNoCredentials = errors.New("no credentials in CI: create a service account API token and set IVALDI_API_TOKEN")

// Client returns a client for the control plane, authenticated with an API
// token when one is given, and otherwise with the signed-in session. In CI
// with neither, it fails at once rather than waiting for a browser.
func (a *App) Client(cmd *cobra.Command) (*apiclient.ClientWithResponses, error) {
	server, err := a.Server(cmd)
	if err != nil {
		return nil, err
	}
	if token := a.APIToken(cmd); token != "" {
		return client.New(server, a.HTTP, client.APIToken(token))
	}
	store, err := a.CredentialStore()
	if err != nil {
		return nil, err
	}
	if ciinfo.Detect(a.Getenv).InCI {
		if _, err := store.Load(server); errors.Is(err, credentials.ErrNoSession) {
			return nil, ErrNoCredentials
		}
	}
	return client.New(server, a.HTTP, client.NewSessionSource(store, server, a.HTTP, a.Now))
}

// openBrowser opens url in the user's browser.
func openBrowser(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	return cmd.Start()
}

// Print writes data as indented JSON with --output json, the default, and
// otherwise as text writes it.
func (a *App) Print(cmd *cobra.Command, data any, text func(w io.Writer)) error {
	format, _ := cmd.Flags().GetString("output")
	switch format {
	case "", "json":
		enc := json.NewEncoder(a.Out)
		enc.SetIndent("", "  ")
		return enc.Encode(data)
	case "text":
		text(a.Out)
		return nil
	}
	return fmt.Errorf("output %q must be json or text", format)
}
