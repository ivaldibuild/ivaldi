import { inject, Service } from '@angular/core';
import { ApiClient } from '../../api/api-client';
import { Environment, ReleaseVariables, Target, Variable, VariableScope } from '../../api/models';
import { DeliveryApi } from '../delivery-api';

/** The Project or the Application that owns variables. */
export type VariableOwner = { projectId: string } | { applicationId: string };

/** A value as it is saved: its text and the Deployments it applies to. */
export interface ValueFields {
  value: string;
  scope: VariableScope;
}

/** What a variable is made from. */
export interface VariableFields {
  name: string;
  values_path?: string;
  description?: string;
  values?: ValueFields[];
}

/** The variables Applications deploy with, and what their values can be scoped to. */
@Service()
export class VariablesApi {
  private readonly api = inject(ApiClient);
  private readonly delivery = inject(DeliveryApi);

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
  update(id: string, changes: { values_path: string; description: string }): Promise<Variable> {
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
  releaseVariables(releaseId: string, targetId?: string): Promise<ReleaseVariables> {
    const query = targetId ? `?${new URLSearchParams({ target_id: targetId })}` : '';
    return this.api.get(`/releases/${releaseId}/variables${query}`);
  }

  /** The Workspace's Environments and their Targets, which a value can be scoped to. */
  async scopeOptions(
    workspaceId: string,
  ): Promise<{ environments: Environment[]; targets: Target[] }> {
    const environments = await this.delivery.environments(workspaceId);
    return { environments, targets: await this.delivery.targetsOf(environments) };
  }
}
