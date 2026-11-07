import { Component, effect, inject, signal, untracked } from '@angular/core';
import { form, FormField, required, submit } from '@angular/forms/signals';
import { Router, RouterLink } from '@angular/router';
import { NgIcon, provideIcons } from '@ng-icons/core';
import { lucidePlus } from '@ng-icons/lucide';
import { HlmAlertImports } from '@spartan-ng/helm/alert';
import { HlmBadgeImports } from '@spartan-ng/helm/badge';
import { HlmButtonImports } from '@spartan-ng/helm/button';
import { HlmFieldImports } from '@spartan-ng/helm/field';
import { HlmInputImports } from '@spartan-ng/helm/input';
import { HlmSpinnerImports } from '@spartan-ng/helm/spinner';
import { CurrentUser } from '../api/current-user';
import { ServiceAccount } from '../api/models';
import { ServiceAccountsApi } from '../api/service-accounts-api';
import { PageHeader } from '../delivery/page-header';
import { formatDay } from '../shared/dates';
import { RequestStatus } from '../shared/request-status';

/**
 * The Organisation's service accounts: principals for CI and other integrations, which act with
 * the roles granted to them and sign in with API tokens. Owners and admins create them here.
 */
@Component({
  selector: 'app-service-accounts-page',
  imports: [
    FormField,
    NgIcon,
    PageHeader,
    RouterLink,
    HlmAlertImports,
    HlmBadgeImports,
    HlmButtonImports,
    HlmFieldImports,
    HlmInputImports,
    HlmSpinnerImports,
  ],
  providers: [provideIcons({ lucidePlus })],
  templateUrl: './service-accounts-page.html',
})
export class ServiceAccountsPage {
  private readonly api = inject(ServiceAccountsApi);
  private readonly router = inject(Router);
  protected readonly user = inject(CurrentUser);

  protected readonly accounts = signal<ServiceAccount[]>([]);
  protected readonly showSuspended = signal(false);
  protected readonly status = new RequestStatus();

  protected readonly creating = signal(false);
  protected readonly createStatus = new RequestStatus();
  private readonly model = signal({ display_name: '', description: '' });
  protected readonly createForm = form(this.model, (path) => {
    required(path.display_name, { message: 'Name it, such as GitHub Actions.' });
  });

  protected readonly formatDay = formatDay;

  constructor() {
    effect(() => {
      const organizationId = this.user.organization()?.id;
      const includeSuspended = this.showSuspended();
      if (organizationId) {
        untracked(() => void this.load(includeSuspended));
      }
    });
  }

  protected async create(event: Event): Promise<void> {
    event.preventDefault();
    await submit(this.createForm, async () => {
      const { display_name, description } = this.model();
      let created: ServiceAccount | undefined;
      const ok = await this.createStatus.run(async () => {
        created = await this.api.create({
          display_name: display_name.trim(),
          ...(description.trim() ? { description: description.trim() } : {}),
        });
      });
      if (ok && created) {
        // Its page is where it gets a role and a token, so go straight there.
        await this.router.navigate(['/settings/service-accounts', created.id]);
      }
      return undefined;
    });
  }

  protected cancelCreate(): void {
    this.creating.set(false);
    this.createStatus.error.set(null);
    this.createForm().reset({ display_name: '', description: '' });
  }

  private async load(includeSuspended: boolean): Promise<void> {
    await this.status.run(async () => {
      this.accounts.set(await this.api.list(includeSuspended));
    }, 'loading');
  }
}
