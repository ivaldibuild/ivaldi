import { Component, computed, effect, inject, input, signal, untracked } from '@angular/core';
import { RouterLink } from '@angular/router';
import { HlmButtonImports } from '@spartan-ng/helm/button';
import { HlmSpinnerImports } from '@spartan-ng/helm/spinner';
import { ApiError } from '../../api/api-client';
import { Deployment, DeploymentStatus, Environment, Target } from '../../api/models';
import { formatDayAndTime } from '../../shared/dates';
import { DeliveryApi } from '../delivery-api';
import { DeploymentStatusBadge, deploymentStatusLabels } from './deployment-status';

/** An Application's Deployments, newest first, filtered by status. */
@Component({
  selector: 'app-deployment-list',
  imports: [RouterLink, DeploymentStatusBadge, HlmButtonImports, HlmSpinnerImports],
  templateUrl: './deployment-list.html',
})
export class DeploymentList {
  private readonly api = inject(DeliveryApi);

  readonly applicationId = input.required<string>();
  readonly targets = input.required<Target[]>();
  readonly environments = input.required<Environment[]>();

  protected readonly statuses = Object.entries(deploymentStatusLabels).map(([value, label]) => ({
    value: value as DeploymentStatus,
    label,
  }));
  protected readonly status = signal<DeploymentStatus | ''>('');
  private readonly items = signal<Deployment[]>([]);
  private readonly versions = signal<ReadonlyMap<string, string>>(new Map());
  protected readonly total = signal(0);
  protected readonly loaded = signal(false);
  protected readonly loadError = signal<string | null>(null);
  protected readonly loadingMore = signal(false);
  private readonly cursor = signal<string | null>(null);

  protected readonly rows = computed(() =>
    this.items().map((deployment) => {
      const target = this.targets().find((t) => t.id === deployment.target_id);
      return {
        deployment,
        version: this.versions().get(deployment.release_id) ?? '…',
        target: target?.display_name ?? deployment.target_id,
        environment:
          this.environments().find((e) => e.id === deployment.environment_id)?.display_name ?? '',
        requested: formatDayAndTime(deployment.requested_at),
      };
    }),
  );
  protected readonly hasMore = computed(() => this.cursor() !== null);

  constructor() {
    effect(() => {
      this.applicationId();
      this.status();
      untracked(() => void this.load(true));
    });
  }

  protected filter(event: Event): void {
    this.status.set((event.target as HTMLSelectElement).value as DeploymentStatus | '');
  }

  protected async more(): Promise<void> {
    this.loadingMore.set(true);
    await this.load(false);
    this.loadingMore.set(false);
  }

  private async load(reset: boolean): Promise<void> {
    try {
      const page = await this.api.deployments(this.applicationId(), {
        status: this.status() || undefined,
        cursor: reset ? null : this.cursor(),
      });
      this.items.update((current) => (reset ? page.items : [...current, ...page.items]));
      this.total.set(page.total_count);
      this.cursor.set(page.has_next ? page.next_cursor : null);
      this.loadError.set(null);
      await this.nameReleases(page.items);
    } catch (e) {
      this.loadError.set(e instanceof ApiError ? e.message : 'Something went wrong. Try again.');
    } finally {
      this.loaded.set(true);
    }
  }

  /** Looks up the version of each Release not already known. */
  private async nameReleases(deployments: Deployment[]): Promise<void> {
    const known = this.versions();
    const missing = [...new Set(deployments.map((d) => d.release_id))].filter(
      (id) => !known.has(id),
    );
    if (missing.length === 0) {
      return;
    }
    const releases = await Promise.all(missing.map((id) => this.api.release(id)));
    this.versions.update((all) => {
      const next = new Map(all);
      releases.forEach((r) => next.set(r.id, r.version));
      return next;
    });
  }
}
