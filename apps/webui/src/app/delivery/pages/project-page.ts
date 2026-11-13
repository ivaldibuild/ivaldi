import { Component, computed, inject, input, resource } from '@angular/core';
import { Router, RouterLink } from '@angular/router';
import { NgIcon, provideIcons } from '@ng-icons/core';
import { lucideArchive, lucidePencil, lucidePlus } from '@ng-icons/lucide';
import { HlmAlertImports } from '@spartan-ng/helm/alert';
import { HlmBadgeImports } from '@spartan-ng/helm/badge';
import { HlmButtonImports } from '@spartan-ng/helm/button';
import { HlmSpinnerImports } from '@spartan-ng/helm/spinner';
import { ApiError } from '../../api/api-client';
import { TeamsApi } from '../../api/teams-api';
import { RequestStatus } from '../../shared/request-status';
import { ApplicationGrid, GridGroup, runsOnGroups } from '../application-grid';
import { DeliveryApi } from '../delivery-api';
import { PageHeader } from '../page-header';
import { ProjectSources } from '../project-sources';
import { ProjectsApi } from '../projects-api';

/** One Project: its Applications and where each is placed. Editing it is a page of its own. */
@Component({
  selector: 'app-project-page',
  imports: [
    NgIcon,
    RouterLink,
    ApplicationGrid,
    PageHeader,
    ProjectSources,
    HlmAlertImports,
    HlmBadgeImports,
    HlmButtonImports,
    HlmSpinnerImports,
  ],
  providers: [provideIcons({ lucideArchive, lucidePencil, lucidePlus })],
  templateUrl: './project-page.html',
})
export class ProjectPage {
  private readonly projects = inject(ProjectsApi);
  private readonly delivery = inject(DeliveryApi);
  private readonly teams = inject(TeamsApi);
  private readonly router = inject(Router);

  /** The `:id` route parameter. */
  readonly id = input.required<string>();

  protected readonly details = resource({
    params: () => this.id(),
    loader: async ({ params: id }) => {
      const project = await this.projects.project(id);
      const page = await this.delivery.runsOn(project.workspace_id, { projectId: id });
      return {
        project,
        team: page.items[0]?.project.owner_team.display_name ?? (await this.teamName(project)),
        environments: page.environments,
        groups: runsOnGroups(page),
      };
    },
  });

  /**
   * The loaded value, or undefined while there is none: a resource's value() throws while it's in
   * an error state, which would stop the page from rendering its error.
   */
  private readonly loaded = computed(() =>
    this.details.hasValue() ? this.details.value() : undefined,
  );

  protected readonly project = computed(() => this.loaded()?.project ?? null);
  protected readonly archived = computed(() => !!this.project()?.archived_at);
  protected readonly team = computed(() => this.loaded()?.team ?? '');
  protected readonly environments = computed(() => this.loaded()?.environments ?? []);
  protected readonly groups = computed<GridGroup[]>(() => this.loaded()?.groups ?? []);
  protected readonly hasApplications = computed(
    () => (this.loaded()?.groups.flatMap((group) => group.rows).length ?? 0) > 0,
  );

  protected readonly failure = computed(() => {
    const error = this.details.error();
    return error ? (error instanceof ApiError ? error.message : 'Something went wrong.') : null;
  });

  /**
   * The owning Team's name, for a Project with no Applications to carry it. A Team that can't be
   * read leaves the name out rather than the page.
   */
  private async teamName(project: { owner_team_id: string }): Promise<string> {
    try {
      return (await this.teams.get(project.owner_team_id)).display_name;
    } catch {
      return '';
    }
  }

  protected readonly restoreStatus = new RequestStatus();

  protected async restore(): Promise<void> {
    if (await this.restoreStatus.run(() => this.projects.restore(this.id()))) {
      this.details.reload();
    }
  }

  protected newApplication(): void {
    void this.router.navigate(['/applications/new'], { queryParams: { project: this.id() } });
  }
}
