package github

import (
	"context"
	"fmt"
	"net/url"
	"time"

	"github.com/ivaldibuild/ivaldi/pkg/vcs"
)

// maxComparePages bounds one comparison at 50 pages of 100 commits, so a
// range between distant commits fails rather than reading without end.
const maxComparePages = 50

// CommitsBetween returns the commits reachable from to but not from, oldest
// first, following the provider's pages. It addresses the repository by its
// stable ID, so a rename or transfer does not break it.
func (c *Client) CommitsBetween(ctx context.Context, at vcs.Endpoint, repo vcs.Repository, from, to string) ([]vcs.Commit, error) {
	if from == "" || to == "" {
		return nil, vcs.ErrInvalidRange
	}
	if from == to {
		return []vcs.Commit{}, nil
	}
	if err := c.checkEndpoint(at); err != nil {
		return nil, err
	}
	if repo.ExternalID == "" {
		return nil, fmt.Errorf("github: the repository has no ID")
	}
	token, err := c.installationToken(ctx, at.InstallationID)
	if err != nil {
		return nil, err
	}
	next := c.apiURL + "/repositories/" + url.PathEscape(repo.ExternalID) +
		"/compare/" + url.PathEscape(from+"..."+to) + "?per_page=100"
	commits := []vcs.Commit{}
	for pages := 0; next != ""; pages++ {
		if pages == maxComparePages {
			return nil, fmt.Errorf("%w: more than %d commits", vcs.ErrRangeTooLarge, maxComparePages*100)
		}
		var page compareResponse
		header, err := c.getWithHeader(ctx, token, next, &page)
		if err != nil {
			return nil, err
		}
		commits = append(commits, page.commits()...)
		if next, err = c.next(header); err != nil {
			return nil, err
		}
	}
	return commits, nil
}

type compareResponse struct {
	Status  string `json:"status"`
	Commits []struct {
		SHA     string `json:"sha"`
		HTMLURL string `json:"html_url"`
		Commit  struct {
			Message string `json:"message"`
			Author  struct {
				Name  string    `json:"name"`
				Email string    `json:"email"`
				Date  time.Time `json:"date"`
			} `json:"author"`
		} `json:"commit"`
	} `json:"commits"`
}

func (c compareResponse) commits() []vcs.Commit {
	out := make([]vcs.Commit, 0, len(c.Commits))
	for _, commit := range c.Commits {
		var url *string
		if commit.HTMLURL != "" {
			u := commit.HTMLURL
			url = &u
		}
		out = append(out, vcs.Commit{
			SHA:         commit.SHA,
			Message:     commit.Commit.Message,
			AuthorName:  commit.Commit.Author.Name,
			AuthorEmail: commit.Commit.Author.Email,
			CommittedAt: commit.Commit.Author.Date,
			URL:         url,
		})
	}
	return out
}
