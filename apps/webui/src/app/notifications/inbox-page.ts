import { Component, inject, signal } from '@angular/core';
import { RouterLink } from '@angular/router';
import { HlmAlertImports } from '@spartan-ng/helm/alert';
import { HlmButtonImports } from '@spartan-ng/helm/button';
import { HlmSpinnerImports } from '@spartan-ng/helm/spinner';
import { Notification } from '../api/models';
import { NotificationsApi } from '../api/notifications-api';
import { PageHeader } from '../delivery/page-header';
import { formatDayAndTime } from '../shared/dates';
import { RequestStatus } from '../shared/request-status';

const kindLabels: Record<Notification['kind'], string> = {
  approval_requested: 'Approval needed',
  approval_decided: 'Approval decided',
};

/**
 * The signed-in person's notifications, newest first: what waits for them to decide, and how
 * what they asked for was decided. Opening one marks it read.
 */
@Component({
  selector: 'app-inbox-page',
  imports: [RouterLink, PageHeader, HlmAlertImports, HlmButtonImports, HlmSpinnerImports],
  templateUrl: './inbox-page.html',
})
export class InboxPage {
  private readonly api = inject(NotificationsApi);

  protected readonly unread = this.api.unread;
  protected readonly unreadOnly = signal(false);
  protected readonly notifications = signal<Notification[]>([]);
  protected readonly hasMore = signal(false);
  protected readonly loadingMore = signal(false);
  protected readonly status = new RequestStatus();
  protected readonly markStatus = new RequestStatus();
  private nextCursor: string | null = null;

  protected readonly kindLabel = (n: Notification) => kindLabels[n.kind];
  protected readonly at = (n: Notification) => formatDayAndTime(n.created_at);
  /** An ask that no longer waits, because the request was decided or cancelled. */
  protected readonly closed = (n: Notification) =>
    n.kind === 'approval_requested' && !!n.resolved_at;

  constructor() {
    void this.load(true);
    void this.api.refreshCount().catch(() => undefined);
  }

  protected show(unreadOnly: boolean): void {
    if (unreadOnly !== this.unreadOnly()) {
      this.unreadOnly.set(unreadOnly);
      void this.load(true);
    }
  }

  protected async loadMore(): Promise<void> {
    this.loadingMore.set(true);
    await this.load(false);
    this.loadingMore.set(false);
  }

  private async load(reset: boolean): Promise<void> {
    await this.status.run(
      async () => {
        const page = await this.api.page({
          unreadOnly: this.unreadOnly(),
          cursor: reset ? null : this.nextCursor,
        });
        this.notifications.update((shown) => (reset ? page.items : [...shown, ...page.items]));
        this.nextCursor = page.next_cursor;
        this.hasMore.set(page.has_next);
      },
      reset ? 'loading' : 'saving',
    );
  }

  /** Marks one read as it opens; the link goes on whether or not that works. */
  protected open(n: Notification): void {
    if (!n.read_at) {
      void this.api.markRead(n.id).catch(() => undefined);
    }
  }

  protected async markAllRead(): Promise<void> {
    if (await this.markStatus.run(() => this.api.markAllRead())) {
      await this.load(true);
    }
  }
}
