import { Component, computed, inject, resource } from '@angular/core';
import { RouterLink } from '@angular/router';
import { NgIcon, provideIcons } from '@ng-icons/core';
import { lucideInfo, lucidePlus } from '@ng-icons/lucide';
import { HlmAlertImports } from '@spartan-ng/helm/alert';
import { HlmButtonImports } from '@spartan-ng/helm/button';
import { HlmSpinnerImports } from '@spartan-ng/helm/spinner';
import { ApiError } from '../../api/api-client';
import { Application, Environment } from '../../api/models';
import { DeliveryApi } from '../delivery-api';
import { PageHeader } from '../page-header';
import { WorkspaceContext } from '../workspace-context';

interface Row {
  application: Application;
  project: string;
  team: string;
  /** Per Environment, in column order: the names of the Targets the Application is placed on. */
  cells: string[][];
}

/**
 * Applications by Environment: which Targets each Application is placed on. Releases and
 * Deployments aren't tracked yet, so cells show placements rather than versions.
 */
@Component({
  selector: 'app-applications-page',
  imports: [RouterLink, NgIcon, PageHeader, HlmAlertImports, HlmButtonImports, HlmSpinnerImports],
  providers: [provideIcons({ lucideInfo, lucidePlus })],
  templateUrl: './applications-page.html',
})
export class ApplicationsPage {
  private readonly api = inject(DeliveryApi);
  protected readonly context = inject(WorkspaceContext);

  protected readonly overview = resource({
    params: () => this.context.current()?.id,
    loader: async ({ params: workspaceId }) => {
      const [applications, environments, projects, teams] = await Promise.all([
        this.api.applications(workspaceId),
        this.api.environments(workspaceId),
        this.api.projects(workspaceId),
        this.api.teams(),
      ]);
      const [targets, placements] = await Promise.all([
        this.api.targetsOf(environments),
        Promise.all(applications.map((a) => this.api.placements(a.id))),
      ]);
      const rows: Row[] = applications.map((application, i) => {
        const placed = placements[i].map((p) => targets.find((t) => t.id === p.target_id));
        return {
          application,
          project: projects.find((p) => p.id === application.project_id)?.display_name ?? '',
          team: teams.find((t) => t.id === application.owner_team_id)?.display_name ?? '',
          cells: environments.map((environment) =>
            placed.filter((t) => t?.environment_id === environment.id).map((t) => t!.display_name),
          ),
        };
      });
      return { environments, rows };
    },
  });

  protected readonly environments = computed<Environment[]>(
    () => this.overview.value()?.environments ?? [],
  );
  protected readonly rows = computed<Row[]>(() => this.overview.value()?.rows ?? []);
  protected readonly failure = computed(() => {
    const error = this.overview.error();
    return error ? (error instanceof ApiError ? error.message : 'Something went wrong.') : null;
  });
}
