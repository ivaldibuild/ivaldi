import { inject, Service } from '@angular/core';
import { ApiClient } from '../api/api-client';
import { SecretStore, SecretStoreKind } from '../api/models';

/** What a Secret Store is made from: its kind's settings, and who may read it. */
export interface SecretStoreFields {
  display_name: string;
  kind: SecretStoreKind;
  settings?: SecretStore['settings'];
  /** The Environments whose Deployments may read the store; all of the Workspace's when empty. */
  environment_ids: string[];
}

/** The Workspace's Secret Stores: where runtime agents read secret variables from. */
@Service()
export class SecretStoresApi {
  private readonly api = inject(ApiClient);

  list(workspaceId: string): Promise<SecretStore[]> {
    return this.api.get(`/workspaces/${workspaceId}/secret-stores`);
  }

  create(workspaceId: string, store: SecretStoreFields): Promise<SecretStore> {
    return this.api.send('POST', `/workspaces/${workspaceId}/secret-stores`, store);
  }

  archive(id: string): Promise<SecretStore> {
    return this.api.send('POST', `/secret-stores/${id}/archive`);
  }
}
