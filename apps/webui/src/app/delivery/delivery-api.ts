import { inject, Service } from '@angular/core';
import { ApiClient } from '../api/api-client';
import {
  Application,
  ApplyConfig,
  Artifact,
  ArtifactKind,
  Channel,
  CursorPage,
  Deployment,
  DeploymentDetail,
  DeploymentStatus,
  Environment,
  Lifecycle,
  Placement,
  Project,
  Release,
  ReleaseChanges,
  ReleaseStatus,
  Target,
  Team,
  Tier,
  Workspace,
  WorkspaceRunsOn,
} from '../api/models';

/** What an Environment is made from; the slug is derived from the name when left out. */
export interface EnvironmentFields {
  display_name: string;
  slug?: string;
  tier: Tier;
  /** Whether Ivaldi deploys there, rather than only tracking what runs there. */
  execution_enabled: boolean;
}

/** A Kubernetes Target: its name, and the cluster and namespace it is. */
export interface TargetFields {
  display_name: string;
  cluster: string;
  namespace: string;
}

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

  /**
   * Where the Workspace's Applications run: their Project and owning Team, and the Targets each is
   * placed on by Environment with the version and status current there. It follows every page, so
   * the result holds all the Applications; the Environments are the last page's.
   */
  async runsOn(
    workspaceId: string,
    options: { projectId?: string } = {},
  ): Promise<WorkspaceRunsOn> {
    const items: WorkspaceRunsOn['items'] = [];
    let cursor: string | null = null;
    let page: WorkspaceRunsOn;
    do {
      const query = new URLSearchParams({ limit: '100' });
      if (options.projectId) {
        query.set('project_id', options.projectId);
      }
      if (cursor) {
        query.set('next_cursor', cursor);
      }
      page = await this.api.get<WorkspaceRunsOn>(`/workspaces/${workspaceId}/runs-on?${query}`);
      items.push(...page.items);
      cursor = page.has_next ? page.next_cursor : null;
    } while (cursor);
    return { ...page, items, total_count: items.length, has_next: false, next_cursor: null };
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

  /** Sets how Releases are applied on the Target, or with null, leaves Deployments there tracked only. */
  configurePlacement(
    applicationId: string,
    targetId: string,
    applyConfig: ApplyConfig | null,
  ): Promise<Placement> {
    return this.api.send(
      'PUT',
      `/applications/${applicationId}/placements/${targetId}`,
      applyConfig ? { apply_config: applyConfig } : {},
    );
  }

  /** A page of an Application's Deployments, newest first. */
  deployments(
    applicationId: string,
    options: {
      status?: DeploymentStatus;
      releaseId?: string;
      cursor?: string | null;
      limit?: number;
    } = {},
  ): Promise<CursorPage<Deployment>> {
    const query = new URLSearchParams({
      limit: String(options.limit ?? 20),
      sort_by: 'requested_at',
      sort_order: 'desc',
    });
    if (options.status) {
      query.set('status', options.status);
    }
    if (options.releaseId) {
      query.set('release_id', options.releaseId);
    }
    if (options.cursor) {
      query.set('next_cursor', options.cursor);
    }
    return this.api.get(`/applications/${applicationId}/deployments?${query}`);
  }

  deployment(id: string): Promise<DeploymentDetail> {
    return this.api.get(`/deployments/${id}`);
  }

  /** Cancels a Deployment that hasn't started applying. */
  cancelDeployment(id: string): Promise<DeploymentDetail> {
    return this.api.send('POST', `/deployments/${id}/cancel`);
  }

  /** What the Release would change on the Target, and the checks a Deployment there must pass. */
  releaseChanges(releaseId: string, targetId: string): Promise<ReleaseChanges> {
    return this.api.get(
      `/releases/${releaseId}/changes?${new URLSearchParams({ target_id: targetId })}`,
    );
  }

  /** Deploys a published Release to Targets: one Deployment each, applying or awaiting approval. */
  /**
   * `latestSecrets` makes a rollback among them read the current secrets instead of the versions
   * the Release last ran with on the Target.
   */
  requestDeployments(
    releaseId: string,
    targetIds: string[],
    latestSecrets = false,
  ): Promise<Deployment[]> {
    return this.api.send('POST', `/releases/${releaseId}/deployments`, {
      target_ids: targetIds,
      ...(latestSecrets ? { secrets: 'latest' } : {}),
    });
  }

  createPlacement(applicationId: string, targetId: string): Promise<Placement> {
    return this.api.send('POST', `/applications/${applicationId}/placements`, {
      target_id: targetId,
    });
  }

  /** A page of an Application's Releases, newest first. */
  releases(
    applicationId: string,
    options: { status?: ReleaseStatus; cursor?: string | null; limit?: number } = {},
  ): Promise<CursorPage<Release>> {
    const query = new URLSearchParams({
      limit: String(options.limit ?? 20),
      sort_by: 'created_at',
      sort_order: 'desc',
    });
    if (options.status) {
      query.set('status', options.status);
    }
    if (options.cursor) {
      query.set('next_cursor', options.cursor);
    }
    return this.api.get(`/applications/${applicationId}/releases?${query}`);
  }

  release(id: string): Promise<Release> {
    return this.api.get(`/releases/${id}`);
  }

  /**
   * Creates a Release from one Build per artifact, given by digest. Without a Channel, the
   * Release joins the one Channel whose rules it meets.
   */
  createRelease(
    applicationId: string,
    release: {
      version: string;
      channel_id?: string;
      builds: { artifact: string; digest: string }[];
      publish: boolean;
    },
  ): Promise<Release> {
    return this.api.send('POST', `/applications/${applicationId}/releases`, release);
  }

  publishRelease(id: string): Promise<Release> {
    return this.api.send('POST', `/releases/${id}/publish`);
  }

  withdrawRelease(id: string): Promise<Release> {
    return this.api.send('POST', `/releases/${id}/withdraw`);
  }

  /** The Workspace's active Environments, and archived ones too when `includeArchived` is set. */
  environments(workspaceId: string, includeArchived = false): Promise<Environment[]> {
    const query = includeArchived ? '?include_archived=true' : '';
    return this.api.list(`/workspaces/${workspaceId}/environments${query}`);
  }

  environment(id: string): Promise<Environment> {
    return this.api.get(`/environments/${id}`);
  }

  createEnvironment(workspaceId: string, environment: EnvironmentFields): Promise<Environment> {
    return this.api.send('POST', `/workspaces/${workspaceId}/environments`, environment);
  }

  updateEnvironment(id: string, changes: Partial<EnvironmentFields>): Promise<Environment> {
    return this.api.send('PATCH', `/environments/${id}`, changes);
  }

  /** Archives an Environment. The control plane refuses while Targets or Lifecycles use it. */
  archiveEnvironment(id: string): Promise<Environment> {
    return this.api.send('POST', `/environments/${id}/archive`);
  }

  restoreEnvironment(id: string): Promise<Environment> {
    return this.api.send('POST', `/environments/${id}/restore`);
  }

  /** The Environment's active Targets, and archived ones too when `includeArchived` is set. */
  targets(environmentId: string, includeArchived = false): Promise<Target[]> {
    const query = includeArchived ? '?include_archived=true' : '';
    return this.api.list(`/environments/${environmentId}/targets${query}`);
  }

  /** Every Target of the given Environments, one call each. */
  async targetsOf(environments: Environment[]): Promise<Target[]> {
    return (await Promise.all(environments.map((e) => this.targets(e.id)))).flat();
  }

  /** Creates a Kubernetes Target: a namespace in a cluster. */
  createTarget(environmentId: string, target: TargetFields): Promise<Target> {
    return this.api.send('POST', `/environments/${environmentId}/targets`, {
      display_name: target.display_name,
      runtime: 'kubernetes',
      locator: { cluster: target.cluster, namespace: target.namespace },
    });
  }

  /** Renames a Target and replaces its locator. */
  updateTarget(id: string, target: TargetFields): Promise<Target> {
    return this.api.send('PATCH', `/targets/${id}`, {
      display_name: target.display_name,
      locator: { cluster: target.cluster, namespace: target.namespace },
    });
  }

  archiveTarget(id: string): Promise<Target> {
    return this.api.send('POST', `/targets/${id}/archive`);
  }

  restoreTarget(id: string): Promise<Target> {
    return this.api.send('POST', `/targets/${id}/restore`);
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
