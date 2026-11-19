import { computed, inject, Service, signal } from '@angular/core';
import { ApiClient } from './api-client';
import { CursorPage, Notification } from './models';

/**
 * The signed-in person's notifications, and how many are unread for the bell. Marking read
 * here keeps the count current; anything else that changes it calls `refreshCount`.
 */
@Service()
export class NotificationsApi {
  private readonly api = inject(ApiClient);

  private readonly count = signal(0);
  /** Unread notifications that still ask for something. */
  readonly unread = computed(() => this.count());

  /** A page of notifications, newest first. */
  page(
    options: { unreadOnly?: boolean; cursor?: string | null } = {},
  ): Promise<CursorPage<Notification>> {
    const query = new URLSearchParams({ limit: '25' });
    if (options.unreadOnly) {
      query.set('unread', 'true');
    }
    if (options.cursor) {
      query.set('next_cursor', options.cursor);
    }
    return this.api.get(`/notifications?${query}`);
  }

  async refreshCount(): Promise<void> {
    const { count } = await this.api.get<{ count: number }>('/notifications/unread-count');
    this.count.set(count);
  }

  async markRead(id: string): Promise<Notification> {
    const notification = await this.api.send<Notification>('POST', `/notifications/${id}/read`);
    void this.refreshCount().catch(() => undefined);
    return notification;
  }

  async markAllRead(): Promise<void> {
    await this.api.send('POST', '/notifications/read');
    this.count.set(0);
  }
}
