package github

import (
	"context"
	"net/url"
	"time"

	"github.com/ivaldibuild/ivaldi/pkg/vcs"
)

// CommitsBetween returns the commits reachable from to but not from, oldest
// first, following the provider's pages.
func (c *Client) CommitsBetween(ctx context.Context, at vcs.Endpoint, repo vcs.Repository, from, to string) ([]vcs.Commit, error) {
	if from == "" || to == "" {
		return nil, vcs.ErrInvalidRange
	}
	if from == to {
		return []vcs.Commit{}, nil
	}
	base, err := c.base(at)
	if err != nil {
		return nil, err
	}
	token, err := c.installationToken(ctx, base, at.InstallationID)
	if err != nil {
		return nil, err
	}
	next := base + "/repos/" + repo.FullName + "/compare/" + url.PathEscape(from+"..."+to) + "?per_page=100"
	var commits []vcs.Commit
	for next != "" {
		var page compareResponse
		header, err := c.getWithHeader(ctx, token, next, &page)
		if err != nil {
			return nil, err
		}
		commits = append(commits, page.commits()...)
		next = nextLink(header)
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
