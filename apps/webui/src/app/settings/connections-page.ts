import { Component, effect, inject, signal, untracked } from '@angular/core';
import { ActivatedRoute } from '@angular/router';
import { NgIcon, provideIcons } from '@ng-icons/core';
import { lucidePlus } from '@ng-icons/lucide';
import { HlmAlertImports } from '@spartan-ng/helm/alert';
import { HlmBadgeImports } from '@spartan-ng/helm/badge';
import { HlmButtonImports } from '@spartan-ng/helm/button';
import { HlmSpinnerImports } from '@spartan-ng/helm/spinner';
import { ApiError } from '../api/api-client';
import { ConnectionsApi } from '../api/connections-api';
import { CurrentUser } from '../api/current-user';
import { Connection, ConnectionProvider } from '../api/models';
import { BrowserLocation } from '../auth/browser-location';
import { PageHeader } from '../delivery/page-header';
import { formatDay } from '../shared/dates';
import { RequestStatus } from '../shared/request-status';

const providerNames: Record<ConnectionProvider, string> = {
  github: 'GitHub',
  gitlab: 'GitLab',
  bitbucket: 'Bitbucket',
  gitea: 'Gitea',
  azure_devops: 'Azure DevOps',
};

/**
 * The Organisation's version-control Connections. Owners and admins connect a GitHub account by
 * installing Ivaldi's GitHub App there; GitHub then sends them to the install callback page,
 * which finishes the Connection and comes back here with `?connected=<account>`.
 */
@Component({
  selector: 'app-connections-page',
  imports: [
    NgIcon,
    PageHeader,
    HlmAlertImports,
    HlmBadgeImports,
    HlmButtonImports,
    HlmSpinnerImports,
  ],
  providers: [provideIcons({ lucidePlus })],
  templateUrl: './connections-page.html',
})
export class ConnectionsPage {
  private readonly api = inject(ConnectionsApi);
  private readonly location = inject(BrowserLocation);
  protected readonly user = inject(CurrentUser);

  protected readonly connections = signal<Connection[]>([]);
  protected readonly showArchived = signal(false);
  protected readonly status = new RequestStatus();
  protected readonly formatDay = formatDay;

  /** The account just connected, when the install callback sent the person back here. */
  protected readonly connected = inject(ActivatedRoute).snapshot.queryParamMap.get('connected');

  // Starting an install, which leaves for GitHub.
  protected readonly starting = signal(false);
  protected readonly startError = signal<string | null>(null);

  // Archiving one Connection at a time.
  protected readonly actionStatus = new RequestStatus();
  protected readonly acting = signal<string | null>(null);

  constructor() {
    effect(() => {
      const organizationId = this.user.organization()?.id;
      const includeArchived = this.showArchived();
      if (organizationId) {
        untracked(() => void this.load(includeArchived));
      }
    });
  }

  protected providerName(connection: Connection): string {
    return providerNames[connection.provider] ?? connection.provider;
  }

  /** The account's name, or its ID when the provider gave no name. */
  protected accountName(connection: Connection): string {
    return connection.external_account_name ?? connection.external_account_id;
  }

  /** Sends the person to GitHub to install the App on an account. */
  protected async connectGitHub(): Promise<void> {
    this.starting.set(true);
    this.startError.set(null);
    try {
      const start = await this.api.startInstallation('github');
      this.location.assign(start.url);
    } catch (e) {
      this.starting.set(false);
      this.startError.set(
        e instanceof ApiError && e.status === 422
          ? "GitHub isn't set up for this Ivaldi installation yet. Ask whoever runs Ivaldi to configure its GitHub App."
          : e instanceof ApiError
            ? e.message
            : 'Something went wrong. Try again.',
      );
    }
  }

  protected async archive(connection: Connection): Promise<void> {
    this.acting.set(connection.id);
    const done = await this.actionStatus.run(() => this.api.archive(connection.id));
    this.acting.set(null);
    if (done) {
      await this.load(this.showArchived(), 'saving');
    }
  }

  /** Loads the list; a reload after a change keeps the table on screen instead of a spinner. */
  private async load(
    includeArchived: boolean,
    flag: 'loading' | 'saving' = 'loading',
  ): Promise<void> {
    await this.status.run(async () => {
      this.connections.set(await this.api.list(includeArchived));
    }, flag);
  }
}
