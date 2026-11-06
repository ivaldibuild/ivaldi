import { Component, computed, inject, resource } from '@angular/core';
import { NgIcon, provideIcons } from '@ng-icons/core';
import { lucideArrowRight } from '@ng-icons/lucide';
import { HlmAlertImports } from '@spartan-ng/helm/alert';
import { HlmBadgeImports } from '@spartan-ng/helm/badge';
import { HlmSpinnerImports } from '@spartan-ng/helm/spinner';
import { ApiError } from '../../api/api-client';
import { DeliveryApi } from '../delivery-api';
import { PageHeader } from '../page-header';
import { WorkspaceContext } from '../workspace-context';

/** The Workspace's Lifecycles: the ordered phases Releases move through. */
@Component({
  selector: 'app-lifecycles-page',
  imports: [NgIcon, PageHeader, HlmAlertImports, HlmBadgeImports, HlmSpinnerImports],
  providers: [provideIcons({ lucideArrowRight })],
  template: `
    <div class="flex flex-col gap-8">
      <app-page-header
        heading="Lifecycles"
        [trail]="(context.current()?.display_name ?? 'Workspace') + ' / Delivery'"
      />
      <p class="text-foreground max-w-205 text-base leading-relaxed">
        A phase opens for a Release once every required phase before it has a healthy Deployment.
        Channels choose which Lifecycle their Releases follow.
      </p>

      @if (failure(); as message) {
        <div hlmAlert variant="destructive" role="alert">
          <h2 hlmAlertTitle>Lifecycles didn't load</h2>
          <p hlmAlertDescription>{{ message }}</p>
        </div>
      } @else if (data.isLoading() && !data.hasValue()) {
        <p class="text-muted-foreground flex items-center gap-3 text-sm" role="status">
          <hlm-spinner aria-hidden="true" /> Loading Lifecycles…
        </p>
      } @else if (lifecycles().length === 0) {
        <p class="text-foreground text-sm">This Workspace has no Lifecycles yet.</p>
      } @else {
        <ul class="flex flex-col gap-4">
          @for (lifecycle of lifecycles(); track lifecycle.id) {
            <li class="border-border flex flex-col gap-4 border p-5">
              <div class="flex flex-col gap-1">
                <h2 class="text-heading text-lg font-semibold">{{ lifecycle.name }}</h2>
                @if (lifecycle.description) {
                  <p class="text-muted-foreground text-sm">{{ lifecycle.description }}</p>
                }
              </div>
              <ol
                class="flex flex-wrap items-center gap-2 text-sm"
                [attr.aria-label]="lifecycle.name + ' phases'"
              >
                @for (phase of lifecycle.phases; track $index; let last = $last; let i = $index) {
                  <li class="border-input flex flex-col gap-1 border px-4 py-3">
                    <span
                      class="text-muted-foreground text-xs font-semibold tracking-[0.24em] uppercase"
                    >
                      Phase {{ i + 1 }}
                    </span>
                    <span class="text-heading font-semibold">{{ phase.name }}</span>
                    @if (phase.environments && phase.environments !== phase.name) {
                      <span class="text-muted-foreground text-[13px]">{{
                        phase.environments
                      }}</span>
                    }
                    @if (phase.optional) {
                      <span hlmBadge variant="outline" class="self-start">Optional</span>
                    }
                  </li>
                  @if (!last) {
                    <li aria-hidden="true" class="text-muted-foreground">
                      <ng-icon name="lucideArrowRight" />
                    </li>
                  }
                }
              </ol>
            </li>
          }
        </ul>
      }
    </div>
  `,
})
export class LifecyclesPage {
  private readonly api = inject(DeliveryApi);
  protected readonly context = inject(WorkspaceContext);

  protected readonly data = resource({
    params: () => this.context.current()?.id,
    loader: async ({ params: workspaceId }) => {
      const [lifecycles, environments] = await Promise.all([
        this.api.lifecycles(workspaceId),
        this.api.environments(workspaceId),
      ]);
      return lifecycles.map((lifecycle) => ({
        id: lifecycle.id,
        name: lifecycle.display_name,
        description: lifecycle.description,
        phases: [...lifecycle.phases]
          .sort((a, b) => a.position - b.position)
          .map((phase) => ({
            name: phase.name,
            optional: phase.optional ?? false,
            environments: phase.environment_ids
              .map((id) => environments.find((e) => e.id === id)?.display_name)
              .filter(Boolean)
              .join(', '),
          })),
      }));
    },
  });

  protected readonly lifecycles = computed(() => this.data.value() ?? []);
  protected readonly failure = computed(() => {
    const error = this.data.error();
    return error ? (error instanceof ApiError ? error.message : 'Something went wrong.') : null;
  });
}
