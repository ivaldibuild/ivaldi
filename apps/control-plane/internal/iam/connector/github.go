package connector

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"golang.org/x/oauth2"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam"
)

// GitHubDotCom is GitHub's own address.
const GitHubDotCom = "https://github.com"

// maxGitHubResponse bounds what is read from GitHub's API.
const maxGitHubResponse = 1 << 20

// GitHubConfig configures GitHub sign-in.
type GitHubConfig struct {
	// BaseURL is GitHub Enterprise Server's address; empty means github.com.
	BaseURL      string
	ClientID     string
	ClientSecret string
	// ReadOrganizations asks for the read:org scope and reads the person's
	// organizations, for providers restricted to organizations.
	ReadOrganizations bool
	HTTPClient        *http.Client
}

type githubConnector struct {
	cfg   GitHubConfig
	oauth *oauth2.Config
	api   string
}

// NewGitHub returns a Connector for GitHub. GitHub issues no ID token, so
// the person is read from GitHub's API: their numeric user ID as subject,
// which survives renaming the account, and only an email GitHub marks
// verified.
func NewGitHub(cfg GitHubConfig) (Connector, error) {
	if cfg.ClientID == "" {
		return nil, errors.New("connector: GitHub needs its client ID")
	}
	base := strings.TrimRight(cfg.BaseURL, "/")
	api := "https://api.github.com"
	if base == "" || base == GitHubDotCom {
		base = GitHubDotCom
	} else {
		api = base + "/api/v3"
	}
	scopes := []string{"read:user", "user:email"}
	if cfg.ReadOrganizations {
		scopes = append(scopes, "read:org")
	}
	return &githubConnector{
		cfg: cfg,
		api: api,
		oauth: &oauth2.Config{
			ClientID:     cfg.ClientID,
			ClientSecret: cfg.ClientSecret,
			Endpoint:     oauth2.Endpoint{AuthURL: base + "/login/oauth/authorize", TokenURL: base + "/login/oauth/access_token"},
			Scopes:       scopes,
		},
	}, nil
}

func (c *githubConnector) Protocol() iam.IdentityProtocol { return iam.ProtocolGitHub }
func (c *githubConnector) DisplayName() string            { return "GitHub" }

func (c *githubConnector) config(redirectURI string) *oauth2.Config {
	cfg := *c.oauth
	cfg.RedirectURL = redirectURI
	return &cfg
}

func (c *githubConnector) AuthCodeURL(_ context.Context, req AuthRequest) (string, error) {
	return c.config(req.RedirectURI).AuthCodeURL(req.State, oauth2.S256ChallengeOption(req.Verifier)), nil
}

func (c *githubConnector) Exchange(ctx context.Context, cb Callback) (iam.ExternalProfile, error) {
	if c.cfg.HTTPClient != nil {
		ctx = context.WithValue(ctx, oauth2.HTTPClient, c.cfg.HTTPClient)
	}
	tok, err := c.config(cb.RedirectURI).Exchange(ctx, cb.Code, oauth2.VerifierOption(cb.Verifier))
	if err != nil {
		return iam.ExternalProfile{}, fmt.Errorf("%w: %v", ErrExchange, err)
	}
	client := c.config(cb.RedirectURI).Client(ctx, tok)

	var user struct {
		ID    int64  `json:"id"`
		Login string `json:"login"`
		Name  string `json:"name"`
	}
	if err := c.get(ctx, client, "/user", &user); err != nil {
		return iam.ExternalProfile{}, err
	}
	if user.ID == 0 {
		return iam.ExternalProfile{}, fmt.Errorf("%w: GitHub gave no user ID", ErrExchange)
	}
	profile := iam.ExternalProfile{Subject: strconv.FormatInt(user.ID, 10), DisplayName: user.Name}
	if profile.DisplayName == "" {
		profile.DisplayName = user.Login
	}

	var emails []struct {
		Email    string `json:"email"`
		Primary  bool   `json:"primary"`
		Verified bool   `json:"verified"`
	}
	if err := c.get(ctx, client, "/user/emails?per_page=100", &emails); err != nil {
		return iam.ExternalProfile{}, err
	}
	for _, e := range emails {
		if e.Primary && e.Verified {
			profile.Email, profile.EmailVerified = e.Email, true
		}
	}

	if c.cfg.ReadOrganizations {
		var memberships []struct {
			Organization struct {
				Login string `json:"login"`
			} `json:"organization"`
		}
		if err := c.get(ctx, client, "/user/memberships/orgs?state=active&per_page=100", &memberships); err != nil {
			return iam.ExternalProfile{}, err
		}
		for _, m := range memberships {
			profile.Organizations = append(profile.Organizations, m.Organization.Login)
		}
	}
	return profile, nil
}

func (c *githubConnector) get(ctx context.Context, client *http.Client, path string, v any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.api+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrExchange, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%w: GitHub answered %s for %s", ErrExchange, resp.Status, strings.SplitN(path, "?", 2)[0])
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxGitHubResponse)).Decode(v); err != nil {
		return fmt.Errorf("%w: %v", ErrExchange, err)
	}
	return nil
}
