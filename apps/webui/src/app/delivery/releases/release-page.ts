import { Component, computed, inject, input, resource, signal } from '@angular/core';
import { RouterLink } from '@angular/router';
import { NgIcon, provideIcons } from '@ng-icons/core';
import { lucideRocket } from '@ng-icons/lucide';
import { HlmAlertImports } from '@spartan-ng/helm/alert';
import { HlmBadgeImports } from '@spartan-ng/helm/badge';
import { HlmButtonImports } from '@spartan-ng/helm/button';
import { HlmSpinnerImports } from '@spartan-ng/helm/spinner';
import { ApiError } from '../../api/api-client';
import { Build } from '../../api/models';
import { formatDayAndTime } from '../../shared/dates';
import { RequestStatus } from '../../shared/request-status';
import { DeliveryApi } from '../delivery-api';
import { PageHeader } from '../page-header';
import { ReleaseVariables } from '../variables/release-variables';
import { channelName } from './release-labels';

/** One Release: its Channel, its Builds in full, and publishing or withdrawing it. */
@Component({
  selector: 'app-release-page',
  imports: [
    NgIcon,
    RouterLink,
    PageHeader,
    ReleaseVariables,
    HlmAlertImports,
    HlmBadgeImports,
    HlmButtonImports,
    HlmSpinnerImports,
  ],
  providers: [provideIcons({ lucideRocket })],
  templateUrl: './release-page.html',
})
export class ReleasePage {
  private readonly api = inject(DeliveryApi);

  /** The `:id` route parameter. */
  readonly id = input.required<string>();

  protected readonly details = resource({
    params: () => this.id(),
    loader: async ({ params: id }) => {
      const release = await this.api.release(id);
      const [application, channels, artifacts] = await Promise.all([
        this.api.application(release.application_id),
        this.api.channels(release.application_id),
        this.api.artifacts(release.application_id),
      ]);
      return { release, application, channels, artifacts };
    },
  });

  protected readonly release = computed(() => this.details.value()?.release ?? null);
  protected readonly application = computed(() => this.details.value()?.application ?? null);
  protected readonly channel = computed(() => {
    const d = this.details.value();
    return d ? channelName(d.channels, d.release.channel_id) : '';
  });

  protected readonly builds = computed(() => {
    const d = this.details.value();
    if (!d) {
      return [];
    }
    return d.release.builds.map((item) => ({
      artifact:
        d.artifacts.find((a) => a.id === item.artifact_id)?.name ?? item.build.artifact_name,
      build: item.build,
    }));
  });

  /** When it was created, published and withdrawn, oldest first. */
  protected readonly history = computed(() => {
    const r = this.release();
    if (!r) {
      return [];
    }
    return [
      { label: 'Created', at: r.created_at },
      { label: 'Published', at: r.published_at },
      { label: 'Withdrawn', at: r.withdrawn_at },
    ]
      .filter((e): e is { label: string; at: string } => e.at !== null)
      .map((e) => ({ label: e.label, at: formatDayAndTime(e.at) }));
  });

  protected readonly failure = computed(() => {
    const error = this.details.error();
    return error ? (error instanceof ApiError ? error.message : 'Something went wrong.') : null;
  });

  protected shortCommit(build: Build): string {
    return build.commit_sha?.slice(0, 7) ?? '';
  }

  /** The CI run as a link when it is one. */
  protected ciRunLink(build: Build): string | null {
    return build.ci_run_ref && /^https?:\/\//.test(build.ci_run_ref) ? build.ci_run_ref : null;
  }

  protected builtAt(build: Build): string {
    return build.built_at ? formatDayAndTime(build.built_at) : '—';
  }

  // Publishing and withdrawing.
  protected readonly action = new RequestStatus();
  protected readonly confirmingWithdraw = signal(false);

  protected async publish(): Promise<void> {
    if (await this.action.run(() => this.api.publishRelease(this.id()))) {
      this.details.reload();
    }
  }

  protected async withdraw(): Promise<void> {
    if (await this.action.run(() => this.api.withdrawRelease(this.id()))) {
      this.confirmingWithdraw.set(false);
      this.details.reload();
    }
  }
}
