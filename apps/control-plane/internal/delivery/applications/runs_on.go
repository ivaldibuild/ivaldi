package applications

import (
	"context"
	"fmt"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/delivery"
	"github.com/ivaldibuild/ivaldi/pkg/identity"
	"github.com/ivaldibuild/ivaldi/pkg/pagination"
)

// RunsOn reads where a Workspace's Applications run, page by Application.
// The placements come from one read over application_targets and what is
// current there; Project and Team names are composed through their owning
// capabilities, as ADR 0005 requires.
func (s *service) RunsOn(ctx context.Context, _ identity.Actor, query delivery.RunsOnQuery) (*delivery.RunsOnPage, error) {
	if query.WorkspaceID == "" {
		return nil, delivery.NewErrInvalidInput("reading where Applications run needs a workspace")
	}
	applications, err := s.applications.ListApplications(ctx, delivery.ListApplicationsQuery{
		OrganizationID:  query.OrganizationID,
		WorkspaceID:     query.WorkspaceID,
		ProjectID:       query.ProjectID,
		OwnerTeamID:     query.OwnerTeamID,
		IncludeArchived: query.IncludeArchived,
		Page:            query.Page,
	})
	if err != nil {
		return nil, fmt.Errorf("list applications: %w", err)
	}
	environments, err := s.environments.ListActiveEnvironments(ctx, query.OrganizationID, query.WorkspaceID)
	if err != nil {
		return nil, fmt.Errorf("list active environments: %w", err)
	}
	page := &delivery.RunsOnPage{
		Applications: &pagination.Result[*delivery.ApplicationRunsOn]{
			Items:          []*delivery.ApplicationRunsOn{},
			TotalCount:     applications.TotalCount,
			HasNext:        applications.HasNext,
			HasPrevious:    applications.HasPrevious,
			NextCursor:     applications.NextCursor,
			PreviousCursor: applications.PreviousCursor,
		},
		Environments: environments,
	}
	if len(applications.Items) == 0 {
		return page, nil
	}

	applicationIDs := make([]string, 0, len(applications.Items))
	projectIDs := make([]string, 0, len(applications.Items))
	teamIDs := make([]string, 0, len(applications.Items)*2)
	seenProjects := make(map[string]bool, len(applications.Items))
	seenTeams := make(map[string]bool, len(applications.Items)*2)
	for _, app := range applications.Items {
		applicationIDs = append(applicationIDs, app.ID())
		if !seenProjects[app.ProjectID()] {
			seenProjects[app.ProjectID()] = true
			projectIDs = append(projectIDs, app.ProjectID())
		}
		if !seenTeams[app.OwnerTeamID()] {
			seenTeams[app.OwnerTeamID()] = true
			teamIDs = append(teamIDs, app.OwnerTeamID())
		}
	}

	projects, err := s.projects.LookupProjects(ctx, query.OrganizationID, projectIDs)
	if err != nil {
		return nil, fmt.Errorf("look up projects: %w", err)
	}
	projectOwners := make(map[string]string, len(projects))
	projectNames := make(map[string]string, len(projects))
	for _, project := range projects {
		projectOwners[project.ID()] = project.OwnerTeamID()
		projectNames[project.ID()] = project.DisplayName()
		if !seenTeams[project.OwnerTeamID()] {
			seenTeams[project.OwnerTeamID()] = true
			teamIDs = append(teamIDs, project.OwnerTeamID())
		}
	}
	teams, err := s.teams.LookupTeams(ctx, query.OrganizationID, teamIDs)
	if err != nil {
		return nil, fmt.Errorf("look up teams: %w", err)
	}
	targets, err := s.applications.ListRunsOn(ctx, query.OrganizationID, applicationIDs)
	if err != nil {
		return nil, fmt.Errorf("list placements: %w", err)
	}
	byApplication := make(map[string][]delivery.PlacementRunsOn, len(applicationIDs))
	for _, target := range targets {
		byApplication[target.ApplicationID] = append(byApplication[target.ApplicationID], target)
	}

	rows := make([]*delivery.ApplicationRunsOn, 0, len(applications.Items))
	for _, app := range applications.Items {
		projectOwnerID := projectOwners[app.ProjectID()]
		rows = append(rows, &delivery.ApplicationRunsOn{
			Application:          app,
			ProjectName:          projectNames[app.ProjectID()],
			ProjectOwnerTeamID:   projectOwnerID,
			ProjectOwnerTeamName: teams[projectOwnerID].DisplayName,
			OwnerTeamName:        teams[app.OwnerTeamID()].DisplayName,
			Targets:              byApplication[app.ID()],
		})
	}
	page.Applications.Items = rows
	return page, nil
}
