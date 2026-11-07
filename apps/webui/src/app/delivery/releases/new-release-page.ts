import { Component, computed, effect, inject, input, resource, signal } from '@angular/core';
import { applyEach, form, FormField, pattern, required, submit } from '@angular/forms/signals';
import { Router, RouterLink } from '@angular/router';
import { HlmAlertImports } from '@spartan-ng/helm/alert';
import { HlmButtonImports } from '@spartan-ng/helm/button';
import { HlmFieldImports } from '@spartan-ng/helm/field';
import { HlmInputImports } from '@spartan-ng/helm/input';
import { HlmSpinnerImports } from '@spartan-ng/helm/spinner';
import { RequestStatus } from '../../shared/request-status';
import { DeliveryApi } from '../delivery-api';
import { PageHeader } from '../page-header';

/** The Channel select's choice for letting the Channels' rules decide. */
const anyChannel = '';

/**
 * Creates a Release by hand from one Build per artifact, given by digest. CI usually does this
 * with `ivaldictl release create`; this is for when a person has the digests.
 */
@Component({
  selector: 'app-new-release-page',
  imports: [
    FormField,
    RouterLink,
    PageHeader,
    HlmAlertImports,
    HlmButtonImports,
    HlmFieldImports,
    HlmInputImports,
    HlmSpinnerImports,
  ],
  templateUrl: './new-release-page.html',
})
export class NewReleasePage {
  private readonly api = inject(DeliveryApi);
  private readonly router = inject(Router);

  /** The `:id` route parameter: the Application. */
  readonly id = input.required<string>();

  protected readonly anyChannel = anyChannel;
  protected readonly status = new RequestStatus();
  /** Set when every artifact is optional and no digest was given. */
  protected readonly noBuilds = signal(false);

  protected readonly details = resource({
    params: () => this.id(),
    loader: async ({ params: id }) => {
      const [application, channels, artifacts] = await Promise.all([
        this.api.application(id),
        this.api.channels(id),
        this.api.artifacts(id),
      ]);
      return { application, channels, artifacts };
    },
  });

  protected readonly application = computed(() => this.details.value()?.application ?? null);
  protected readonly channels = computed(() =>
    (this.details.value()?.channels ?? []).filter((c) => !c.archived_at),
  );

  private readonly model = signal({
    version: '',
    channel_id: anyChannel,
    builds: [] as { artifact: string; published_as: string; required: boolean; digest: string }[],
    publish: true,
  });
  protected readonly releaseForm = form(this.model, (path) => {
    required(path.version, { message: 'Give the version, such as 1.4.0.' });
    applyEach(path.builds, (build) => {
      required(build.digest, {
        message: 'Give the digest of the Build to release.',
        when: ({ valueOf }) => valueOf(build.required),
      });
      pattern(build.digest, /^sha256:[0-9a-f]{64}$/, {
        message: 'Use sha256: followed by 64 hexadecimal characters.',
      });
    });
  });

  constructor() {
    // One row per artifact, required ones first.
    effect(() => {
      const artifacts = this.details.value()?.artifacts;
      if (!artifacts || this.model().builds.length > 0) {
        return;
      }
      const rows = [...artifacts]
        .filter((a) => !a.archived_at)
        .sort((a, b) => Number(b.required) - Number(a.required))
        .map((a) => ({
          artifact: a.name,
          published_as: a.artifact_name,
          required: a.required,
          digest: '',
        }));
      this.model.update((m) => ({ ...m, builds: rows }));
    });
  }

  protected async create(event: Event): Promise<void> {
    event.preventDefault();
    const builds = this.model().builds;
    this.noBuilds.set(builds.every((b) => !b.required && !b.digest.trim()));
    if (this.noBuilds()) {
      return;
    }
    await submit(this.releaseForm, async () => {
      const model = this.model();
      let releaseId = '';
      const done = await this.status.run(async () => {
        const release = await this.api.createRelease(this.id(), {
          version: model.version.trim(),
          ...(model.channel_id ? { channel_id: model.channel_id } : {}),
          builds: model.builds
            .filter((b) => b.digest.trim())
            .map((b) => ({ artifact: b.artifact, digest: b.digest.trim() })),
          publish: model.publish,
        });
        releaseId = release.id;
      });
      if (done) {
        await this.router.navigate(['/releases', releaseId]);
      }
      return undefined;
    });
  }
}
