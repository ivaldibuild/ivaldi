import { inject, Service } from '@angular/core';
import { ApiClient } from '../api/api-client';
import {
  Agent,
  Environment,
  Lifecycle,
  Organization,
  Target,
  Team,
  TeamRole,
  Tier,
  Workspace,
} from '../api/models';

export { ApiError } from '../api/api-client';
export type {
  Agent,
  Environment,
  Lifecycle,
  LifecyclePhase,
  Organization,
  Target,
  Team,
  TeamMember,
  TeamRole,
  Tier,
  Workspace,
} from '../api/models';

/** The control-plane operations the first-run setup uses. */
@Service()
export class OnboardingApi {
  private readonly api = inject(ApiClient);

  organization(): Promise<Organization> {
    return this.api.get('/organization');
  }

  /** Changes only the fields given; the rest stay as they are. */
  updateOrganization(
    change: Partial<Pick<Organization, 'display_name' | 'name' | 'slug'>> & { logo_url?: string },
  ): Promise<Organization> {
    return this.api.send('PATCH', '/organization', change);
  }

  async defaultWorkspace(): Promise<Workspace | null> {
    const workspaces = await this.api.get<Workspace[]>('/workspaces');
    return workspaces.find((w) => w.is_default) ?? workspaces[0] ?? null;
  }

  updateWorkspace(id: string, change: { display_name: string; slug: string }): Promise<Workspace> {
    return this.api.send('PATCH', `/workspaces/${id}`, change);
  }

  async teams(): Promise<Team[]> {
    return this.api.list<Team>('/teams');
  }

  createTeam(team: { display_name: string; slug?: string }): Promise<Team> {
    return this.api.send('POST', '/teams', team);
  }

  setTeamMember(teamId: string, principalId: string, role: TeamRole): Promise<unknown> {
    return this.api.send('PUT', `/teams/${teamId}/members/${principalId}`, { role });
  }

  async createAgent(teamId: string, displayName: string): Promise<Agent> {
    const created = await this.api.send<{ agent: Agent }>('POST', `/teams/${teamId}/agents`, {
      display_name: displayName,
      role: 'member',
    });
    return created.agent;
  }

  async environments(workspaceId: string): Promise<Environment[]> {
    return this.api.list<Environment>(`/workspaces/${workspaceId}/environments`);
  }

  createEnvironment(
    workspaceId: string,
    environment: { display_name: string; tier: Tier; execution_enabled: boolean },
  ): Promise<Environment> {
    return this.api.send('POST', `/workspaces/${workspaceId}/environments`, environment);
  }

  async lifecycles(workspaceId: string): Promise<Lifecycle[]> {
    return this.api.list<Lifecycle>(`/workspaces/${workspaceId}/lifecycles`);
  }

  createLifecycle(
    workspaceId: string,
    lifecycle: {
      display_name: string;
      description?: string;
      phases: { name: string; environment_ids: string[] }[];
    },
  ): Promise<Lifecycle> {
    return this.api.send('POST', `/workspaces/${workspaceId}/lifecycles`, lifecycle);
  }

  createTarget(
    environmentId: string,
    target: { display_name: string; cluster: string; namespace: string },
  ): Promise<Target> {
    return this.api.send('POST', `/environments/${environmentId}/targets`, {
      display_name: target.display_name,
      runtime: 'kubernetes',
      locator: { cluster: target.cluster, namespace: target.namespace },
    });
  }
}
