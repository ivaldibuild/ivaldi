import { inject, Service } from '@angular/core';
import { ApiClient } from './api-client';
import { BuiltInRole, CursorPage, Grant, Person } from './models';

/** The Organisation's people and the roles granted to them. */
@Service()
export class PeopleApi {
  private readonly api = inject(ApiClient);

  /** A page of people, filtered by name or email when `search` is given. */
  people(
    options: { search?: string; cursor?: string | null; limit?: number } = {},
  ): Promise<CursorPage<Person>> {
    const query = new URLSearchParams({ limit: String(options.limit ?? 50) });
    if (options.search?.trim()) {
      query.set('q', options.search.trim());
    }
    if (options.cursor) {
      query.set('next_cursor', options.cursor);
    }
    return this.api.get(`/people?${query}`);
  }

  /** Every unexpired grant on a resource, following the pages. */
  async grants(resourceId: string): Promise<Grant[]> {
    const grants: Grant[] = [];
    let cursor: string | null = null;
    do {
      const query = new URLSearchParams({ resource_id: resourceId, limit: '100' });
      if (cursor) {
        query.set('next_cursor', cursor);
      }
      const page: CursorPage<Grant> = await this.api.get(`/grants?${query}`);
      grants.push(...page.items);
      cursor = page.has_next ? page.next_cursor : null;
    } while (cursor);
    return grants;
  }

  grantRole(principalId: string, role: BuiltInRole, resourceId: string): Promise<Grant> {
    return this.api.send('POST', '/grants', {
      principal_id: principalId,
      role,
      resource_id: resourceId,
    });
  }

  removeGrant(grantId: string): Promise<unknown> {
    return this.api.send('DELETE', `/grants/${grantId}`);
  }
}

const roleOrder: BuiltInRole[] = ['owner', 'admin', 'member', 'viewer'];

export const roleLabels: Record<BuiltInRole, string> = {
  owner: 'Owner',
  admin: 'Admin',
  member: 'Member',
  viewer: 'Viewer',
};

/** The strongest built-in role among `roles`, or null when there is none. */
export function strongestRole(roles: { role: string }[]): BuiltInRole | null {
  const held = new Set(roles.map((r) => r.role));
  return roleOrder.find((r) => held.has(r)) ?? null;
}
