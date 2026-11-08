import { inject, Service } from '@angular/core';
import { ApiClient } from './api-client';
import {
  CompleteInstallation,
  Connection,
  InstallableProvider,
  InstallationStart,
  Repository,
} from './models';

/**
 * The Organisation's version-control Connections. An app installation is connected only by
 * installing the app at the provider: `startInstallation` gives the page to send the person to,
 * and the callback page passes what the provider sent back to `completeInstallation`.
 */
@Service()
export class ConnectionsApi {
  private readonly api = inject(ApiClient);

  list(includeArchived = false): Promise<Connection[]> {
    return this.api.get(`/connections${includeArchived ? '?include_archived=true' : ''}`);
  }

  /** What the Connection's account lets Ivaldi read now, by name; asked of the provider each time. */
  repositories(id: string): Promise<Repository[]> {
    return this.api.get(`/connections/${encodeURIComponent(id)}/repositories`);
  }

  archive(id: string): Promise<Connection> {
    return this.api.send('POST', `/connections/${encodeURIComponent(id)}/archive`);
  }

  startInstallation(provider: InstallableProvider): Promise<InstallationStart> {
    return this.api.send('POST', '/connections/installations', { provider });
  }

  completeInstallation(body: CompleteInstallation): Promise<Connection> {
    return this.api.send('POST', '/connections/installations/complete', body);
  }
}
