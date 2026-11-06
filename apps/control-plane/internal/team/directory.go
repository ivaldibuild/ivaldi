package team

import (
	"context"
	"errors"
)

// Directory answers questions about Teams. Capabilities that record an
// owning Team check it here before storing it. It does not authorize: the
// calling capability has already authorized its own operation.
type Directory interface {
	// IsActiveTeam reports whether teamID is an active Team in the Organization.
	IsActiveTeam(ctx context.Context, organizationID, teamID string) (bool, error)
}

// NewDirectory returns a Directory that reads through repo.
func NewDirectory(repo TeamRepository) Directory {
	return directory{repo: repo}
}

type directory struct {
	repo TeamRepository
}

func (d directory) IsActiveTeam(ctx context.Context, organizationID, teamID string) (bool, error) {
	t, err := d.repo.GetTeam(ctx, organizationID, teamID)
	if errors.Is(err, ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return !t.Archived(), nil
}
