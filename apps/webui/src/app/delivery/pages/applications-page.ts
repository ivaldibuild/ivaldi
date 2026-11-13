import { Component, computed, inject, resource } from '@angular/core';
import { RouterLink } from '@angular/router';
import { NgIcon, provideIcons } from '@ng-icons/core';
import { lucidePlus } from '@ng-icons/lucide';
import { HlmAlertImports } from '@spartan-ng/helm/alert';
import { HlmButtonImports } from '@spartan-ng/helm/button';
import { HlmSpinnerImports } from '@spartan-ng/helm/spinner';
import { ApiError } from '../../api/api-client';
import { Environment } from '../../api/models';
import { ApplicationGrid, GridGroup, runsOnGroups } from '../application-grid';
import { DeliveryApi } from '../delivery-api';
import { PageHeader } from '../page-header';
import { WorkspaceContext } from '../workspace-context';

/**
 * The Workspace's Applications under their Projects, by Environment: which Targets each is
 * placed on, and the version and Deployment status current there.
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
  providers: [provideIcons({ lucidePlus })],
  templateUrl: './applications-page.html',
})
export class ApplicationsPage {
  private readonly api = inject(DeliveryApi);
  protected readonly context = inject(WorkspaceContext);

  protected readonly overview = resource({
    params: () => this.context.current()?.id,
    loader: async ({ params: workspaceId }) => {
      const page = await this.api.runsOn(workspaceId);
      return { environments: page.environments, groups: runsOnGroups(page) };
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
