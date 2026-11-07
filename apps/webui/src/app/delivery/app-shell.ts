import { Component, computed, inject, signal } from '@angular/core';
import { toSignal } from '@angular/core/rxjs-interop';
import { NavigationEnd, Router, RouterLink, RouterOutlet } from '@angular/router';
import { filter, map } from 'rxjs';
import { NgIcon, provideIcons } from '@ng-icons/core';
import {
  lucideBuilding2,
  lucideCheck,
  lucideChevronDown,
  lucideChevronsUpDown,
  lucideFolder,
  lucideGitBranch,
  lucideLayers,
  lucideLogOut,
  lucideMoon,
  lucideSettings,
  lucideSun,
  lucideUsers,
} from '@ng-icons/lucide';
import { HlmCollapsibleImports } from '@spartan-ng/helm/collapsible';
import { HlmDropdownMenuImports } from '@spartan-ng/helm/dropdown-menu';
import { HlmSidebarImports, HlmSidebarService } from '@spartan-ng/helm/sidebar';
import { CurrentUser } from '../api/current-user';
import { Session } from '../auth/session';
import { UserBadge } from '../shared/identity';
import { Theme } from '../shared/theme';
import { WorkspaceContext } from './workspace-context';

interface NavItem {
  path: string;
  label: string;
  icon: string;
  within?: string[];
}

/**
 * The signed-in app: Spartan's collapsible sidebar, with the Organisation, the Workspace switcher
 * and the signed-in person, beside the current page. It folds to icons on wide screens and opens
 * as a sheet on narrow ones.
 */
@Component({
  selector: 'app-shell',
  imports: [
    RouterOutlet,
    RouterLink,
    NgIcon,
    HlmCollapsibleImports,
    HlmDropdownMenuImports,
    HlmSidebarImports,
    UserBadge,
  ],
  providers: [
    provideIcons({
      lucideBuilding2,
      lucideCheck,
      lucideChevronDown,
      lucideChevronsUpDown,
      lucideFolder,
      lucideGitBranch,
      lucideLayers,
      lucideLogOut,
      lucideMoon,
      lucideSettings,
      lucideSun,
      lucideUsers,
    }),
  ],
  templateUrl: './app-shell.html',
})
export class AppShell {
  private readonly session = inject(Session);
  protected readonly context = inject(WorkspaceContext);
  protected readonly user = inject(CurrentUser);
  protected readonly theme = inject(Theme);
  private readonly sidebar = inject(HlmSidebarService);
  private readonly router = inject(Router);

  /** The current URL, for the items that also cover pages outside their own path. */
  private readonly url = toSignal(
    this.router.events.pipe(
      filter((e) => e instanceof NavigationEnd),
      map(() => this.router.url),
    ),
    { initialValue: this.router.url },
  );

  /** `within`: other pages that sit under the item, such as a Project's Applications. */
  protected readonly groups: { label: string; items: NavItem[] }[] = [
    {
      label: 'Delivery',
      items: [
        {
          path: '/projects',
          label: 'Projects',
          icon: 'lucideFolder',
          within: ['/applications', '/releases'],
        },
        { path: '/environments', label: 'Environments', icon: 'lucideLayers' },
        { path: '/lifecycles', label: 'Lifecycles', icon: 'lucideGitBranch' },
      ],
    },
    {
      label: 'Organisation',
      items: [
        { path: '/people', label: 'People & access', icon: 'lucideUsers' },
        { path: '/setup', label: 'Setup', icon: 'lucideSettings' },
      ],
    },
  ];

  protected readonly workspaceLabel = computed(
    () => `${this.context.current()?.display_name ?? 'No'} Workspace`,
  );
  protected readonly organisationInitial = computed(
    () => this.user.organization()?.display_name.trim().charAt(0).toUpperCase() ?? '',
  );

  /** The groups folded away; folded to icons, the sidebar shows every group's items. */
  private readonly closedGroups = signal<ReadonlySet<string>>(new Set());

  protected isOpen(label: string): boolean {
    return this.sidebar.state() === 'collapsed' || !this.closedGroups().has(label);
  }

  protected setOpen(label: string, open: boolean): void {
    this.closedGroups.update((closed) => {
      const next = new Set(closed);
      if (open) {
        next.delete(label);
      } else {
        next.add(label);
      }
      return next;
    });
  }

  constructor() {
    void this.context.load().catch(() => undefined);
  }

  /**
   * Whether a sidebar item is the current page or one of the pages under it. Worked out here
   * rather than by RouterLinkActive, which removes the aria-current this sets.
   */
  protected isActive(item: NavItem): boolean {
    const url = this.url().split(/[?#]/)[0];
    return [item.path, ...(item.within ?? [])].some((p) => url === p || url.startsWith(p + '/'));
  }

  protected signOut(): void {
    this.session.signOut();
  }
}
