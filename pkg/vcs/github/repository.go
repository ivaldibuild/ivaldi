package github

import (
	"context"
	"fmt"
	"net/url"

	"github.com/ivaldibuild/ivaldi/pkg/vcs"
)

// Repository reads one repository by GitHub's stable repository ID.
func (c *Client) Repository(ctx context.Context, at vcs.Endpoint, externalRepositoryID string) (vcs.Repository, error) {
	base, err := c.base(at)
	if err != nil {
		return vcs.Repository{}, err
	}
	token, err := c.installationToken(ctx, base, at.InstallationID)
	if err != nil {
		return vcs.Repository{}, err
	}
	var body repositoryResponse
	path := base + "/repositories/" + url.PathEscape(externalRepositoryID)
	if err := c.get(ctx, token, path, &body); err != nil {
		return vcs.Repository{}, err
	}
	return body.repository(at), nil
}

type repositoryResponse struct {
	ID            int64  `json:"id"`
	FullName      string `json:"full_name"`
	DefaultBranch string `json:"default_branch"`
	Visibility    string `json:"visibility"`
	CloneURL      string `json:"clone_url"`
	HTMLURL       string `json:"html_url"`
}

func (r repositoryResponse) repository(at vcs.Endpoint) vcs.Repository {
	return vcs.Repository{
		Provider:      at.Provider,
		HostURL:       at.HostURL,
		ExternalID:    fmt.Sprint(r.ID),
		FullName:      r.FullName,
		DefaultBranch: r.DefaultBranch,
		Visibility:    r.Visibility,
		CloneURL:      r.CloneURL,
		WebURL:        r.HTMLURL,
	}
}
