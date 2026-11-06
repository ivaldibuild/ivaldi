import { inject, Service } from '@angular/core';
import { ApiClient, Page } from '../api/api-client';
import { Application, Project, ResolvedSetting, Setting } from '../api/models';

/** The setting naming the Lifecycle a new Application's default Channel follows. */
export const defaultLifecycleKey = 'delivery.default_lifecycle';

/** What a list of Projects keeps. */
export interface ProjectFilter {
  /** Matches names and slugs, ignoring case. */
  search: string;
  ownerTeamId: string;
  includeArchived: boolean;
}

/** Managing a Workspace's Projects, and the settings stored for one. */
@Service()
export class ProjectsApi {
  private readonly api = inject(ApiClient);

  /** A page of the Workspace's Projects by name; `cursor` asks for the page after a previous one. */
  page(workspaceId: string, filter: ProjectFilter, cursor?: string | null): Promise<Page<Project>> {
    const params = new URLSearchParams({ sort_by: 'display_name', limit: '50' });
    if (filter.search.trim()) {
      params.set('q', filter.search.trim());
    }
    if (filter.ownerTeamId) {
      params.set('owner_team_id', filter.ownerTeamId);
    }
    if (filter.includeArchived) {
      params.set('include_archived', 'true');
    }
    if (cursor) {
      params.set('next_cursor', cursor);
    }
    return this.api.get(`/workspaces/${workspaceId}/projects?${params}`);
  }

  project(id: string): Promise<Project> {
    return this.api.get(`/projects/${id}`);
  }

  update(
    id: string,
    changes: { display_name?: string; slug?: string; description?: string },
  ): Promise<Project> {
    return this.api.send('PATCH', `/projects/${id}`, changes);
  }

  /** Hands the Project to another Team. Its Applications keep their own owners. */
  transfer(id: string, ownerTeamId: string): Promise<Project> {
    return this.api.send('POST', `/projects/${id}/transfer`, { owner_team_id: ownerTeamId });
  }

  archive(id: string): Promise<Project> {
    return this.api.send('POST', `/projects/${id}/archive`);
  }

  restore(id: string): Promise<Project> {
    return this.api.send('POST', `/projects/${id}/restore`);
  }

  /** The Project's Applications, up to the 100 a page holds. */
  applications(id: string): Promise<Application[]> {
    return this.api.list(`/projects/${id}/applications?limit=100`);
  }

  /** The setting's effective value for the Project, from the Project, its Workspace, or above. */
  resolved(project: Project, key: string): Promise<ResolvedSetting> {
    return this.api.get(`/settings/${key}/resolved?${scope(project)}`);
  }

  /** The value stored on the Project itself, or null when the levels above decide. */
  async stored(project: Project, key: string): Promise<Setting | null> {
    const settings = await this.api.get<Setting[]>(`/settings?${scope(project)}`);
    return settings.find((s) => s.key === key) ?? null;
  }

  /** Stores a value on the Project; `version` is the stored one's, or 0 when there is none. */
  set(project: Project, key: string, value: unknown, version: number): Promise<Setting> {
    return this.api.send('PUT', `/settings/${key}?${scope(project)}`, {
      value,
      expected_version: version,
    });
  }

  /** Removes the Project's own value, so its Workspace's applies. */
  clear(project: Project, key: string, version: number): Promise<void> {
    return this.api.send(
      'DELETE',
      `/settings/${key}?${scope(project)}&expected_version=${version}`,
    );
  }
}

function scope(project: Project): URLSearchParams {
  return new URLSearchParams({ workspace_id: project.workspace_id, project_id: project.id });
}
