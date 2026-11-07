import { inject, Service } from '@angular/core';
import { ApiClient } from './api-client';
import { Workspace } from './models';

/** What a Workspace is created with. */
export interface NewWorkspace {
  display_name: string;
  slug: string;
  description?: string;
}

/** The Organisation's Workspaces, as owners and admins manage them. */
@Service()
export class WorkspacesApi {
  private readonly api = inject(ApiClient);

  /** Active Workspaces, and archived ones too when `includeArchived` is set. */
  list(includeArchived = false): Promise<Workspace[]> {
    return this.api.get(`/workspaces${includeArchived ? '?include_archived=true' : ''}`);
  }

  create(workspace: NewWorkspace): Promise<Workspace> {
    return this.api.send('POST', '/workspaces', workspace);
  }

  update(
    id: string,
    changes: { display_name?: string; slug?: string; description?: string },
  ): Promise<Workspace> {
    return this.api.send('PATCH', `/workspaces/${encodeURIComponent(id)}`, changes);
  }

  /** Makes it the Workspace new people and pages start in. */
  makeDefault(id: string): Promise<Workspace> {
    return this.api.send('POST', `/workspaces/${encodeURIComponent(id)}/default`);
  }

  archive(id: string): Promise<Workspace> {
    return this.api.send('POST', `/workspaces/${encodeURIComponent(id)}/archive`);
  }

  restore(id: string): Promise<Workspace> {
    return this.api.send('POST', `/workspaces/${encodeURIComponent(id)}/restore`);
  }
}
