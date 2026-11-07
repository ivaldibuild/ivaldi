import { Component, inject } from '@angular/core';
import { toSignal } from '@angular/core/rxjs-interop';
import { NavigationEnd, Router, RouterLink, RouterOutlet } from '@angular/router';
import { filter, map } from 'rxjs';
import { NgIcon, provideIcons } from '@ng-icons/core';
import {
  lucideFolder,
  lucideGitBranch,
  lucideLayers,
  lucideLogOut,
  lucideSettings,
  lucideUsers,
} from '@ng-icons/lucide';
import { HlmSidebarImports } from '@spartan-ng/helm/sidebar';
import { Session } from '../auth/session';
import { OrganisationBadge, UserBadge } from '../shared/identity';
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
  imports: [RouterOutlet, RouterLink, NgIcon, HlmSidebarImports, OrganisationBadge, UserBadge],
  providers: [
    provideIcons({
      lucideFolder,
      lucideGitBranch,
      lucideLayers,
      lucideLogOut,
      lucideSettings,
      lucideUsers,
    }),
  ],
  templateUrl: './app-shell.html',
})
export class AppShell {
  private readonly session = inject(Session);
  protected readonly context = inject(WorkspaceContext);
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

  protected selectWorkspace(event: Event): void {
    this.context.select((event.target as HTMLSelectElement).value);
  }

  protected signOut(): void {
    this.session.signOut();
  }
}
