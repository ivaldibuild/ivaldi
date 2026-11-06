import { Component, computed, inject, resource } from '@angular/core';
import { RouterLink } from '@angular/router';
import { HlmAlertImports } from '@spartan-ng/helm/alert';
import { HlmBadgeImports } from '@spartan-ng/helm/badge';
import { HlmButtonImports } from '@spartan-ng/helm/button';
import { HlmSpinnerImports } from '@spartan-ng/helm/spinner';
import { ApiError } from '../../api/api-client';
import { Tier } from '../../api/models';
import { DeliveryApi } from '../delivery-api';
import { PageHeader } from '../page-header';
import { WorkspaceContext } from '../workspace-context';

const tiers: Record<Tier, string> = {
  development: 'Development',
  staging: 'Staging',
  production: 'Production',
  other: 'Other',
};

/** The Workspace's Environments and the Targets each runs on. */
@Component({
  selector: 'app-environments-page',
  imports: [
    RouterLink,
    PageHeader,
    HlmAlertImports,
    HlmBadgeImports,
    HlmButtonImports,
    HlmSpinnerImports,
  ],
  template: `
    <div class="flex flex-col gap-8">
      <app-page-header
        heading="Environments"
        [trail]="(context.current()?.display_name ?? 'Workspace') + ' / Delivery'"
      >
        <ng-container pageActions>
          <a hlmBtn variant="outline" routerLink="/setup/environments">Set up Environments</a>
        </ng-container>
      </app-page-header>

      @if (failure(); as message) {
        <div hlmAlert variant="destructive" role="alert">
          <h2 hlmAlertTitle>Environments didn't load</h2>
          <p hlmAlertDescription>{{ message }}</p>
        </div>
      } @else if (data.isLoading() && !data.hasValue()) {
        <p class="text-muted-foreground flex items-center gap-3 text-sm" role="status">
          <hlm-spinner aria-hidden="true" /> Loading Environments…
        </p>
      } @else if (environments().length === 0) {
        <p class="text-foreground text-sm">This Workspace has no Environments yet.</p>
      } @else {
        <ul class="flex flex-col gap-4">
          @for (item of environments(); track item.environment.id) {
            <li class="border-border flex flex-col gap-4 border p-5">
              <div class="flex flex-wrap items-center gap-3">
                <h2 class="text-heading text-lg font-semibold">
                  {{ item.environment.display_name }}
                </h2>
                <span hlmBadge variant="outline">{{ tierLabel(item.environment.tier) }}</span>
                <span class="text-muted-foreground text-sm">
                  {{ item.environment.execution_enabled ? 'Ivaldi deploys here' : 'Tracked only' }}
                </span>
              </div>
              @if (item.targets.length > 0) {
                <ul class="border-border flex flex-col border">
                  @for (target of item.targets; track target.id) {
                    <li
                      class="border-border flex flex-wrap justify-between gap-2 border-b px-4 py-3 text-sm last:border-b-0"
                    >
                      <span>
                        <span class="text-heading font-semibold">{{ target.display_name }}</span>
                        <span class="text-muted-foreground">
                          · {{ target.locator['cluster'] }} / {{ target.locator['namespace'] }}
                        </span>
                      </span>
                      <span class="text-muted-foreground">
                        {{ target.agent_principal_id ? 'Agent assigned' : 'No agent yet' }}
                      </span>
                    </li>
                  }
                </ul>
              } @else {
                <p class="text-muted-foreground text-sm">No Targets yet.</p>
              }
            </li>
          }
        </ul>
      }
    </div>
  `,
})
export class EnvironmentsPage {
  private readonly api = inject(DeliveryApi);
  protected readonly context = inject(WorkspaceContext);

  protected readonly data = resource({
    params: () => this.context.current()?.id,
    loader: async ({ params: workspaceId }) => {
      const environments = await this.api.environments(workspaceId);
      const targets = await this.api.targetsOf(environments);
      return environments.map((environment) => ({
        environment,
        targets: targets.filter((t) => t.environment_id === environment.id),
      }));
    },
  });

  protected readonly environments = computed(() => this.data.value() ?? []);
  protected readonly failure = computed(() => {
    const error = this.data.error();
    return error ? (error instanceof ApiError ? error.message : 'Something went wrong.') : null;
  });

  protected tierLabel(tier: Tier): string {
    return tiers[tier] ?? tier;
  }
}
