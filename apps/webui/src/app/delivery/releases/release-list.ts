import { Component, computed, effect, inject, input, signal, untracked } from '@angular/core';
import { RouterLink } from '@angular/router';
import { HlmAlertImports } from '@spartan-ng/helm/alert';
import { HlmBadgeImports } from '@spartan-ng/helm/badge';
import { HlmButtonImports } from '@spartan-ng/helm/button';
import { HlmSpinnerImports } from '@spartan-ng/helm/spinner';
import { ApiError } from '../../api/api-client';
import { Artifact, Channel, Release, ReleaseStatus } from '../../api/models';
import { formatDay } from '../../shared/dates';
import { RequestStatus } from '../../shared/request-status';
import { DeliveryApi } from '../delivery-api';
import { buildSummaries, channelName } from './release-labels';

const filters: { value: ReleaseStatus | ''; label: string }[] = [
  { value: '', label: 'All Releases' },
  { value: 'draft', label: 'Drafts' },
  { value: 'published', label: 'Published' },
  { value: 'withdrawn', label: 'Withdrawn' },
];

/** An Application's Releases, newest first, with a draft's Publish action. */
@Component({
  selector: 'app-release-list',
  imports: [RouterLink, HlmAlertImports, HlmBadgeImports, HlmButtonImports, HlmSpinnerImports],
  templateUrl: './release-list.html',
})
export class ReleaseList {
  private readonly api = inject(DeliveryApi);

  readonly applicationId = input.required<string>();
  readonly channels = input.required<Channel[]>();
  readonly artifacts = input.required<Artifact[]>();

  protected readonly filters = filters;
  protected readonly status = signal<ReleaseStatus | ''>('');
  protected readonly releases = signal<Release[]>([]);
  protected readonly total = signal(0);
  protected readonly loaded = signal(false);
  protected readonly loadError = signal<string | null>(null);
  protected readonly loadingMore = signal(false);
  private readonly cursor = signal<string | null>(null);
  protected readonly hasMore = signal(false);

  protected readonly emptyFilter = computed(
    () =>
      ({ draft: 'drafts', published: 'published Releases', withdrawn: 'withdrawn Releases' })[
        this.status() || 'draft'
      ],
  );

  protected readonly publishing = signal<string | null>(null);
  protected readonly action = new RequestStatus();

  constructor() {
    effect(() => {
      this.applicationId();
      this.status();
      untracked(() => void this.load(true));
    });
  }

  protected channel(id: string): string {
    return channelName(this.channels(), id);
  }

  protected builds(release: Release) {
    return buildSummaries(release, this.artifacts());
  }

  protected created(release: Release): string {
    return formatDay(release.created_at);
  }

  protected filter(event: Event): void {
    this.status.set((event.target as HTMLSelectElement).value as ReleaseStatus | '');
  }

  protected async publish(release: Release): Promise<void> {
    this.publishing.set(release.id);
    const done = await this.action.run(() => this.api.publishRelease(release.id));
    this.publishing.set(null);
    if (done) {
      await this.load(true);
    }
  }

  protected async more(): Promise<void> {
    this.loadingMore.set(true);
    await this.load(false);
    this.loadingMore.set(false);
  }

  private async load(reset: boolean): Promise<void> {
    try {
      const page = await this.api.releases(this.applicationId(), {
        status: this.status() || undefined,
        cursor: reset ? null : this.cursor(),
      });
      this.releases.update((current) => (reset ? page.items : [...current, ...page.items]));
      this.total.set(page.total_count);
      this.cursor.set(page.has_next ? page.next_cursor : null);
      this.hasMore.set(page.has_next);
      this.loadError.set(null);
    } catch (e) {
      this.loadError.set(e instanceof ApiError ? e.message : 'Something went wrong. Try again.');
    } finally {
      this.loaded.set(true);
    }
  }
}
