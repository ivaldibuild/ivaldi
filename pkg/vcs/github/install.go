package github

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/ivaldibuild/ivaldi/pkg/vcs"
)

// InstallURL returns the page where a person installs the App. The App must
// have "Request user authorization (OAuth) during installation" turned on,
// so GitHub sends the person back to its callback URL with an authorization
// code, the installation ID, and state.
func (c *Client) InstallURL(state string) string {
	apps := "/apps/"
	if c.webHost != "github.com" {
		apps = "/github-apps/"
	}
	return c.webURL + apps + url.PathEscape(c.slug) + "/installations/new?" + url.Values{"state": {state}}.Encode()
}

// VerifyInstallation exchanges the authorization code for the person's
// user token, and returns the installation only if it is one of this App's
// installations that the person can access. The installation ID in the
// callback comes through the browser, so it proves nothing on its own.
func (c *Client) VerifyInstallation(ctx context.Context, code, installationID string) (vcs.Installation, error) {
	if code == "" || installationID == "" {
		return vcs.Installation{}, fmt.Errorf("%w: an authorization code and an installation ID are required", vcs.ErrUnauthorized)
	}
	userToken, err := c.exchangeCode(ctx, code)
	if err != nil {
		return vcs.Installation{}, err
	}
	next := c.apiURL + "/user/installations?per_page=100"
	for next != "" {
		var page userInstallationsResponse
		header, err := c.getWithHeader(ctx, userToken, next, &page)
		if err != nil {
			return vcs.Installation{}, err
		}
		for _, inst := range page.Installations {
			if strconv.FormatInt(inst.ID, 10) == installationID && inst.AppID == c.appID {
				return vcs.Installation{
					Provider:       vcs.ProviderGitHub,
					HostURL:        c.webURL,
					InstallationID: installationID,
					AccountName:    inst.Account.Login,
				}, nil
			}
		}
		if next, err = c.next(header); err != nil {
			return vcs.Installation{}, err
		}
	}
	return vcs.Installation{}, fmt.Errorf("%w: the GitHub user cannot access installation %s", vcs.ErrUnauthorized, installationID)
}

type userInstallationsResponse struct {
	Installations []struct {
		ID      int64 `json:"id"`
		AppID   int64 `json:"app_id"`
		Account struct {
			Login string `json:"login"`
		} `json:"account"`
	} `json:"installations"`
}

// exchangeCode trades the authorization code GitHub sent back for the
// person's user token. Ivaldi uses it once, to list their installations,
// and does not keep it.
func (c *Client) exchangeCode(ctx context.Context, code string) (string, error) {
	form := url.Values{"client_id": {c.clientID}, "client_secret": {c.clientSecret}, "code": {code}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.webURL+"/login/oauth/access_token", strings.NewReader(form.Encode()))
	if err != nil {
		return "", fmt.Errorf("github: build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	res, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("%w: %w", vcs.ErrUnavailable, err)
	}
	defer res.Body.Close()
	// GitHub answers a refused code with 200 and an error field.
	var body struct {
		AccessToken string `json:"access_token"`
		Error       string `json:"error"`
	}
	if err := decodeResponse(res, &body); err != nil {
		return "", err
	}
	if body.Error != "" || body.AccessToken == "" {
		return "", fmt.Errorf("%w: GitHub refused the authorization code (%s)", vcs.ErrUnauthorized, body.Error)
	}
	return body.AccessToken, nil
}
