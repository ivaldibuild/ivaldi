package applications

import (
	"context"
	"errors"
	"fmt"

	"golang.org/x/sync/errgroup"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/delivery"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/team"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/workspace"
	"github.com/ivaldibuild/ivaldi/pkg/identity"
	"github.com/ivaldibuild/ivaldi/pkg/pagination"
)

// RunsOn reads where a Workspace's Applications run, page by Application.
// The placements come from one read over application_targets and what is
// current there; Project and Team names are composed through their owning
// capabilities, as ADR 0005 requires.
//
// Reads that do not depend on each other run concurrently, so RunsOn must not
// be called with a transaction in the context: a transaction's connection
// cannot serve several queries at once.
func (s *service) RunsOn(ctx context.Context, _ identity.Actor, query delivery.RunsOnQuery) (*delivery.RunsOnPage, error) {
	if query.WorkspaceID == "" {
		return nil, delivery.NewErrInvalidInput("reading where Applications run needs a workspace")
	}

	var (
		applications *pagination.Result[*delivery.Application]
		environments []*delivery.Environment
	)
	g, gctx := errgroup.WithContext(ctx)
	g.Go(func() error { return s.checkRunsOnScope(gctx, query) })
	g.Go(func() (err error) {
		applications, err = s.applications.ListApplications(gctx, delivery.ListApplicationsQuery{
			OrganizationID:  query.OrganizationID,
			WorkspaceID:     query.WorkspaceID,
			ProjectID:       query.ProjectID,
			OwnerTeamID:     query.OwnerTeamID,
			IncludeArchived: query.IncludeArchived,
			Page:            query.Page,
		})
		if err != nil {
			return fmt.Errorf("list applications: %w", err)
		}
		return nil
	})
	g.Go(func() (err error) {
		environments, err = s.environments.ListActiveEnvironments(gctx, query.OrganizationID, query.WorkspaceID)
		if err != nil {
			return fmt.Errorf("list active environments: %w", err)
		}
		return nil
	})
	if err := g.Wait(); err != nil {
		return nil, err
	}

	labels, err := s.runsOnLabels(ctx, query.OrganizationID, applications.Items)
	if err != nil {
		return nil, err
	}
	rows := snapshots(applications, func(app *delivery.Application) *delivery.ApplicationRunsOn {
		projectOwnerID := labels.projectOwners[app.ProjectID()]
		return &delivery.ApplicationRunsOn{
			Application:          app,
			ProjectName:          labels.projectNames[app.ProjectID()],
			ProjectOwnerTeamID:   projectOwnerID,
			ProjectOwnerTeamName: labels.teams[projectOwnerID].DisplayName,
			OwnerTeamName:        labels.teams[app.OwnerTeamID()].DisplayName,
			Targets:              labels.targets[app.ID()],
		}
	})
	return &delivery.RunsOnPage{Applications: &rows, Environments: environments}, nil
}

// runsOnLabels is what a page of Applications is labelled with: their
// Projects' names and owners, Team names, and placements.
type runsOnLabels struct {
	projectNames  map[string]string
	projectOwners map[string]string
	teams         map[string]team.TeamName
	targets       map[string][]delivery.PlacementRunsOn
}

func (s *service) runsOnLabels(ctx context.Context, organizationID string, applications []*delivery.Application) (runsOnLabels, error) {
	labels := runsOnLabels{
		projectNames:  map[string]string{},
		projectOwners: map[string]string{},
		teams:         map[string]team.TeamName{},
		targets:       map[string][]delivery.PlacementRunsOn{},
	}
	if len(applications) == 0 {
		return labels, nil
	}
	applicationIDs := make([]string, 0, len(applications))
	projectIDs := make([]string, 0, len(applications))
	seenProjects := make(map[string]bool, len(applications))
	for _, app := range applications {
		applicationIDs = append(applicationIDs, app.ID())
		if !seenProjects[app.ProjectID()] {
			seenProjects[app.ProjectID()] = true
			projectIDs = append(projectIDs, app.ProjectID())
		}
	}

	var (
		projects []*workspace.Project
		targets  []delivery.PlacementRunsOn
	)
	g, gctx := errgroup.WithContext(ctx)
	g.Go(func() (err error) {
		projects, err = s.projects.LookupProjects(gctx, organizationID, projectIDs)
		if err != nil {
			return fmt.Errorf("look up projects: %w", err)
		}
		return nil
	})
	g.Go(func() (err error) {
		targets, err = s.applications.ListRunsOn(gctx, organizationID, applicationIDs)
		if err != nil {
			return fmt.Errorf("list placements: %w", err)
		}
		return nil
	})
	if err := g.Wait(); err != nil {
		return runsOnLabels{}, err
	}
	for _, target := range targets {
		labels.targets[target.ApplicationID] = append(labels.targets[target.ApplicationID], target)
	}

	teamIDs := make([]string, 0, len(applications)+len(projects))
	seenTeams := make(map[string]bool, cap(teamIDs))
	addTeam := func(id string) {
		if !seenTeams[id] {
			seenTeams[id] = true
			teamIDs = append(teamIDs, id)
		}
	}
	for _, app := range applications {
		addTeam(app.OwnerTeamID())
	}
	for _, project := range projects {
		labels.projectOwners[project.ID()] = project.OwnerTeamID()
		labels.projectNames[project.ID()] = project.DisplayName()
		addTeam(project.OwnerTeamID())
	}
	teams, err := s.teams.LookupTeams(ctx, organizationID, teamIDs)
	if err != nil {
		return runsOnLabels{}, fmt.Errorf("look up teams: %w", err)
	}
	labels.teams = teams
	return labels, nil
}

// checkRunsOnScope checks that the Workspace exists and, when the read is
// narrowed to a Project, that the Project is in it. The authorizer checks
// the Project alone, so without this a Project in one Workspace would
// unlock another Workspace's Environments.
func (s *service) checkRunsOnScope(ctx context.Context, query delivery.RunsOnQuery) error {
	if query.ProjectID == "" {
		if _, err := s.workspaces.LookupWorkspace(ctx, query.OrganizationID, query.WorkspaceID); err != nil {
			if errors.Is(err, workspace.ErrNotFound) {
				return fmt.Errorf("%w: workspace %s", delivery.ErrNotFound, query.WorkspaceID)
			}
			return fmt.Errorf("look up workspace %s: %w", query.WorkspaceID, err)
		}
		return nil
	}
	// A Project's Workspace exists for as long as the Project does.
	p, err := s.workspaces.LookupProject(ctx, query.OrganizationID, query.ProjectID)
	if err != nil && !errors.Is(err, workspace.ErrNotFound) {
		return fmt.Errorf("look up project %s: %w", query.ProjectID, err)
	}
	if err != nil || p.WorkspaceID() != query.WorkspaceID {
		return fmt.Errorf("%w: project %s in workspace %s", delivery.ErrNotFound, query.ProjectID, query.WorkspaceID)
	}
	return nil
}
