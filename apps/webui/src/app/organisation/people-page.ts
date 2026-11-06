import { Component, computed, effect, inject, signal, untracked } from '@angular/core';
import { NgIcon, provideIcons } from '@ng-icons/core';
import { lucideSearch } from '@ng-icons/lucide';
import { HlmAlertImports } from '@spartan-ng/helm/alert';
import { HlmBadgeImports } from '@spartan-ng/helm/badge';
import { HlmButtonImports } from '@spartan-ng/helm/button';
import { HlmInputImports } from '@spartan-ng/helm/input';
import { HlmSpinnerImports } from '@spartan-ng/helm/spinner';
import { ApiError } from '../api/api-client';
import { CurrentUser } from '../api/current-user';
import { BuiltInRole, Grant, Person } from '../api/models';
import { PeopleApi, roleLabels, strongestRole } from '../api/people-api';
import { PageHeader } from '../delivery/page-header';

interface Row {
  person: Person;
  /** The strongest role held on the Organisation, directly or through a group. */
  role: BuiltInRole | null;
  /** The person's own grants on the Organisation, which a role change replaces. */
  direct: Grant[];
  /** The role comes only through a group, such as a Team, so it can't be changed here. */
  throughGroup: boolean;
}

const searchDelay = 300;

/** The Organisation's people, and their roles on it for owners and admins to change. */
@Component({
  selector: 'app-people-page',
  imports: [
    NgIcon,
    PageHeader,
    HlmAlertImports,
    HlmBadgeImports,
    HlmButtonImports,
    HlmInputImports,
    HlmSpinnerImports,
  ],
  providers: [provideIcons({ lucideSearch })],
  templateUrl: './people-page.html',
})
export class PeoplePage {
  private readonly api = inject(PeopleApi);
  protected readonly user = inject(CurrentUser);

  protected readonly search = signal('');
  private readonly query = signal('');
  private searchTimer: ReturnType<typeof setTimeout> | undefined;

  private readonly people = signal<Person[]>([]);
  private readonly grants = signal<Grant[]>([]);
  private nextCursor: string | null = null;
  protected readonly hasMore = signal(false);
  protected readonly total = signal(0);

  protected readonly loading = signal(true);
  protected readonly loadingMore = signal(false);
  protected readonly changing = signal<string | null>(null);
  protected readonly error = signal<string | null>(null);

  protected readonly rows = computed<Row[]>(() =>
    this.people().map((person) => {
      const direct = this.grants().filter((g) => g.principal?.id === person.id);
      const role = strongestRole(person.roles);
      return { person, role, direct, throughGroup: !!role && strongestRole(direct) !== role };
    }),
  );

  /** Roles this person may hand out: owner only by owners. */
  protected readonly assignable = computed<BuiltInRole[]>(() =>
    this.user.role() === 'owner'
      ? ['owner', 'admin', 'member', 'viewer']
      : ['admin', 'member', 'viewer'],
  );

  protected readonly roleLabels = roleLabels;

  constructor() {
    effect(() => {
      const organizationId = this.user.organization()?.id;
      this.query();
      if (organizationId) {
        untracked(() => void this.load(true));
      }
    });
  }

  protected onSearch(value: string): void {
    this.search.set(value);
    clearTimeout(this.searchTimer);
    this.searchTimer = setTimeout(() => this.query.set(value), searchDelay);
  }

  protected loadMore(): void {
    void this.load(false);
  }

  /** Whether the signed-in person may change this row's role. */
  protected canChange(row: Row): boolean {
    if (!this.user.canManageOrganization() || row.person.id === this.user.profile()?.principal.id) {
      return false;
    }
    return row.role !== 'owner' || this.user.role() === 'owner';
  }

  protected async changeRole(row: Row, role: BuiltInRole): Promise<void> {
    const organizationId = this.user.organization()?.id;
    if (!organizationId || role === row.role) {
      return;
    }
    this.changing.set(row.person.id);
    this.error.set(null);
    try {
      // Grant the new role before removing the old ones, so the person is never left without
      // one and the control plane can refuse to remove the last owner.
      if (!row.direct.some((g) => g.role === role)) {
        await this.api.grantRole(row.person.id, role, organizationId);
      }
      for (const grant of row.direct.filter((g) => g.role !== role)) {
        await this.api.removeGrant(grant.id);
      }
    } catch (e) {
      this.error.set(e instanceof ApiError ? e.message : 'The role was not changed.');
    } finally {
      this.changing.set(null);
      await this.load(true);
    }
  }

  protected lastSignedIn(person: Person): string {
    if (!person.last_signed_in_at) {
      return 'Never';
    }
    return new Intl.DateTimeFormat('en-GB', {
      day: 'numeric',
      month: 'short',
      year: 'numeric',
    }).format(new Date(person.last_signed_in_at));
  }

  private async load(reset: boolean): Promise<void> {
    const organizationId = this.user.organization()?.id;
    if (!organizationId) {
      return;
    }
    (reset ? this.loading : this.loadingMore).set(true);
    try {
      const [page, grants] = await Promise.all([
        this.api.people({ search: this.query(), cursor: reset ? null : this.nextCursor }),
        reset ? this.api.grants(organizationId) : Promise.resolve(this.grants()),
      ]);
      this.people.update((current) => (reset ? page.items : [...current, ...page.items]));
      this.grants.set(grants);
      this.nextCursor = page.next_cursor;
      this.hasMore.set(page.has_next);
      this.total.set(page.total_count);
    } catch (e) {
      this.error.set(e instanceof ApiError ? e.message : "People didn't load.");
    } finally {
      this.loading.set(false);
      this.loadingMore.set(false);
    }
  }
}
