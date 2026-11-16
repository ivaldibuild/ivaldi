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

// Compare returns the commits moving from one commit to another adds and
// removes, oldest first, following the provider's pages. It addresses the
// repository by its stable ID, so a rename or transfer does not break it.
//
// GitHub lists only the commits the head gained since the merge base, so a
// move that also loses commits, such as a rollback, is compared a second
// time the other way.
func (c *Client) Compare(ctx context.Context, at vcs.Endpoint, repo vcs.Repository, from, to string) (vcs.Comparison, error) {
	if from == "" || to == "" {
		return vcs.Comparison{}, vcs.ErrInvalidRange
	}
	if from == to {
		return vcs.Comparison{Added: []vcs.Commit{}, Removed: []vcs.Commit{}}, nil
	}
	if err := c.checkEndpoint(at); err != nil {
		return vcs.Comparison{}, err
	}
	if repo.ExternalID == "" {
		return vcs.Comparison{}, fmt.Errorf("github: the repository has no ID")
	}
	token, err := c.installationToken(ctx, at.InstallationID)
	if err != nil {
		return vcs.Comparison{}, err
	}
	added, behindBy, err := c.compare(ctx, token, repo.ExternalID, from, to)
	if err != nil {
		return vcs.Comparison{}, err
	}
	removed := []vcs.Commit{}
	if behindBy > 0 {
		if removed, _, err = c.compare(ctx, token, repo.ExternalID, to, from); err != nil {
			return vcs.Comparison{}, err
		}
	}
	return vcs.Comparison{Added: added, Removed: removed}, nil
}

// compare returns the commits head gained since its merge base with base,
// and how many commits base has that head does not.
func (c *Client) compare(ctx context.Context, token, repositoryID, base, head string) ([]vcs.Commit, int, error) {
	next := c.apiURL + "/repositories/" + url.PathEscape(repositoryID) +
		"/compare/" + url.PathEscape(base+"..."+head) + "?per_page=100"
	commits := []vcs.Commit{}
	behindBy := 0
	for pages := 0; next != ""; pages++ {
		if pages == maxComparePages {
			return nil, 0, fmt.Errorf("%w: more than %d commits", vcs.ErrRangeTooLarge, maxComparePages*100)
		}
		var page compareResponse
		header, err := c.getWithHeader(ctx, token, next, &page)
		if err != nil {
			return nil, 0, err
		}
		behindBy = page.BehindBy
		commits = append(commits, page.commits()...)
		if next, err = c.next(header); err != nil {
			return nil, 0, err
		}
	}
	return commits, behindBy, nil
}

type compareResponse struct {
	BehindBy int `json:"behind_by"`
	Commits  []struct {
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
