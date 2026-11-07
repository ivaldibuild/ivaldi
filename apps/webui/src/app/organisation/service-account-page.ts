import {
  Component,
  computed,
  DOCUMENT,
  effect,
  inject,
  input,
  signal,
  untracked,
} from '@angular/core';
import { form, FormField, required, submit } from '@angular/forms/signals';
import { RouterLink } from '@angular/router';
import { HlmAlertImports } from '@spartan-ng/helm/alert';
import { HlmBadgeImports } from '@spartan-ng/helm/badge';
import { HlmButtonImports } from '@spartan-ng/helm/button';
import { HlmFieldImports } from '@spartan-ng/helm/field';
import { HlmInputImports } from '@spartan-ng/helm/input';
import { HlmSpinnerImports } from '@spartan-ng/helm/spinner';
import { CurrentUser } from '../api/current-user';
import {
  ApiToken,
  BuiltInRole,
  Grant,
  IssuedApiToken,
  ServiceAccount,
  Workspace,
} from '../api/models';
import { PeopleApi, roleLabels } from '../api/people-api';
import { ServiceAccountsApi } from '../api/service-accounts-api';
import { PageHeader } from '../delivery/page-header';
import { WorkspaceContext } from '../delivery/workspace-context';
import { formatDay, formatDayAndTime } from '../shared/dates';
import { RequestStatus } from '../shared/request-status';

/** How long a new token lasts. Tokens that never expire are allowed, but not the default. */
const lifetimes = [
  { value: '30', label: '30 days' },
  { value: '90', label: '90 days' },
  { value: '365', label: '1 year' },
  { value: 'never', label: 'No expiry' },
] as const;
type Lifetime = (typeof lifetimes)[number]['value'];

const dayInMs = 24 * 60 * 60 * 1000;

/** The roles a service account may be given. Owner stays with people. */
const grantableRoles: BuiltInRole[] = ['admin', 'member', 'viewer'];

interface AccessRow {
  grant: Grant;
  /** The role's name, such as Admin. */
  role: string;
  /** Where the role applies: the Organisation, or a Workspace's name. */
  scope: string;
}

type TokenState = 'active' | 'expired' | 'revoked';

/**
 * One service account: suspending and reactivating it, the roles it acts with, and its API
 * tokens. A token's secret is shown once, when it is issued.
 */
@Component({
  selector: 'app-service-account-page',
  imports: [
    FormField,
    PageHeader,
    RouterLink,
    HlmAlertImports,
    HlmBadgeImports,
    HlmButtonImports,
    HlmFieldImports,
    HlmInputImports,
    HlmSpinnerImports,
  ],
  templateUrl: './service-account-page.html',
})
export class ServiceAccountPage {
  readonly id = input.required<string>();

  private readonly api = inject(ServiceAccountsApi);
  private readonly people = inject(PeopleApi);
  private readonly workspaceContext = inject(WorkspaceContext);
  private readonly document = inject(DOCUMENT);
  protected readonly user = inject(CurrentUser);

  protected readonly status = new RequestStatus();
  protected readonly account = signal<ServiceAccount | null>(null);
  protected readonly tokens = signal<ApiToken[]>([]);
  private readonly grants = signal<Grant[]>([]);
  protected readonly workspaces = signal<Workspace[]>([]);

  protected readonly canManage = this.user.canManageOrganization;
  protected readonly roleLabels = roleLabels;
  protected readonly grantableRoles = grantableRoles;
  protected readonly lifetimes = lifetimes;
  protected readonly formatDay = formatDay;
  protected readonly formatDayAndTime = formatDayAndTime;

  protected readonly access = computed<AccessRow[]>(() => {
    const organizationId = this.user.organization()?.id;
    const names = new Map(this.workspaces().map((w) => [w.id, w.display_name]));
    return this.grants().map((grant) => ({
      grant,
      role: roleLabels[grant.role as BuiltInRole] ?? grant.role,
      scope:
        grant.resource_id === organizationId
          ? 'Organisation'
          : `Workspace ${names.get(grant.resource_id) ?? grant.resource_id}`,
    }));
  });

  // Suspending and reactivating.
  protected readonly statusChange = new RequestStatus();

  // Granting a role.
  protected readonly grantStatus = new RequestStatus();
  protected readonly grantRole = signal<BuiltInRole>('admin');
  protected readonly grantScope = signal('');
  protected readonly removing = signal<string | null>(null);

