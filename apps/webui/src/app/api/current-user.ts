import { computed, inject, resource, Service } from '@angular/core';
import { ApiClient } from './api-client';
import { Me } from './models';

/** Built-in roles, strongest first. */
const roles = ['owner', 'admin', 'member', 'viewer'] as const;
const roleLabels: Record<(typeof roles)[number], string> = {
  owner: 'Owner',
  admin: 'Admin',
  member: 'Member',
  viewer: 'Viewer',
};

/** The signed-in person and their Organisation, loaded once from `GET /v1/me`. */
@Service()
export class CurrentUser {
  private readonly api = inject(ApiClient);

  private readonly me = resource({ loader: () => this.api.get<Me>('/me') });

  readonly profile = computed(() => (this.me.hasValue() ? this.me.value() : null));
  readonly displayName = computed(() => this.profile()?.principal.display_name ?? '');
  readonly firstName = computed(() => this.displayName().split(/\s+/)[0] ?? '');
  readonly email = computed(() => this.profile()?.principal.email ?? null);
  readonly organization = computed(() => this.profile()?.organization ?? null);

  /** The strongest built-in role the person holds on the Organisation itself. */
  readonly role = computed(() => {
    const me = this.profile();
    if (!me) {
      return null;
    }
    const held = new Set(
      me.roles.filter((r) => r.resource_id === me.organization.id).map((r) => r.role),
    );
    return roles.find((r) => held.has(r)) ?? null;
  });

  readonly roleLabel = computed(() => {
    const role = this.role();
    return role ? roleLabels[role] : '';
  });

  /** Owners and admins may change the Organisation (`organization.manage`). */
  readonly canManageOrganization = computed(() => ['owner', 'admin'].includes(this.role() ?? ''));

  readonly initials = computed(() =>
    this.displayName()
      .split(/\s+/)
      .filter(Boolean)
      .slice(0, 2)
      .map((part) => part[0]!.toUpperCase())
      .join(''),
  );

  /** Loads `/me` again, such as after the Organisation is renamed. */
  reload(): void {
    this.me.reload();
  }
}
