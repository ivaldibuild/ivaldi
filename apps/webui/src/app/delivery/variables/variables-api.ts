import { inject, Service } from '@angular/core';
import { ApiClient } from '../../api/api-client';
import {
  Environment,
  ReleaseVariables,
  ReleaseVariablesRefresh,
  SecretStore,
  Target,
  Variable,
  VariableScope,
} from '../../api/models';
import { DeliveryApi } from '../delivery-api';
import { SecretStoresApi } from '../secret-stores/secret-stores-api';

/** The Project or the Application that owns variables. */
export type VariableOwner = { projectId: string } | { applicationId: string };

/**
 * A value as it is saved, and the Deployments it applies to: a text variable's text, or where a
 * secret variable's secret is kept.
 */
export interface ValueFields {
  value?: string;
  secret?: { store_id: string; path: string; field?: string; version?: string };
  scope: VariableScope;
}

/** What a variable is made from. */
export interface VariableFields {
  name: string;
  kind?: Variable['kind'];
  /** How a secret variable reaches the chart; a Kubernetes Secret when left out. */
  deliver_as?: 'secret' | 'value';
  values_path?: string;
  description?: string;
  values?: ValueFields[];
}

/** The variables Applications deploy with, and what their values can be scoped to. */
@Service()
export class VariablesApi {
  private readonly api = inject(ApiClient);
  private readonly delivery = inject(DeliveryApi);
  private readonly secretStores = inject(SecretStoresApi);

  /** An owner's variables. An Application's are followed by the ones its Project gives it. */
  list(owner: VariableOwner): Promise<Variable[]> {
    return 'applicationId' in owner
      ? this.api.get(`/applications/${owner.applicationId}/variables?include_inherited=true`)
      : this.api.get(`/projects/${owner.projectId}/variables`);
  }

  create(owner: VariableOwner, variable: VariableFields): Promise<Variable> {
    const path =
      'applicationId' in owner
        ? `/applications/${owner.applicationId}/variables`
        : `/projects/${owner.projectId}/variables`;
    return this.api.send('POST', path, variable);
  }

  /** Changes where the chart receives it, or its description; an empty string clears either. */
  update(
    id: string,
    changes: { values_path: string; description: string; deliver_as?: 'secret' | 'value' },
  ): Promise<Variable> {
    return this.api.send('PATCH', `/variables/${id}`, changes);
  }

  archive(id: string): Promise<Variable> {
    return this.api.send('POST', `/variables/${id}/archive`);
  }

  addValue(id: string, value: ValueFields): Promise<Variable> {
    return this.api.send('POST', `/variables/${id}/values`, value);
  }

  updateValue(id: string, valueId: string, value: ValueFields): Promise<Variable> {
    return this.api.send('PUT', `/variables/${id}/values/${valueId}`, value);
  }

  removeValue(id: string, valueId: string): Promise<Variable> {
    return this.api.send('DELETE', `/variables/${id}/values/${valueId}`);
  }

  /**
   * The variables a Release deploys with: its current snapshot and, for a Target, the value of
   * each a Deployment there would get.
   */
  releaseVariables(
    releaseId: string,
    targetId?: string,
    deploymentId?: string,
  ): Promise<ReleaseVariables> {
    const params = new URLSearchParams();
    if (targetId) params.set('target_id', targetId);
    if (deploymentId) params.set('deployment_id', deploymentId);
    const query = params.size ? `?${params}` : '';
    return this.api.get(`/releases/${releaseId}/variables${query}`);
  }

  /**
   * Takes a new revision of a Release's variables from its Application's as they are now, or with
   * `preview` only says what that would change. Refused when it would change production.
   */
  refreshReleaseVariables(releaseId: string, preview: boolean): Promise<ReleaseVariablesRefresh> {
    return this.api.send(
      'POST',
      `/releases/${releaseId}/variables/refresh${preview ? '?preview=true' : ''}`,
    );
  }

  /**
   * What a value can name: the Workspace's Environments and their Targets, which it can be scoped
   * to, and the Secret Stores a secret can be kept in. Someone who can't read the stores gets none.
   */
  async scopeOptions(
    workspaceId: string,
  ): Promise<{ environments: Environment[]; targets: Target[]; stores: SecretStore[] }> {
    const [environments, stores] = await Promise.all([
      this.delivery.environments(workspaceId),
      this.secretStores.list(workspaceId).catch(() => []),
    ]);
    return { environments, targets: await this.delivery.targetsOf(environments), stores };
  }
}
