// Package client calls the control plane's /v1 operations.
package client

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/ivaldibuild/ivaldi-cli/internal/apiclient"
	"github.com/ivaldibuild/ivaldi-cli/internal/credentials"
	"github.com/ivaldibuild/ivaldi-cli/internal/oauth"
)

// TokenSource gives the bearer token for a request.
type TokenSource interface {
	Token(ctx context.Context) (string, error)
	// Describe says how requests are authenticated, for auth status.
	Describe() string
}

// APIToken is a fixed token, such as a service account's in CI.
type APIToken string

func (t APIToken) Token(context.Context) (string, error) { return string(t), nil }
func (APIToken) Describe() string                        { return "an API token" }

// NewSessionSource returns a TokenSource from the stored session for
// server, refreshing it when its access token has expired and storing the
// new one.
func NewSessionSource(store credentials.Store, server string, httpc *http.Client, now func() time.Time) TokenSource {
	return &sessionSource{store: store, server: server, http: httpc, now: now}
}

type sessionSource struct {
	store  credentials.Store
	server string
	http   *http.Client
	now    func() time.Time
	mu     sync.Mutex
}

// refreshSkew refreshes an access token this long before it expires, so it
// does not expire on its way to the control plane.
const refreshSkew = 30 * time.Second

func (s *sessionSource) Token(ctx context.Context) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	session, err := s.store.Load(s.server)
	if errors.Is(err, credentials.ErrNoSession) {
		return "", fmt.Errorf("not signed in to %s; run ivaldictl auth login, or set IVALDI_API_TOKEN", s.server)
	}
	if err != nil {
		return "", err
	}
	if !session.Expired(s.now(), refreshSkew) {
		return session.AccessToken, nil
	}
	next, err := oauth.Refresh(ctx, s.http, s.server, session.RefreshToken, s.now)
	if errors.Is(err, oauth.ErrSignInAgain) {
		_ = s.store.Delete(s.server)
		return "", err
	}
	if err != nil {
		return "", err
	}
	if err := s.store.Save(s.server, next); err != nil {
		return "", err
	}
	return next.AccessToken, nil
}

func (s *sessionSource) Describe() string { return "a signed-in session" }

// New returns the control plane's API client, generated from its
// description, authenticating every request with tokens.
func New(server string, httpc *http.Client, tokens TokenSource) (*apiclient.ClientWithResponses, error) {
	if httpc == nil {
		httpc = &http.Client{Timeout: 60 * time.Second}
	}
	return apiclient.NewClientWithResponses(strings.TrimRight(server, "/"),
		apiclient.WithHTTPClient(httpc),
		apiclient.WithRequestEditorFn(func(ctx context.Context, req *http.Request) error {
			token, err := tokens.Token(ctx)
			if err != nil {
				return err
			}
			req.Header.Set("Authorization", "Bearer "+token)
			req.Header.Set("Accept", "application/json")
			return nil
		}),
	)
}

// Response is what every generated operation's response has.
type Response interface {
	StatusCode() int
	GetApplicationproblemJSONDefault() *apiclient.ErrorModel
}

// Error is a refusal from the control plane, with its problem detail.
type Error struct {
	Status int
	Detail string
}

func (e *Error) Error() string {
	switch {
	case e.Status == http.StatusUnauthorized:
		return "the control plane did not accept the credentials: " + e.Detail
	case e.Detail != "":
		return e.Detail
	}
	return fmt.Sprintf("the control plane answered %d", e.Status)
}

// Check returns the control plane's refusal for a response that is not a
// success. decoded says whether a success's body was read, so a success the
// CLI cannot read is an error rather than a nil body later.
func Check(resp Response, decoded bool) error {
	if status := resp.StatusCode(); status < 300 {
		if !decoded {
			return fmt.Errorf("the control plane answered %d with a body ivaldictl could not read", status)
		}
		return nil
	}
	e := &Error{Status: resp.StatusCode()}
	if problem := resp.GetApplicationproblemJSONDefault(); problem != nil {
		if problem.Detail != nil {
			e.Detail = *problem.Detail
		}
		if problem.Errors != nil {
			for _, d := range *problem.Errors {
				where := ""
				if d.Location != nil {
					where = *d.Location + " "
				}
				if d.Message != nil {
					e.Detail += "; " + where + *d.Message
				}
			}
		}
	}
	return e
}
