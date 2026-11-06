import { Component, inject } from '@angular/core';
import { RouterLink, RouterLinkActive, RouterOutlet } from '@angular/router';
import { NgIcon, provideIcons } from '@ng-icons/core';
import {
  lucideBox,
  lucideFolder,
  lucideGitBranch,
  lucideLayers,
  lucideLogOut,
  lucideSettings,
  lucideUsers,
} from '@ng-icons/lucide';
import { HlmButtonImports } from '@spartan-ng/helm/button';
import { Session } from '../auth/session';
import { OrganisationBadge, UserBadge } from '../shared/identity';
import { WorkspaceContext } from './workspace-context';

/** The signed-in app: the sidebar with the Workspace switcher beside the current page. */
@Component({
  selector: 'app-shell',
  imports: [
    RouterOutlet,
    RouterLink,
    RouterLinkActive,
    NgIcon,
    HlmButtonImports,
    OrganisationBadge,
    UserBadge,
  ],
  providers: [
    provideIcons({
      lucideBox,
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

  protected readonly delivery = [
    { path: '/applications', label: 'Applications', icon: 'lucideBox' },
    { path: '/projects', label: 'Projects', icon: 'lucideFolder' },
    { path: '/environments', label: 'Environments', icon: 'lucideLayers' },
    { path: '/lifecycles', label: 'Lifecycles', icon: 'lucideGitBranch' },
  ];

  protected readonly organisation = [
    { path: '/people', label: 'People & access', icon: 'lucideUsers' },
  ];

  constructor() {
    void this.context.load().catch(() => undefined);
  }

  protected selectWorkspace(event: Event): void {
    this.context.select((event.target as HTMLSelectElement).value);
  }

  protected signOut(): void {
    this.session.signOut();
  }
}
