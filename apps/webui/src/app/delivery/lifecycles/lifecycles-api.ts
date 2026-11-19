import { inject, Service } from '@angular/core';
import { ApiClient } from '../../api/api-client';
import { Lifecycle, Setting } from '../../api/models';
import { defaultLifecycleKey } from '../projects-api';

/** A phase as the Lifecycle forms edit it and the control plane takes it. */
export interface PhaseFields {
  name: string;
  environment_ids: string[];
  /** How many of its Environments need a healthy Deployment; all when left out. */
  min_environments?: number;
  /** Whether a Release may skip it. */
  optional: boolean;
}

/** A Workspace's Lifecycles, their phases, and which one new Applications follow. */
@Service()
export class LifecyclesApi {
  private readonly api = inject(ApiClient);

  /** The Workspace's Lifecycles by name, up to the 100 a page holds. */
  list(workspaceId: string, includeArchived = false): Promise<Lifecycle[]> {
    const query = new URLSearchParams({ limit: '100', sort_by: 'display_name' });
    if (includeArchived) {
      query.set('include_archived', 'true');
    }
    return this.api.list(`/workspaces/${workspaceId}/lifecycles?${query}`);
  }

  get(id: string): Promise<Lifecycle> {
    return this.api.get(`/lifecycles/${id}`);
  }

  create(
    workspaceId: string,
    lifecycle: { display_name: string; description: string; phases: PhaseFields[] },
  ): Promise<Lifecycle> {
    return this.api.send('POST', `/workspaces/${workspaceId}/lifecycles`, lifecycle);
  }

  update(id: string, fields: { display_name: string; description: string }): Promise<Lifecycle> {
    return this.api.send('PATCH', `/lifecycles/${id}`, fields);
  }

  addPhase(id: string, phase: PhaseFields): Promise<Lifecycle> {
    return this.api.send('POST', `/lifecycles/${id}/phases`, phase);
  }

  updatePhase(id: string, phaseId: string, phase: PhaseFields): Promise<Lifecycle> {
    return this.api.send('PUT', `/lifecycles/${id}/phases/${phaseId}`, phase);
  }

  /** Moves a phase to `position`, counting from 0. */
  movePhase(id: string, phaseId: string, position: number): Promise<Lifecycle> {
    return this.api.send('PUT', `/lifecycles/${id}/phases/${phaseId}/position`, { position });
  }

  removePhase(id: string, phaseId: string): Promise<Lifecycle> {
    return this.api.send('DELETE', `/lifecycles/${id}/phases/${phaseId}`);
  }

  /** Archives a Lifecycle. The control plane refuses while active Channels follow it. */
  archive(id: string): Promise<Lifecycle> {
    return this.api.send('POST', `/lifecycles/${id}/archive`);
  }

  restore(id: string): Promise<Lifecycle> {
    return this.api.send('POST', `/lifecycles/${id}/restore`);
  }

  /**
   * The Lifecycle a new Application's default Channel follows when its creator names none, as
   * stored on the Workspace, or null when the Workspace stores none.
   */
  async workspaceDefault(workspaceId: string): Promise<Setting | null> {
    const settings = await this.api.get<Setting[]>(`/settings?${scope(workspaceId)}`);
    return settings.find((s) => s.key === defaultLifecycleKey) ?? null;
  }

  /** Makes a Lifecycle the Workspace's default; `version` is the stored value's, or 0. */
  makeDefault(workspaceId: string, lifecycleId: string, version: number): Promise<Setting> {
    return this.api.send('PUT', `/settings/${defaultLifecycleKey}?${scope(workspaceId)}`, {
      value: lifecycleId,
      expected_version: version,
    });
  }
}

function scope(workspaceId: string): URLSearchParams {
  return new URLSearchParams({ workspace_id: workspaceId });
}
