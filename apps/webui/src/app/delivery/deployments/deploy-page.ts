import { Component, computed, inject, input, resource, signal } from '@angular/core';
import { Router, RouterLink } from '@angular/router';
import { NgIcon, provideIcons } from '@ng-icons/core';
import { lucideCheck, lucideRocket, lucideX } from '@ng-icons/lucide';
import { HlmAlertImports } from '@spartan-ng/helm/alert';
import { HlmButtonImports } from '@spartan-ng/helm/button';
import { HlmSpinnerImports } from '@spartan-ng/helm/spinner';
import { ApiError } from '../../api/api-client';
import { Build, Deployment, ReleaseChanges, Target } from '../../api/models';
import { RequestStatus } from '../../shared/request-status';
import { DeliveryApi } from '../delivery-api';
import { PageHeader } from '../page-header';
import { ProjectsApi } from '../projects-api';
import { shortDigest } from '../releases/release-labels';
import { checkLabel, DeploymentStatusBadge } from './deployment-status';

/** What a Target's checks and changes look like while they load and after. */
type Preview =
  | { state: 'loading' }
  | { state: 'ready'; changes: ReleaseChanges; ready: boolean }
  | { state: 'error'; message: string };

/**
 * Deploying a published Release: choose the Targets its Application is placed on, see what each
 * would change and the checks it must pass, then deploy. Each Target gets a Deployment of its
 * own, which applies at once or waits for approval.
 */
@Component({
  selector: 'app-deploy-page',
  imports: [
    NgIcon,
    RouterLink,
    PageHeader,
    DeploymentStatusBadge,
    HlmAlertImports,
    HlmButtonImports,
    HlmSpinnerImports,
  ],
  providers: [provideIcons({ lucideCheck, lucideRocket, lucideX })],
  templateUrl: './deploy-page.html',
})
export class DeployPage {
  private readonly api = inject(DeliveryApi);
  private readonly projects = inject(ProjectsApi);
  private readonly router = inject(Router);

  /** The `:id` route parameter: the Release. */
  readonly id = input.required<string>();

  protected readonly details = resource({
    params: () => this.id(),
    loader: async ({ params: id }) => {
      const release = await this.api.release(id);
      const application = await this.api.application(release.application_id);
      const [project, placements, environments, channels, lifecycles, releases] = await Promise.all(
        [
          this.projects.project(application.project_id),
          this.api.placements(application.id),
          this.api.environments(application.workspace_id),
          this.api.channels(application.id),
          this.api.lifecycles(application.workspace_id),
          this.api.releases(application.id, { limit: 100 }),
        ],
      );
      const targets = await this.api.targetsOf(environments);
      return {
        release,
        application,
        project,
        placements,
        environments,
        channels,
        lifecycles,
        targets,
        versions: new Map(releases.items.map((r) => [r.id, r.version])),
      };
    },
  });

  protected readonly release = computed(() => this.details.value()?.release ?? null);
  protected readonly application = computed(() => this.details.value()?.application ?? null);
  protected readonly project = computed(() => this.details.value()?.project ?? null);
  protected readonly published = computed(() => this.release()?.status === 'published');

  /**
   * The Targets the Application is placed on, by Environment, in the order of the Release's
   * Lifecycle and then any other Environments.
   */
  protected readonly groups = computed(() => {
    const d = this.details.value();
    if (!d) {
      return [];
    }
    const channel = d.channels.find((c) => c.id === d.release.channel_id);
    const lifecycle = d.lifecycles.find((l) => l.id === channel?.lifecycle_id);
    const order = [...(lifecycle?.phases ?? [])]
      .sort((a, b) => a.position - b.position)
      .flatMap((p) => p.environment_ids);
    const rank = (id: string) => (order.includes(id) ? order.indexOf(id) : order.length);
    return [...d.environments]
      .sort((a, b) => rank(a.id) - rank(b.id))
      .map((environment) => ({
        environment,
        inLifecycle: order.includes(environment.id),
        targets: d.placements
          .map((placement) => ({
            placement,
            target: d.targets.find((t) => t.id === placement.target_id),
          }))
          .filter(
            (p): p is { placement: typeof p.placement; target: Target } =>
              p.target?.environment_id === environment.id,
          )
          .map(({ placement, target }) => ({
            target,
            current: placement.current_release_id
              ? (d.versions.get(placement.current_release_id) ?? 'an earlier Release')
              : null,
          })),
      }))
      .filter((g) => g.targets.length > 0);
  });

  protected readonly failure = computed(() => {
    const error = this.details.error();
    return error ? (error instanceof ApiError ? error.message : 'Something went wrong.') : null;
  });

  // Choosing Targets, and what each would change.

  protected readonly selected = signal<string[]>([]);
  protected readonly previews = signal<ReadonlyMap<string, Preview>>(new Map());

  protected isSelected(targetId: string): boolean {
    return this.selected().includes(targetId);
  }

  protected toggle(target: Target, event: Event): void {
    const checked = (event.target as HTMLInputElement).checked;
    this.selected.update((ids) =>
      checked ? [...ids, target.id] : ids.filter((id) => id !== target.id),
    );
    if (checked) {
      void this.preview(target.id);
    }
  }

  private async preview(targetId: string): Promise<void> {
    this.setPreview(targetId, { state: 'loading' });
    try {
      const changes = await this.api.releaseChanges(this.id(), targetId);
      this.setPreview(targetId, {
        state: 'ready',
        changes,
        ready: changes.checks.every((c) => c.ok),
      });
    } catch (e) {
      this.setPreview(targetId, {
        state: 'error',
        message: e instanceof ApiError ? e.message : 'Something went wrong.',
      });
    }
  }

  private setPreview(targetId: string, preview: Preview): void {
    this.previews.update((all) => new Map(all).set(targetId, preview));
  }

  /** Each chosen Target with its preview, in the order chosen. */
  protected readonly chosen = computed(() => {
    const targets = this.details.value()?.targets ?? [];
    return this.selected().map((id) => ({
      target: targets.find((t) => t.id === id)!,
      preview: this.previews().get(id) ?? ({ state: 'loading' } as Preview),
    }));
  });

  protected readonly blocked = computed(() =>
    this.chosen().some((c) => c.preview.state !== 'ready' || !c.preview.ready),
  );

  protected readonly checkLabel = checkLabel;

  protected buildLabel(build: Build | null | undefined): string {
    if (!build) {
      return 'Nothing yet';
    }
    return build.version ?? shortDigest(build.digest);
  }

  protected sameBuild(change: ReleaseChanges['artifacts'][number]): boolean {
    return !!change.previous && change.previous.digest === change.next.digest;
  }

  protected shortCommit(sha: string): string {
    return sha.slice(0, 8);
  }

  // Deploying.

  protected readonly status = new RequestStatus();
  protected readonly deployed = signal<Deployment[]>([]);

  protected targetName(id: string): string {
    return this.details.value()?.targets.find((t) => t.id === id)?.display_name ?? id;
  }

  protected async deploy(): Promise<void> {
    if (this.selected().length === 0 || this.blocked()) {
      return;
    }
    let created: Deployment[] = [];
    const done = await this.status.run(async () => {
      created = await this.api.requestDeployments(this.id(), this.selected());
    });
    if (!done) {
      return;
    }
    if (created.length === 1) {
      await this.router.navigate(['/deployments', created[0].id]);
      return;
    }
    this.deployed.set(created);
    this.selected.set([]);
  }
}
