package github

import (
	"context"
	"fmt"
	"net/url"

	"github.com/ivaldibuild/ivaldi/pkg/vcs"
)

// Repository reads one repository by GitHub's stable repository ID.
func (c *Client) Repository(ctx context.Context, at vcs.Endpoint, externalRepositoryID string) (vcs.Repository, error) {
	if err := c.checkEndpoint(at); err != nil {
		return vcs.Repository{}, err
	}
	token, err := c.installationToken(ctx, at.InstallationID)
	if err != nil {
		return vcs.Repository{}, err
	}
	var body repositoryResponse
	path := c.apiURL + "/repositories/" + url.PathEscape(externalRepositoryID)
	if err := c.get(ctx, token, path, &body); err != nil {
		return vcs.Repository{}, err
	}
	return body.repository(at), nil
}

// maxRepositoryPages bounds a listing at 50 pages of 100 repositories.
const maxRepositoryPages = 50

// Repositories lists the repositories the installation can read, following
// the provider's pages.
func (c *Client) Repositories(ctx context.Context, at vcs.Endpoint) ([]vcs.Repository, error) {
	if err := c.checkEndpoint(at); err != nil {
		return nil, err
	}
	token, err := c.installationToken(ctx, at.InstallationID)
	if err != nil {
		return nil, err
	}
	repositories := []vcs.Repository{}
	next := c.apiURL + "/installation/repositories?per_page=100"
	for pages := 0; next != ""; pages++ {
		if pages == maxRepositoryPages {
			return nil, fmt.Errorf("%w: more than %d", vcs.ErrTooManyRepositories, maxRepositoryPages*100)
		}
		var page struct {
			Repositories []repositoryResponse `json:"repositories"`
		}
		header, err := c.getWithHeader(ctx, token, next, &page)
		if err != nil {
			return nil, err
		}
		for _, r := range page.Repositories {
			repositories = append(repositories, r.repository(at))
		}
		if next, err = c.next(header); err != nil {
			return nil, err
		}
	}
	return repositories, nil
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