  // Issuing a token.
  protected readonly issuing = signal(false);
  protected readonly tokenStatus = new RequestStatus();
  private readonly tokenModel = signal({ name: '', lifetime: '90' as Lifetime });
  protected readonly tokenForm = form(this.tokenModel, (path) => {
    required(path.name, {
      message: 'Name the token after where it is used, such as github-actions.',
    });
  });
  /** The token just issued, with its secret, until it is dismissed. */
  protected readonly issued = signal<IssuedApiToken | null>(null);
  protected readonly copied = signal(false);
  protected readonly revoking = signal<string | null>(null);

  constructor() {
    effect(() => {
      const id = this.id();
      const organizationId = this.user.organization()?.id;
      if (organizationId) {
        untracked(() => void this.load(id, organizationId));
      }
    });
  }

  protected tokenState(token: ApiToken): TokenState {
    if (token.revoked_at) {
      return 'revoked';
    }
    if (token.expires_at && new Date(token.expires_at).getTime() <= Date.now()) {
      return 'expired';
    }
    return 'active';
  }

  protected async suspend(): Promise<void> {
    await this.statusChange.run(async () => this.account.set(await this.api.suspend(this.id())));
  }

  protected async reactivate(): Promise<void> {
    await this.statusChange.run(async () => this.account.set(await this.api.reactivate(this.id())));
  }

  protected async addGrant(event: Event): Promise<void> {
    event.preventDefault();
    const resourceId = this.grantScope() || this.user.organization()?.id;
    if (!resourceId) {
      return;
    }
    const added = await this.grantStatus.run(() =>
      this.people.grantRole(this.id(), this.grantRole(), resourceId),
    );
    if (added) {
      await this.loadGrants();
    }
  }

  protected async removeGrant(grant: Grant): Promise<void> {
    this.removing.set(grant.id);
    const removed = await this.grantStatus.run(() => this.people.removeGrant(grant.id));
    this.removing.set(null);
    if (removed) {
      await this.loadGrants();
    }
  }

  protected async issueToken(event: Event): Promise<void> {
    event.preventDefault();
    await submit(this.tokenForm, async () => {
      const { name, lifetime } = this.tokenModel();
      const body: { name: string; expires_at?: string } = { name: name.trim() };
      if (lifetime !== 'never') {
        body.expires_at = new Date(Date.now() + Number(lifetime) * dayInMs).toISOString();
      }
      let issued: IssuedApiToken | undefined;
      const ok = await this.tokenStatus.run(async () => {
        issued = await this.api.issueToken(this.id(), body);
      });
      if (ok && issued) {
        this.issued.set(issued);
        this.copied.set(false);
        this.issuing.set(false);
        this.tokenForm().reset({ name: '', lifetime: '90' });
        this.tokens.update((tokens) => [issued!.token, ...tokens]);
      }
      return undefined;
    });
  }

  protected cancelIssue(): void {
    this.issuing.set(false);
    this.tokenStatus.error.set(null);
    this.tokenForm().reset({ name: '', lifetime: '90' });
  }

  protected async copySecret(): Promise<void> {
    const secret = this.issued()?.secret;
    const clipboard = this.document.defaultView?.navigator.clipboard;
    if (!secret || !clipboard) {
      return;
    }
    try {
      await clipboard.writeText(secret);
      this.copied.set(true);
    } catch {
      // The secret stays on screen to copy by hand.
    }
  }

  protected async revoke(token: ApiToken): Promise<void> {
    this.revoking.set(token.id);
    await this.tokenStatus.run(async () => {
      const revoked = await this.api.revokeToken(token.id);
      this.tokens.update((tokens) => tokens.map((t) => (t.id === revoked.id ? revoked : t)));
    });
    this.revoking.set(null);
  }

  private async load(id: string, organizationId: string): Promise<void> {
    await this.status.run(async () => {
      const [account, tokens] = await Promise.all([
        this.api.get(id),
        this.api.tokens(id),
        this.workspaceContext.load(),
      ]);
      this.account.set(account);
      this.tokens.set(tokens);
      this.workspaces.set(this.workspaceContext.workspaces());
      // A role on the Workspace being worked in is the narrowest that lets CI release there.
      this.grantScope.set(this.workspaceContext.current()?.id ?? organizationId);
      await this.loadGrants();
    }, 'loading');
  }

  /** The service account's grants on the Organisation and on each Workspace. */
  private async loadGrants(): Promise<void> {
    const organizationId = this.user.organization()?.id;
    if (!organizationId) {
      return;
    }
    const resources = [organizationId, ...this.workspaces().map((w) => w.id)];
    const all = await Promise.all(resources.map((r) => this.people.grants(r)));
    this.grants.set(all.flat().filter((g) => g.principal?.id === this.id()));
  }
}
