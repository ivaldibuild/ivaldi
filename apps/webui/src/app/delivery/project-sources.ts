import { Component, computed, effect, inject, input, signal, untracked } from '@angular/core';
import { RouterLink } from '@angular/router';
import { NgIcon, provideIcons } from '@ng-icons/core';
import { lucidePlus } from '@ng-icons/lucide';
import { HlmAlertImports } from '@spartan-ng/helm/alert';
import { HlmBadgeImports } from '@spartan-ng/helm/badge';
import { HlmButtonImports } from '@spartan-ng/helm/button';
import { HlmInputImports } from '@spartan-ng/helm/input';
import { HlmSpinnerImports } from '@spartan-ng/helm/spinner';
import { ConnectionsApi } from '../api/connections-api';
import { Connection, Repository, Source } from '../api/models';
import { PermissionsApi } from '../api/permissions-api';
import { SourcesApi } from '../api/sources-api';
import { formatDay } from '../shared/dates';
import { RequestStatus } from '../shared/request-status';

/**
 * A Project's Sources: the repositories connected to it through the Organisation's Connections.
 * Connecting one picks from the repositories a Connection's account lets Ivaldi read, which the
 * provider is asked for each time the picker opens.
 */
@Component({
  selector: 'app-project-sources',
  imports: [
    NgIcon,
    RouterLink,
    HlmAlertImports,
    HlmBadgeImports,
    HlmButtonImports,
    HlmInputImports,
    HlmSpinnerImports,
  ],
  providers: [provideIcons({ lucidePlus })],
  templateUrl: './project-sources.html',
})
export class ProjectSources {
  private readonly sourcesApi = inject(SourcesApi);
  private readonly connectionsApi = inject(ConnectionsApi);
  private readonly permissionsApi = inject(PermissionsApi);

  readonly projectId = input.required<string>();
  /** An archived Project gets no new Sources. */
  readonly archived = input(false);

  /**
   * Whether the person may connect and disconnect Sources here (`source.manage` on the Project,
   * from a grant on it or above). Until that's known, nothing is offered.
   */
  protected readonly canManage = signal(false);

  protected readonly sources = signal<Source[]>([]);
  protected readonly showArchived = signal(false);
  protected readonly status = new RequestStatus();
  protected readonly formatDay = formatDay;

  // Disconnecting and reconnecting one Source at a time.
  protected readonly actionStatus = new RequestStatus();
  protected readonly acting = signal<string | null>(null);

  // Connecting a repository.
  protected readonly connecting = signal(false);
  protected readonly connections = signal<Connection[]>([]);
  protected readonly connectionsStatus = new RequestStatus();
  protected readonly connectionId = signal('');
  protected readonly repositories = signal<Repository[]>([]);
  protected readonly repositoriesStatus = new RequestStatus();
  protected readonly filter = signal('');
  protected readonly selected = signal<string | null>(null);
  protected readonly createStatus = new RequestStatus();

  /** Connections the control plane can list repositories through. */
  protected readonly usableConnections = computed(() =>
    this.connections().filter(
      (c) => c.status === 'active' && !c.deleted_at && c.lookup_from === 'control_plane',
    ),
  );

  protected readonly filtered = computed(() => {
    const text = this.filter().trim().toLowerCase();
    return text
      ? this.repositories().filter((r) => r.full_name.toLowerCase().includes(text))
      : this.repositories();
  });

  /** Repositories already connected to this Project through the chosen Connection. */
  private readonly connectedHere = computed(
    () =>
      new Set(
        this.sources()
          .filter((s) => !s.archived_at && s.connection_id === this.connectionId())
          .map((s) => s.external_repository_id),
      ),
  );

  constructor() {
    effect(() => {
      const projectId = this.projectId();
      const includeArchived = this.showArchived();
      untracked(() => void this.load(projectId, includeArchived));
    });
    effect(() => {
      const projectId = this.projectId();
      untracked(() => void this.loadPermissions(projectId));
    });
  }

  protected isConnectedHere(repository: Repository): boolean {
    return this.connectedHere().has(repository.external_id);
  }

  /** The badge for a repository an active Source already connects, here or in another Project. */
  protected connectedLabel(repository: Repository): string {
    return this.isConnectedHere(repository) ? 'Connected here' : 'In another Project';
  }

  protected accountName(connection: Connection): string {
    return connection.external_account_name ?? connection.external_account_id;
  }

  protected async openConnect(): Promise<void> {
    this.connecting.set(true);
    this.createStatus.error.set(null);
    await this.connectionsStatus.run(async () => {
      this.connections.set(await this.connectionsApi.list());
    }, 'loading');
    const first = this.usableConnections()[0];
    if (first) {
      await this.chooseConnection(first.id);
    }
  }

  protected closeConnect(): void {
    this.connecting.set(false);
    this.connectionId.set('');
    this.repositories.set([]);
    this.filter.set('');
    this.selected.set(null);
    this.createStatus.error.set(null);
  }

  protected async chooseConnection(id: string): Promise<void> {
    this.connectionId.set(id);
    this.selected.set(null);
    this.repositories.set([]);
    await this.repositoriesStatus.run(async () => {
      this.repositories.set(await this.connectionsApi.repositories(id));
    }, 'loading');
  }

  protected async connect(event: Event): Promise<void> {
    event.preventDefault();
    const repository = this.selected();
    if (!repository) {
      return;
    }
    const created = await this.createStatus.run(() =>
      this.sourcesApi.create(this.projectId(), this.connectionId(), repository),
    );
    if (created) {
      this.closeConnect();
      await this.load(this.projectId(), this.showArchived(), 'saving');
    }
  }

  protected disconnect(source: Source): Promise<void> {
    return this.act(source, () => this.sourcesApi.archive(source.id));
  }

  protected reconnect(source: Source): Promise<void> {
    return this.act(source, () => this.sourcesApi.restore(source.id));
  }

  private async act(source: Source, work: () => Promise<unknown>): Promise<void> {
    this.acting.set(source.id);
    const done = await this.actionStatus.run(work);
    this.acting.set(null);
    if (done) {
      await this.load(this.projectId(), this.showArchived(), 'saving');
    }
  }

  private async loadPermissions(projectId: string): Promise<void> {
    this.canManage.set(false);
    try {
      const granted = await this.permissionsApi.granted(projectId, ['source.manage']);
      this.canManage.set(granted.has('source.manage'));
    } catch {
      // Offering nothing is safe: the control plane refuses what the person may not do anyway.
    }
  }

  /** Loads the list; a reload after a change keeps the table on screen instead of a spinner. */
  private async load(
    projectId: string,
    includeArchived: boolean,
    flag: 'loading' | 'saving' = 'loading',
  ): Promise<void> {
    await this.status.run(async () => {
      this.sources.set(await this.sourcesApi.list(projectId, includeArchived));
    }, flag);
  }
}
