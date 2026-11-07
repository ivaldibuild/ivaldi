import { Component, computed, inject, resource } from '@angular/core';
import { RouterLink } from '@angular/router';
import { NgIcon, provideIcons } from '@ng-icons/core';
import { lucideInfo, lucidePlus } from '@ng-icons/lucide';
import { HlmAlertImports } from '@spartan-ng/helm/alert';
import { HlmButtonImports } from '@spartan-ng/helm/button';
import { HlmSpinnerImports } from '@spartan-ng/helm/spinner';
import { ApiError } from '../../api/api-client';
import { Environment } from '../../api/models';
import { ApplicationGrid, GridGroup, placementRows } from '../application-grid';
import { DeliveryApi } from '../delivery-api';
import { PageHeader } from '../page-header';
import { ProjectsApi } from '../projects-api';
import { WorkspaceContext } from '../workspace-context';

/**
 * The Workspace's Applications under their Projects, by Environment: which Targets each is
 * placed on. Deployments aren't tracked yet, so cells show placements rather than versions.
 */
@Component({
  selector: 'app-applications-page',
  imports: [
    RouterLink,
    NgIcon,
    ApplicationGrid,
    PageHeader,
    HlmAlertImports,
    HlmButtonImports,
    HlmSpinnerImports,
  ],
  providers: [provideIcons({ lucideInfo, lucidePlus })],
  templateUrl: './applications-page.html',
})
export class ApplicationsPage {
  private readonly api = inject(DeliveryApi);
  private readonly projectsApi = inject(ProjectsApi);
  protected readonly context = inject(WorkspaceContext);

  protected readonly overview = resource({
    params: () => this.context.current()?.id,
    loader: async ({ params: workspaceId }) => {
      const [applications, environments, listed, teams] = await Promise.all([
        this.api.applications(workspaceId),
        this.api.environments(workspaceId),
        this.api.projects(workspaceId),
        this.api.teams(),
      ]);
      // The list holds the first 100 Projects; fetch any other that has an Application here.
      const missing = [...new Set(applications.map((a) => a.project_id))].filter(
        (id) => !listed.some((p) => p.id === id),
      );
      const projects = [
        ...listed,
        ...(await Promise.all(missing.map((id) => this.projectsApi.project(id)))),
      ];
      const rows = await placementRows(this.api, environments, applications, teams, projects);
      const groups: GridGroup[] = projects.map((project) => ({
        project,
        team: teams.find((t) => t.id === project.owner_team_id)?.display_name ?? '',
        rows: rows.filter((r) => r.application.project_id === project.id),
      }));
      return { environments, groups, applications: applications.length };
    },
  });

  protected readonly environments = computed<Environment[]>(
    () => this.overview.value()?.environments ?? [],
  );
  protected readonly groups = computed<GridGroup[]>(() => this.overview.value()?.groups ?? []);
  protected readonly failure = computed(() => {
    const error = this.overview.error();
    return error ? (error instanceof ApiError ? error.message : 'Something went wrong.') : null;
  });
}
