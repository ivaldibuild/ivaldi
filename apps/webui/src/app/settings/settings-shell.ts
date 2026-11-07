import { Component, inject } from '@angular/core';
import { toSignal } from '@angular/core/rxjs-interop';
import { NavigationEnd, Router, RouterLink, RouterOutlet } from '@angular/router';
import { filter, map } from 'rxjs';
import { CurrentUser } from '../api/current-user';

interface Section {
  path: string;
  label: string;
}

/**
 * Organisation settings: a menu of its sections beside the one being shown. Opened from the
 * person's menu at the foot of the sidebar.
 */
@Component({
  selector: 'app-settings-shell',
  imports: [RouterLink, RouterOutlet],
  template: `
    <div class="flex flex-col gap-6 md:flex-row md:gap-10">
      <nav aria-label="Organisation settings" class="flex shrink-0 flex-col gap-3 md:w-52">
        <h2 class="text-muted-foreground text-[11px] font-semibold tracking-[0.24em] uppercase">
          {{ user.organization()?.display_name ?? 'Organisation' }} settings
        </h2>
        <ul class="-mx-1 flex gap-1 overflow-x-auto md:mx-0 md:flex-col md:overflow-visible">
          @for (section of sections; track section.path) {
            <li class="shrink-0">
              <a
                class="text-foreground hover:bg-muted block px-3 py-2 text-sm no-underline"
                [class.bg-muted]="isActive(section)"
                [class.font-semibold]="isActive(section)"
                [class.text-heading]="isActive(section)"
                [routerLink]="section.path"
                [attr.aria-current]="isActive(section) ? 'page' : null"
                >{{ section.label }}</a
              >
            </li>
          }
        </ul>
      </nav>
      <div class="min-w-0 flex-1">
        <router-outlet />
      </div>
    </div>
  `,
})
export class SettingsShell {
  private readonly router = inject(Router);
  protected readonly user = inject(CurrentUser);

  protected readonly sections: Section[] = [
    { path: '/settings/general', label: 'General' },
    { path: '/settings/people', label: 'People & access' },
    { path: '/settings/service-accounts', label: 'Service accounts' },
    { path: '/settings/workspaces', label: 'Workspaces' },
    { path: '/settings/teams', label: 'Teams' },
  ];

  private readonly url = toSignal(
    this.router.events.pipe(
      filter((e) => e instanceof NavigationEnd),
      map(() => this.router.url),
    ),
    { initialValue: this.router.url },
  );

  /** Whether the section, or a page under it such as one Team, is showing. */
  protected isActive(section: Section): boolean {
    const url = this.url().split(/[?#]/)[0];
    return url === section.path || url.startsWith(section.path + '/');
  }
}
