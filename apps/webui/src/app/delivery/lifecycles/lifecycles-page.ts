import { Component, computed, inject, resource, signal } from '@angular/core';
import { RouterLink } from '@angular/router';
import { NgIcon, provideIcons } from '@ng-icons/core';
import { lucideArrowRight, lucidePlus } from '@ng-icons/lucide';
import { HlmAlertImports } from '@spartan-ng/helm/alert';
import { HlmBadgeImports } from '@spartan-ng/helm/badge';
import { HlmButtonImports } from '@spartan-ng/helm/button';
import { HlmSpinnerImports } from '@spartan-ng/helm/spinner';
import { ApiError } from '../../api/api-client';
import { PermissionsApi } from '../../api/permissions-api';
import { DeliveryApi } from '../delivery-api';
import { PageHeader } from '../page-header';
import { WorkspaceContext } from '../workspace-context';
import { LifecyclesApi } from './lifecycles-api';

/** The Workspace's Lifecycles: the ordered phases Releases move through. */
@Component({
  selector: 'app-lifecycles-page',
  imports: [
    NgIcon,
    RouterLink,
    PageHeader,
    HlmAlertImports,
    HlmBadgeImports,
    HlmButtonImports,
    HlmSpinnerImports,
  ],
  providers: [provideIcons({ lucideArrowRight, lucidePlus })],
  template: `
    <div class="flex flex-col gap-8">
      <app-page-header
        heading="Lifecycles"
        [trail]="(context.current()?.display_name ?? 'Workspace') + ' / Delivery'"
      >
        @if (canManage()) {
          <ng-container pageActions>
            <a hlmBtn size="lg" routerLink="/lifecycles/new">
              <ng-icon name="lucidePlus" aria-hidden="true" /> New Lifecycle
            </a>
          </ng-container>
        }
      </app-page-header>
      <p class="text-foreground max-w-205 text-base leading-relaxed">
        A phase opens for a Release once every required phase before it has a healthy Deployment.
        Channels choose which Lifecycle their Releases follow.
      </p>
      <label class="flex items-center gap-2 text-sm">
        <input
          type="checkbox"
          class="accent-primary size-4"
          [checked]="includeArchived()"
          (change)="includeArchived.set($any($event.target).checked)"
        />
        Show archived Lifecycles
      </label>

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
                <h2 class="text-heading flex flex-wrap items-center gap-2 text-lg font-semibold">
                  <a class="text-heading" [routerLink]="['/lifecycles', lifecycle.id]">{{
                    lifecycle.name
                  }}</a>
                  @if (lifecycle.isDefault) {
                    <span hlmBadge>Default</span>
                  }
                  @if (lifecycle.archived) {
                    <span hlmBadge variant="outline">Archived</span>
                  }
                </h2>
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
  private readonly api = inject(LifecyclesApi);
  private readonly delivery = inject(DeliveryApi);
  private readonly permissions = inject(PermissionsApi);
  protected readonly context = inject(WorkspaceContext);

  protected readonly includeArchived = signal(false);

  protected readonly data = resource({
    params: () => {
      const workspaceId = this.context.current()?.id;
      return workspaceId ? { workspaceId, includeArchived: this.includeArchived() } : undefined;
    },
    loader: async ({ params: { workspaceId, includeArchived } }) => {
      const [lifecycles, environments, granted, setting] = await Promise.all([
        this.api.list(workspaceId, includeArchived),
        this.delivery.environments(workspaceId, true),
        this.permissions.granted(workspaceId, ['lifecycle.manage']).catch(() => new Set<string>()),
        this.api.workspaceDefault(workspaceId).catch(() => null),
      ]);
      return {
        canManage: granted.has('lifecycle.manage'),
        lifecycles: lifecycles.map((lifecycle) => ({
          id: lifecycle.id,
          name: lifecycle.display_name,
          description: lifecycle.description,
          archived: !!lifecycle.deleted_at,
          isDefault: setting?.value === lifecycle.id,
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
        })),
      };
    },
  });

  private readonly loaded = computed(() => (this.data.hasValue() ? this.data.value() : undefined));
  protected readonly lifecycles = computed(() => this.loaded()?.lifecycles ?? []);
  protected readonly canManage = computed(() => this.loaded()?.canManage ?? false);
  protected readonly failure = computed(() => {
    const error = this.data.error();
    return error ? (error instanceof ApiError ? error.message : 'Something went wrong.') : null;
  });
}
