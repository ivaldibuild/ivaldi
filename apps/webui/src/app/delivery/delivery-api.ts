import { inject, Service } from '@angular/core';
import { ApiClient } from '../api/api-client';
import {
  Application,
  Artifact,
  ArtifactKind,
  Channel,
  Environment,
  Lifecycle,
  Placement,
  Project,
  Target,
  Team,
  Workspace,
} from '../api/models';

/** The delivery configuration operations: Applications and where they run. */
@Service()
export class DeliveryApi {
  private readonly api = inject(ApiClient);

  workspaces(): Promise<Workspace[]> {
    return this.api.get('/workspaces');
  }

  /** The Workspace's active Projects by name, up to the 100 a page holds, for labels and pickers. */
  projects(workspaceId: string): Promise<Project[]> {
    return this.api.list(`/workspaces/${workspaceId}/projects?limit=100&sort_by=display_name`);
  }

  createProject(
    workspaceId: string,
    project: { display_name: string; slug: string; owner_team_id: string; description?: string },
  ): Promise<Project> {
    return this.api.send('POST', `/workspaces/${workspaceId}/projects`, project);
  }

  teams(): Promise<Team[]> {
    return this.api.list('/teams');
  }

  applications(workspaceId: string): Promise<Application[]> {
    return this.api.list(`/workspaces/${workspaceId}/applications`);
  }

  application(id: string): Promise<Application> {
    return this.api.get(`/applications/${id}`);
  }

  async createApplication(
    projectId: string,
    application: { display_name: string; owner_team_id: string; default_lifecycle_id: string },
  ): Promise<Application> {
    const created = await this.api.send<{ application: Application }>(
      'POST',
      `/projects/${projectId}/applications`,
      application,
    );
    return created.application;
  }

  artifacts(applicationId: string): Promise<Artifact[]> {
    return this.api.get(`/applications/${applicationId}/artifacts`);
  }

  createArtifact(
    applicationId: string,
    artifact: {
      name: string;
      artifact_kind: ArtifactKind;
      artifact_name: string;
      required: boolean;
    },
  ): Promise<Artifact> {
    return this.api.send('POST', `/applications/${applicationId}/artifacts`, artifact);
  }

  channels(applicationId: string): Promise<Channel[]> {
    return this.api.get(`/applications/${applicationId}/channels`);
  }

  placements(applicationId: string): Promise<Placement[]> {
    return this.api.get(`/applications/${applicationId}/placements`);
  }

  createPlacement(applicationId: string, targetId: string): Promise<Placement> {
    return this.api.send('POST', `/applications/${applicationId}/placements`, {
      target_id: targetId,
    });
  }

  environments(workspaceId: string): Promise<Environment[]> {
    return this.api.list(`/workspaces/${workspaceId}/environments`);
  }

  targets(environmentId: string): Promise<Target[]> {
    return this.api.list(`/environments/${environmentId}/targets`);
  }

  /** Every Target of the given Environments, one call each. */
  async targetsOf(environments: Environment[]): Promise<Target[]> {
    return (await Promise.all(environments.map((e) => this.targets(e.id)))).flat();
  }

  lifecycles(workspaceId: string): Promise<Lifecycle[]> {
    return this.api.list(`/workspaces/${workspaceId}/lifecycles`);
  }
}

/** A slug from a display name, as the control plane accepts: lower-case words joined by hyphens. */
export function slugify(name: string): string {
  return name
    .toLowerCase()
    .normalize('NFKD')
    .replace(/[^a-z0-9]+/g, '-')
    .replace(/^-+|-+$/g, '')
    .slice(0, 63);
}
