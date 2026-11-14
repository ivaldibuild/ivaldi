import { DatePipe } from '@angular/common';
import { Component, computed, inject, resource, signal } from '@angular/core';
import { form, FormField, maxLength, pattern, required, submit } from '@angular/forms/signals';
import { Router, RouterLink } from '@angular/router';
import { NgIcon, provideIcons } from '@ng-icons/core';
import { lucidePlus, lucideServer } from '@ng-icons/lucide';
import { HlmAlertImports } from '@spartan-ng/helm/alert';
import { HlmBadgeImports } from '@spartan-ng/helm/badge';
import { HlmButtonImports } from '@spartan-ng/helm/button';
import { HlmFieldImports } from '@spartan-ng/helm/field';
import { HlmInputImports } from '@spartan-ng/helm/input';
import { HlmSpinnerImports } from '@spartan-ng/helm/spinner';
import { ApiError } from '../../api/api-client';
import { CurrentUser } from '../../api/current-user';
import { RequestStatus } from '../../shared/request-status';
import { ClustersApi } from '../clusters-api';
import { slugify } from '../delivery-api';
import { PageHeader } from '../page-header';
import { slugPattern } from './new-project-page';

const emptyCluster = { display_name: '', slug: '' };

/**
 * The Organization's Kubernetes clusters. One runtime agent serves each
 * cluster and every Target in it.
 */
@Component({
  selector: 'app-clusters-page',
  imports: [
    DatePipe,
    FormField,
    NgIcon,
    RouterLink,
    PageHeader,
    HlmAlertImports,
    HlmBadgeImports,
    HlmButtonImports,
    HlmFieldImports,
    HlmInputImports,
    HlmSpinnerImports,
  ],
  providers: [provideIcons({ lucidePlus, lucideServer })],
  templateUrl: './clusters-page.html',
})
export class ClustersPage {
  private readonly api = inject(ClustersApi);
  private readonly router = inject(Router);
  private readonly user = inject(CurrentUser);

  /** Clusters are managed with cluster.manage, which owners and admins hold. */
  protected readonly canManage = this.user.canManageOrganization;
  protected readonly showArchived = signal(false);

  protected readonly data = resource({
    params: () => ({ includeArchived: this.showArchived() }),
    loader: ({ params: { includeArchived } }) => this.api.clusters(includeArchived),
  });

  protected readonly clusters = computed(() => (this.data.hasValue() ? this.data.value() : []));
  protected readonly failure = computed(() => {
    const error = this.data.error();
    return error ? (error instanceof ApiError ? error.message : 'Something went wrong.') : null;
  });

  // Adding a Cluster.
  protected readonly creating = signal(false);
  protected readonly createStatus = new RequestStatus();
  private readonly model = signal({ ...emptyCluster });
  protected readonly createForm = form(this.model, (path) => {
    required(path.display_name, { message: 'Name the cluster, such as Prod EU.' });
    maxLength(path.display_name, 255, { message: 'Use at most 255 characters.' });
    required(path.slug, { message: 'Give the cluster an address.' });
    pattern(path.slug, slugPattern, {
      message: 'Use lower-case letters and digits, with single hyphens between words.',
    });
    maxLength(path.slug, 63, { message: 'Use at most 63 characters.' });
  });
  private slugTyped = false;

  protected nameChanged(): void {
    if (!this.slugTyped) {
      this.model.update((m) => ({ ...m, slug: slugify(m.display_name) }));
    }
  }

  protected slugChanged(): void {
    this.slugTyped = true;
  }

  protected cancelCreate(): void {
    this.creating.set(false);
    this.createStatus.error.set(null);
    this.slugTyped = false;
    this.createForm().reset({ ...emptyCluster });
  }

  protected async create(event: Event): Promise<void> {
    event.preventDefault();
    await submit(this.createForm, async () => {
      const model = this.model();
      let createdId = '';
      const done = await this.createStatus.run(async () => {
        const created = await this.api.createCluster({
          display_name: model.display_name.trim(),
          slug: model.slug,
        });
        createdId = created.id;
      });
      if (done) {
        // Its page is where the agent is installed.
        await this.router.navigate(['/clusters', createdId]);
      }
      return undefined;
    });
  }
}
