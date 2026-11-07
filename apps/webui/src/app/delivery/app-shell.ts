import { Component, computed, DestroyRef, effect, inject, signal, untracked } from '@angular/core';
import { takeUntilDestroyed, toSignal } from '@angular/core/rxjs-interop';
import { NavigationEnd, Router, RouterLink, RouterOutlet } from '@angular/router';
import { filter, map } from 'rxjs';
import { NgIcon, provideIcons } from '@ng-icons/core';
import {
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
} from '@ng-icons/lucide';
import { HlmCollapsibleImports } from '@spartan-ng/helm/collapsible';
import { HlmDropdownMenuImports } from '@spartan-ng/helm/dropdown-menu';
import { HlmSidebarImports, HlmSidebarService } from '@spartan-ng/helm/sidebar';
import { CurrentUser } from '../api/current-user';
import { Project } from '../api/models';
import { Session } from '../auth/session';
import { UserBadge } from '../shared/identity';
import { Theme } from '../shared/theme';
import { ProjectsApi } from './projects-api';
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
  private readonly projectsApi = inject(ProjectsApi);

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
  ];

  /** The selected Workspace's active Projects, listed under Projects in the sidebar. */
  protected readonly projects = signal<Project[]>([]);
  /** More Projects than the sidebar lists; the rest are on the Projects page. */
  protected readonly moreProjects = signal(false);
  /**
   * Bumped on each visit to a Projects page, so a Project just created, renamed or archived
   * shows in the sidebar.
   */
  private readonly projectsVisits = signal(0);

  /** The Project whose pages are showing, to mark it in the sidebar. */
  protected readonly currentProjectId = computed(
    () => /^\/projects\/([^/?#]+)/.exec(this.url())?.[1] ?? null,
  );

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

    this.router.events
      .pipe(
        filter((e) => e instanceof NavigationEnd),
        filter((e) => /^\/projects(\/|\?|$)/.test(e.urlAfterRedirects)),
        takeUntilDestroyed(inject(DestroyRef)),
      )
      .subscribe(() => this.projectsVisits.update((n) => n + 1));

    effect(() => {
      const workspaceId = this.context.current()?.id;
      this.projectsVisits();
      if (workspaceId) {
        untracked(() => void this.loadProjects(workspaceId));
      }
    });
  }

  private async loadProjects(workspaceId: string): Promise<void> {
    try {
      const page = await this.projectsApi.page(workspaceId, {
        search: '',
        ownerTeamId: '',
        includeArchived: false,
      });
      // A Workspace switched meanwhile has its own load under way.
      if (this.context.current()?.id === workspaceId) {
        this.projects.set(page.items);
        this.moreProjects.set(page.has_next);
      }
    } catch {
      // The sidebar keeps what it had; the Projects page shows the error.
    }
  }

  /**
   * Whether a sidebar item is the current page or one of the pages under it. Worked out here
   * rather than by RouterLinkActive, which removes the aria-current this sets.
   */
  protected isActive(item: NavItem): boolean {
    const url = this.url().split(/[?#]/)[0];
    return [item.path, ...(item.within ?? [])].some((p) => url === p || url.startsWith(p + '/'));
  }

  /**
   * Switches Workspace. A page that belongs to the old one, such as one of its Projects, gives
   * way to the same list in the new one.
   */
  protected switchWorkspace(id: string): void {
    if (id === this.context.current()?.id) {
      return;
    }
    this.context.select(id);
    const url = this.url().split(/[?#]/)[0];
    if (/^\/(projects|applications|releases)\/./.test(url)) {
      void this.router.navigateByUrl('/projects');
    } else if (/^\/environments\/./.test(url)) {
      void this.router.navigateByUrl('/environments');
    }
  }

  protected signOut(): void {
    this.session.signOut();
  }
}
