import {
  Component,
  computed,
  DestroyRef,
  effect,
  inject,
  input,
  resource,
  signal,
} from '@angular/core';
import { RouterLink } from '@angular/router';
import { NgIcon, provideIcons } from '@ng-icons/core';
import { lucideClock } from '@ng-icons/lucide';
import { HlmAlertImports } from '@spartan-ng/helm/alert';
import { HlmButtonImports } from '@spartan-ng/helm/button';
import { HlmSpinnerImports } from '@spartan-ng/helm/spinner';
import { ApiError } from '../../api/api-client';
import { DeploymentEvent, DeploymentStatus } from '../../api/models';
import { formatDayAndTime } from '../../shared/dates';
import { RequestStatus } from '../../shared/request-status';
import { DeliveryApi } from '../delivery-api';
import { PageHeader } from '../page-header';
import { ProjectsApi } from '../projects-api';
import {
  cancellable,
  DeploymentStatusBadge,
  deploymentStatusLabels,
  findingLabel,
  inFlight,
} from './deployment-status';

/** How often a Deployment that is still moving is looked at again. */
export const refreshMs = 5000;

/**
 * One Deployment: the Release and Target, its status and timeline as the runtime agent reports
 * it, and cancelling it before it starts applying. While it is still moving the page looks again
 * every few seconds.
 */
@Component({
  selector: 'app-deployment-page',
  imports: [
    NgIcon,
    RouterLink,
    PageHeader,
    DeploymentStatusBadge,
    HlmAlertImports,
    HlmButtonImports,
    HlmSpinnerImports,
  ],
  providers: [provideIcons({ lucideClock })],
  templateUrl: './deployment-page.html',
})
export class DeploymentPage {
  private readonly api = inject(DeliveryApi);
  private readonly projects = inject(ProjectsApi);

  /** The `:id` route parameter. */
  readonly id = input.required<string>();

  protected readonly details = resource({
    params: () => this.id(),
    loader: async ({ params: id }) => {
      const detail = await this.api.deployment(id);
      const d = detail.deployment;
      const [release, application, environment, targets] = await Promise.all([
        this.api.release(d.release_id),
        this.api.application(d.application_id),
        this.api.environment(d.environment_id),
        this.api.targets(d.environment_id, true),
      ]);
      const project = await this.projects.project(application.project_id);
      return {
        ...detail,
        release,
        application,
        environment,
        project,
        target: targets.find((t) => t.id === d.target_id) ?? null,
      };
    },
  });

  /**
   * The loaded value, kept while a refresh is under way so the page doesn't flicker; undefined
   * only before the first load or after a failure.
   */
  private readonly loaded = computed(() =>
    this.details.hasValue() ? this.details.value() : undefined,
  );

  protected readonly deployment = computed(() => this.loaded()?.deployment ?? null);
  protected readonly findingLabel = findingLabel;
  protected readonly release = computed(() => this.loaded()?.release ?? null);
  protected readonly application = computed(() => this.loaded()?.application ?? null);
  protected readonly project = computed(() => this.loaded()?.project ?? null);
  protected readonly environment = computed(() => this.loaded()?.environment ?? null);
  protected readonly targetName = computed(
    () => this.loaded()?.target?.display_name ?? this.deployment()?.target_id ?? '',
  );

  protected readonly heading = computed(() => {
    const d = this.loaded();
    return d ? `${d.release.version} to ${this.targetName()}` : '';
  });

  protected readonly moving = computed(() => {
    const d = this.deployment();
    return !!d && inFlight(d.status);
  });
  protected readonly canCancel = computed(() => {
    const d = this.deployment();
    return !!d && cancellable(d.status);
  });

  /** The timeline, newest first. */
  protected readonly timeline = computed(() =>
    [...(this.loaded()?.events ?? [])]
      .sort((a, b) => b.occurred_at.localeCompare(a.occurred_at))
      .map((event) => ({
        id: event.id,
        text: describe(event),
        at: formatDayAndTime(event.occurred_at),
      })),
  );

  protected readonly facts = computed(() => {
    const d = this.deployment();
    if (!d) {
      return [];
    }
    const facts = [
      { label: 'Requested', value: formatDayAndTime(d.requested_at) },
      { label: 'Started', value: d.started_at ? formatDayAndTime(d.started_at) : '—' },
      { label: 'Finished', value: d.finished_at ? formatDayAndTime(d.finished_at) : '—' },
      { label: 'Kind', value: reasons[d.reason] },
      {
        label: 'How',
        value:
          d.source === 'observed'
            ? 'Observed: applied by another tool and reported by the runtime agent'
            : 'Applied by Ivaldi’s runtime agent',
      },
    ];
    if (d.session_id) {
      facts.push({ label: 'Agent Session', value: d.session_id });
    }
    return facts;
  });

  protected readonly failure = computed(() => {
    const error = this.details.error();
    return error ? (error instanceof ApiError ? error.message : 'Something went wrong.') : null;
  });

  constructor() {
    // Look again while the Deployment is still moving.
    let timer: ReturnType<typeof setTimeout> | undefined;
    effect(() => {
      clearTimeout(timer);
      if (this.moving() && !this.details.isLoading()) {
        timer = setTimeout(() => this.details.reload(), refreshMs);
      }
    });
    inject(DestroyRef).onDestroy(() => clearTimeout(timer));
  }

  // Cancelling.

  protected readonly confirmingCancel = signal(false);
  protected readonly cancelStatus = new RequestStatus();

  protected async cancel(): Promise<void> {
    if (await this.cancelStatus.run(() => this.api.cancelDeployment(this.id()))) {
      this.confirmingCancel.set(false);
      this.details.reload();
    }
  }
}

const reasons = { rollout: 'Rollout', rollback: 'Rollback', redeploy: 'Redeploy' } as const;

/** One timeline entry in words. */
function describe(event: DeploymentEvent): string {
  const detail = event.detail as { message?: unknown } | null;
  const message = typeof detail?.message === 'string' ? detail.message : '';
  switch (event.kind) {
    case 'status_changed': {
      const status = event.status as DeploymentStatus | null;
      const label = status ? (deploymentStatusLabels[status] ?? status) : 'Changed';
      return message ? `${label}: ${message}` : label;
    }
    case 'progress':
      return message || 'Progress reported';
    case 'health':
      return message ? `Health: ${message}` : 'Health reported';
  }
}
