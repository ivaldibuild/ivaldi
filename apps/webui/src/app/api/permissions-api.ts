import { inject, Service } from '@angular/core';
import { ApiClient } from './api-client';
import { MyPermissions } from './models';

/**
 * What the signed-in person may do on a resource, so a page offers only that. The control plane
 * still decides every operation for itself.
 */
@Service()
export class PermissionsApi {
  private readonly api = inject(ApiClient);

  /** Those of `permissions` the person holds on the resource, such as a Project. */
  async granted(resourceId: string, permissions: string[]): Promise<Set<string>> {
    const query = new URLSearchParams({ resource_id: resourceId });
    for (const permission of permissions) {
      query.append('permission', permission);
    }
    const answer: MyPermissions = await this.api.get(`/me/permissions?${query}`);
    return new Set(answer.granted);
  }
}
