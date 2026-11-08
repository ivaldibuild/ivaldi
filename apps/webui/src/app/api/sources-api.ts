import { inject, Service } from '@angular/core';
import { ApiClient, Page } from './api-client';
import { Source } from './models';

/** The repositories connected to a Project as Sources. */
@Service()
export class SourcesApi {
  private readonly api = inject(ApiClient);

  /** Every Source in the Project by name, and disconnected ones when `includeArchived` is set. */
  async list(projectId: string, includeArchived = false): Promise<Source[]> {
    const sources: Source[] = [];
    let cursor: string | null = null;
    do {
      const query = new URLSearchParams({ sort_by: 'full_name', limit: '100' });
      if (includeArchived) {
        query.set('include_archived', 'true');
      }
      if (cursor) {
        query.set('next_cursor', cursor);
      }
      const page: Page<Source> = await this.api.get(
        `/projects/${encodeURIComponent(projectId)}/sources?${query}`,
      );
      sources.push(...page.items);
      cursor = page.has_next ? page.next_cursor : null;
    } while (cursor);
    return sources;
  }

  /** Connects a repository the Connection's account lets Ivaldi read. */
  create(projectId: string, connectionId: string, externalRepositoryId: string): Promise<Source> {
    return this.api.send('POST', `/projects/${encodeURIComponent(projectId)}/sources`, {
      connection_id: connectionId,
      external_repository_id: externalRepositoryId,
    });
  }

  /** Disconnects a Source; its history stays. */
  archive(id: string): Promise<Source> {
    return this.api.send('POST', `/sources/${encodeURIComponent(id)}/archive`);
  }

  /** Reconnects a disconnected Source. */
  restore(id: string): Promise<Source> {
    return this.api.send('POST', `/sources/${encodeURIComponent(id)}/restore`);
  }
}
