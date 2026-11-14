import { DatePipe } from '@angular/common';
import { Component, computed, inject, input, linkedSignal, resource, signal } from '@angular/core';
import { form, FormField, maxLength, required, submit } from '@angular/forms/signals';
import { RouterLink } from '@angular/router';
import { NgIcon, provideIcons } from '@ng-icons/core';
import { lucideArchive, lucideServer, lucideTerminal } from '@ng-icons/lucide';
import { HlmAlertImports } from '@spartan-ng/helm/alert';
import { HlmBadgeImports } from '@spartan-ng/helm/badge';
import { HlmButtonImports } from '@spartan-ng/helm/button';
import { HlmFieldImports } from '@spartan-ng/helm/field';
import { HlmInputImports } from '@spartan-ng/helm/input';
import { HlmSpinnerImports } from '@spartan-ng/helm/spinner';
import { ApiError } from '../../api/api-client';
import { CurrentUser } from '../../api/current-user';
import { AgentStatus, Cluster, Environment } from '../../api/models';
import { RequestStatus } from '../../shared/request-status';
import { ClustersApi } from '../clusters-api';
import { DeliveryApi } from '../delivery-api';
import { PageHeader } from '../page-header';
import { WorkspaceContext } from '../workspace-context';
import { ClusterGuide } from './cluster-guide';

/**
 * One Cluster: its agent, credential, and what it last reported; renaming and
 * archiving it; and the onboarding guide that installs or re-enrols the agent.
 */
@Component({
  selector: 'app-cluster-page',
  imports: [
    DatePipe,
    FormField,
    NgIcon,
    RouterLink,
    PageHeader,
    ClusterGuide,
    HlmAlertImports,
    HlmBadgeImports,
    HlmButtonImports,
    HlmFieldImports,
    HlmInputImports,
    HlmSpinnerImports,
  ],
  providers: [provideIcons({ lucideArchive, lucideServer, lucideTerminal })],
  templateUrl: './cluster-page.html',
})
export class ClusterPage {
  private readonly api = inject(ClustersApi);
  private readonly delivery = inject(DeliveryApi);
  private readonly context = inject(WorkspaceContext);
  private readonly user = inject(CurrentUser);

  /** The `:id` route parameter. */
  readonly id = input.required<string>();

  /** Clusters are managed with cluster.manage, which owners and admins hold. */
  protected readonly canManage = this.user.canManageOrganization;

  protected readonly details = resource({
    params: () => this.id(),
    loader: async ({ params: id }) => {
      const [cluster, agent] = await Promise.all([this.api.cluster(id), this.api.agentStatus(id)]);
      return { cluster, agent };
    },
  });

  private readonly environmentList = resource({
    loader: () => this.loadEnvironments(),
  });

  private readonly loaded = computed(() =>
    this.details.hasValue() ? this.details.value() : undefined,
  );
  protected readonly cluster = computed(() => this.loaded()?.cluster ?? null);
  protected readonly agent = computed<AgentStatus | null>(() => this.loaded()?.agent ?? null);
  protected readonly environments = computed<Environment[]>(() =>
    this.environmentList.hasValue() ? this.environmentList.value() : [],
  );
  protected readonly failure = computed(() => {
    const error = this.details.error();
    return error ? (error instanceof ApiError ? error.message : 'Something went wrong.') : null;
  });
  protected readonly archived = computed(() => !!this.cluster()?.deleted_at);

  private async loadEnvironments(): Promise<Environment[]> {
    const workspace = this.context.current();
    return workspace ? this.delivery.environments(workspace.id) : [];
  }

  /**
   * The guide is shown while an active Cluster has no agent, or when
   * re-enrolling, and only to people who can create its enrolment token.
   * Archiving clears the agent, but an archived Cluster cannot be enrolled.
   */
  protected readonly connecting = signal(false);
  private readonly needsAgent = computed(
    () => !!this.cluster() && !this.archived() && !this.cluster()!.agent_principal_id,
  );
  protected readonly showGuide = computed(
    () => this.canManage() && !this.archived() && (this.connecting() || this.needsAgent()),
  );
  /** Someone who cannot connect the Cluster is told who can. */
  protected readonly showWhoCanConnect = computed(() => !this.canManage() && this.needsAgent());
  protected readonly guideStart = computed(() => (this.agent()?.status === 'unassigned' ? 2 : 3));

  protected guideDone(): void {
    this.connecting.set(false);
    this.details.reload();
  }

  protected capabilities(): string[] {
    const capabilities = this.cluster()?.agent_capabilities as
      Record<string, unknown> | null | undefined;
    if (!capabilities) {
      return [];
    }
    const out: string[] = [];
    for (const key of ['deployers', 'secret_store_kinds']) {
      const value = capabilities[key];
      if (Array.isArray(value) && value.length > 0) {
        out.push(`${key.replace(/_/g, ' ')}: ${value.join(', ')}`);
      }
    }
    if (typeof capabilities['watch_mode'] === 'string') {
      out.push(`watching ${capabilities['watch_mode']}`);
    }
    return out;
  }

  // Renaming.
  private readonly nameModel = linkedSignal(() => ({
    display_name: this.cluster()?.display_name ?? '',
  }));
  protected readonly nameForm = form(this.nameModel, (path) => {
    required(path.display_name, { message: 'Name the cluster.' });
    maxLength(path.display_name, 255, { message: 'Use at most 255 characters.' });
  });
  protected readonly nameStatus = new RequestStatus();

  protected async rename(event: Event): Promise<void> {
    event.preventDefault();
    const cluster = this.cluster();
    if (!cluster) {
      return;
    }
    await submit(this.nameForm, async () => {
      const name = this.nameModel().display_name.trim();
      if (
        name !== cluster.display_name &&
        (await this.nameStatus.run(() => this.api.updateCluster(cluster.id, name)))
      ) {
        this.details.reload();
      }
      return undefined;
    });
  }

  // Archiving and restoring.
  protected readonly confirmingArchive = signal(false);
  protected readonly lifecycle = new RequestStatus();

  protected async archive(): Promise<void> {
    const cluster = this.cluster();
    if (cluster && (await this.lifecycle.run(() => this.api.archiveCluster(cluster.id)))) {
      this.confirmingArchive.set(false);
      this.details.reload();
    }
  }

  protected async restore(): Promise<void> {
    const cluster = this.cluster();
    if (cluster && (await this.lifecycle.run(() => this.api.restoreCluster(cluster.id)))) {
      this.details.reload();
    }
  }
}
