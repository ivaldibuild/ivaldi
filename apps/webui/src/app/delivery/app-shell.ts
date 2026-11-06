import { Component, inject } from '@angular/core';
import { RouterLink, RouterLinkActive, RouterOutlet } from '@angular/router';
import { NgIcon, provideIcons } from '@ng-icons/core';
import {
  lucideBox,
  lucideGitBranch,
  lucideLayers,
  lucideLogOut,
  lucideSettings,
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
    provideIcons({ lucideBox, lucideGitBranch, lucideLayers, lucideLogOut, lucideSettings }),
  ],
  templateUrl: './app-shell.html',
})
export class AppShell {
  private readonly session = inject(Session);
  protected readonly context = inject(WorkspaceContext);

  protected readonly delivery = [
    { path: '/applications', label: 'Applications', icon: 'lucideBox' },
    { path: '/environments', label: 'Environments', icon: 'lucideLayers' },
    { path: '/lifecycles', label: 'Lifecycles', icon: 'lucideGitBranch' },
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
