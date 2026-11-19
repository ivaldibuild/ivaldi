import { Component, computed, DestroyRef, inject } from '@angular/core';
import { takeUntilDestroyed } from '@angular/core/rxjs-interop';
import { NavigationEnd, Router, RouterLink } from '@angular/router';
import { filter } from 'rxjs';
import { NgIcon, provideIcons } from '@ng-icons/core';
import { lucideBell } from '@ng-icons/lucide';
import { HlmButtonImports } from '@spartan-ng/helm/button';
import { NotificationsApi } from '../api/notifications-api';

/** How often the bell asks for the unread count. */
export const bellRefreshMs = 60_000;

/**
 * The bell in the top bar: a link to the inbox, with how many notifications are unread. It asks
 * again every minute and after each navigation.
 */
@Component({
  selector: 'app-notification-bell',
  imports: [NgIcon, RouterLink, HlmButtonImports],
  providers: [provideIcons({ lucideBell })],
  template: `
    <a
      hlmBtn
      variant="ghost"
      size="icon"
      routerLink="/inbox"
      class="relative"
      [attr.aria-label]="label()"
    >
      <ng-icon name="lucideBell" aria-hidden="true" />
      @if (unread() > 0) {
        <span
          aria-hidden="true"
          class="bg-primary text-primary-foreground absolute -top-0.5 -right-0.5 inline-flex h-4 min-w-4 items-center justify-center rounded-full px-1 text-[10px] leading-none font-semibold"
          >{{ badge() }}</span
        >
      }
    </a>
  `,
})
export class NotificationBell {
  private readonly notifications = inject(NotificationsApi);
  protected readonly unread = this.notifications.unread;
  protected readonly badge = computed(() => (this.unread() > 99 ? '99+' : String(this.unread())));
  protected readonly label = computed(() => {
    const n = this.unread();
    return n === 0 ? 'Inbox' : `Inbox, ${n} unread`;
  });

  constructor() {
    const refresh = () => void this.notifications.refreshCount().catch(() => undefined);
    refresh();
    const timer = setInterval(refresh, bellRefreshMs);
    inject(DestroyRef).onDestroy(() => clearInterval(timer));
    inject(Router)
      .events.pipe(
        filter((e) => e instanceof NavigationEnd),
        takeUntilDestroyed(),
      )
      .subscribe(refresh);
  }
}
