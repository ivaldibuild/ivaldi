import { HttpClient, HttpErrorResponse } from '@angular/common/http';
import { inject, Service } from '@angular/core';
import { firstValueFrom, map, Observable } from 'rxjs';
import { API_BASE_URL } from '../api-base-url';

interface Envelope<T> {
  status: string;
  data: T;
}

interface Page<T> {
  items: T[];
}

export interface Workspace {
  id: string;
  slug: string;
  display_name: string;
  is_default: boolean;
}

export type TeamRole = 'lead' | 'member';

export interface TeamMember {
  principal_id: string;
  principal_kind: string;
  role: TeamRole;
}

export interface Team {
  id: string;
  slug: string;
  display_name: string;
  members: TeamMember[];
}

export interface Agent {
  id: string;
  display_name: string;
}

export type Tier = 'development' | 'staging' | 'production' | 'other';

export interface Environment {
  id: string;
  slug: string;
  display_name: string;
  tier: Tier;
  execution_enabled: boolean;
}

export interface LifecyclePhase {
  id: string;
  name: string;
  position: number;
  environment_ids: string[];
}

export interface Lifecycle {
  id: string;
  slug: string;
  display_name: string;
  phases: LifecyclePhase[];
}

export interface Target {
  id: string;
  environment_id: string;
  slug: string;
  display_name: string;
  locator: Record<string, string>;
}

/** A refusal from the control plane, carrying its `application/problem+json` detail. */
export class ApiError extends Error {
  readonly status: number;

  constructor(status: number, message: string) {
    super(message);
    this.status = status;
  }
}

/** The control-plane operations the first-run setup uses. */
@Service()
export class OnboardingApi {
  private readonly http = inject(HttpClient);
  private readonly base = `${inject(API_BASE_URL)}/v1`;

  async defaultWorkspace(): Promise<Workspace | null> {
    const workspaces = await this.get<Workspace[]>('/workspaces');
    return workspaces.find((w) => w.is_default) ?? workspaces[0] ?? null;
  }

  updateWorkspace(id: string, change: { display_name: string; slug: string }): Promise<Workspace> {
    return this.send('PATCH', `/workspaces/${id}`, change);
  }

  async teams(): Promise<Team[]> {
    return (await this.get<Page<Team>>('/teams')).items;
  }

  createTeam(team: { display_name: string; slug?: string }): Promise<Team> {
    return this.send('POST', '/teams', team);
  }

  setTeamMember(teamId: string, principalId: string, role: TeamRole): Promise<unknown> {
    return this.send('PUT', `/teams/${teamId}/members/${principalId}`, { role });
  }

  async createAgent(teamId: string, displayName: string): Promise<Agent> {
    const created = await this.send<{ agent: Agent }>('POST', `/teams/${teamId}/agents`, {
      display_name: displayName,
      role: 'member',
    });
    return created.agent;
  }

  async environments(workspaceId: string): Promise<Environment[]> {
    return (await this.get<Page<Environment>>(`/workspaces/${workspaceId}/environments`)).items;
  }

  createEnvironment(
    workspaceId: string,
    environment: { display_name: string; tier: Tier; execution_enabled: boolean },
  ): Promise<Environment> {
    return this.send('POST', `/workspaces/${workspaceId}/environments`, environment);
  }

  async lifecycles(workspaceId: string): Promise<Lifecycle[]> {
    return (await this.get<Page<Lifecycle>>(`/workspaces/${workspaceId}/lifecycles`)).items;
  }

  createLifecycle(
    workspaceId: string,
    lifecycle: {
      display_name: string;
      description?: string;
      phases: { name: string; environment_ids: string[] }[];
    },
  ): Promise<Lifecycle> {
    return this.send('POST', `/workspaces/${workspaceId}/lifecycles`, lifecycle);
  }

  createTarget(
    environmentId: string,
    target: { display_name: string; cluster: string; namespace: string },
  ): Promise<Target> {
    return this.send('POST', `/environments/${environmentId}/targets`, {
      display_name: target.display_name,
      runtime: 'kubernetes',
      locator: { cluster: target.cluster, namespace: target.namespace },
    });
  }

  private get<T>(path: string): Promise<T> {
    return unwrap(this.http.get<Envelope<T>>(`${this.base}${path}`));
  }

  private send<T>(method: 'POST' | 'PUT' | 'PATCH', path: string, body: unknown): Promise<T> {
    return unwrap(this.http.request<Envelope<T>>(method, `${this.base}${path}`, { body }));
  }
}

async function unwrap<T>(request: Observable<Envelope<T>>): Promise<T> {
  try {
    return await firstValueFrom(request.pipe(map((envelope) => envelope.data)));
  } catch (error) {
    throw toApiError(error);
  }
}

function toApiError(error: unknown): ApiError {
  if (!(error instanceof HttpErrorResponse)) {
    return new ApiError(0, 'Something went wrong. Try again.');
  }
  if (error.status === 0) {
    return new ApiError(0, "Ivaldi can't be reached. Check your connection and try again.");
  }
  if (error.status === 403) {
    return new ApiError(403, "You don't have permission to do this. Ask an owner.");
  }
  const problem = error.error as { detail?: string; errors?: { message?: string }[] } | null;
  const detail = problem?.errors?.[0]?.message ?? problem?.detail;
  if (error.status < 500 && detail) {
    return new ApiError(error.status, sentence(detail));
  }
  return new ApiError(error.status, 'Something went wrong on our side. Try again.');
}

function sentence(text: string): string {
  const trimmed = text.trim();
  const capitalised = trimmed.charAt(0).toUpperCase() + trimmed.slice(1);
  return /[.!?]$/.test(capitalised) ? capitalised : `${capitalised}.`;
}
