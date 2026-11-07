import { Component, computed, inject, resource, signal } from '@angular/core';
import { form, FormField, maxLength, pattern, required, submit } from '@angular/forms/signals';
import { Router, RouterLink } from '@angular/router';
import { NgIcon, provideIcons } from '@ng-icons/core';
import { lucidePlus } from '@ng-icons/lucide';
import { HlmAlertImports } from '@spartan-ng/helm/alert';
import { HlmBadgeImports } from '@spartan-ng/helm/badge';
import { HlmButtonImports } from '@spartan-ng/helm/button';
import { HlmFieldImports } from '@spartan-ng/helm/field';
import { HlmInputImports } from '@spartan-ng/helm/input';
import { HlmSpinnerImports } from '@spartan-ng/helm/spinner';
import { ApiError } from '../../api/api-client';
import { CurrentUser } from '../../api/current-user';
import { Tier } from '../../api/models';
import { RequestStatus } from '../../shared/request-status';
import { DeliveryApi, slugify } from '../delivery-api';
import { PageHeader } from '../page-header';
import { WorkspaceContext } from '../workspace-context';
import { slugPattern } from './new-project-page';

export const tierLabels: Record<Tier, string> = {
  development: 'Development',
  staging: 'Staging',
  production: 'Production',
  other: 'Other',
};

/** The tiers in the order a person picks from them. */
export const tierOptions = (Object.keys(tierLabels) as Tier[]).map((value) => ({
  value,
  label: tierLabels[value],
}));

const emptyEnvironment = {
  display_name: '',
  slug: '',
  tier: 'development' as Tier,
  execution_enabled: true,
};

/**
 * The Workspace's Environments and the Targets each runs on. Owners and admins add Environments
 * here, and manage each one on its own page.
 */
@Component({
  selector: 'app-environments-page',
  imports: [
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
  providers: [provideIcons({ lucidePlus })],
  templateUrl: './environments-page.html',
})
export class EnvironmentsPage {
  private readonly api = inject(DeliveryApi);
  private readonly router = inject(Router);
  private readonly user = inject(CurrentUser);
  protected readonly context = inject(WorkspaceContext);

  /** Environments are managed with environment.manage, which owners and admins hold. */
  protected readonly canManage = this.user.canManageOrganization;
  protected readonly showArchived = signal(false);

  protected readonly data = resource({
    params: () => {
      const workspaceId = this.context.current()?.id;
      return workspaceId ? { workspaceId, includeArchived: this.showArchived() } : undefined;
    },
    loader: async ({ params: { workspaceId, includeArchived } }) => {
      const environments = await this.api.environments(workspaceId, includeArchived);
      const targets = await this.api.targetsOf(environments);
      return environments.map((environment) => ({
        environment,
        targets: targets.filter((t) => t.environment_id === environment.id),
      }));
    },
  });

  protected readonly environments = computed(() => (this.data.hasValue() ? this.data.value() : []));
  protected readonly failure = computed(() => {
    const error = this.data.error();
    return error ? (error instanceof ApiError ? error.message : 'Something went wrong.') : null;
  });

  protected readonly tierOptions = tierOptions;

  // Adding an Environment.
  protected readonly creating = signal(false);
  protected readonly createStatus = new RequestStatus();
  private readonly model = signal({ ...emptyEnvironment });
  protected readonly createForm = form(this.model, (path) => {
    required(path.display_name, { message: 'Name the Environment, such as Staging.' });
    maxLength(path.display_name, 255, { message: 'Use at most 255 characters.' });
    required(path.slug, { message: 'Give the Environment an address.' });
    pattern(path.slug, slugPattern, {
      message: 'Use lower-case letters and digits, with single hyphens between words.',
    });
    maxLength(path.slug, 63, { message: 'Use at most 63 characters.' });
  });
  /** Whether the slug was typed; until it is, it follows the name. */
  private slugTyped = false;

  protected tierLabel(tier: Tier): string {
    return tierLabels[tier] ?? tier;
  }

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
    this.createForm().reset({ ...emptyEnvironment });
  }

  protected async create(event: Event): Promise<void> {
    event.preventDefault();
    const workspace = this.context.current();
    if (!workspace) {
      return;
    }
    await submit(this.createForm, async () => {
      const model = this.model();
      let createdId = '';
      const done = await this.createStatus.run(async () => {
        const created = await this.api.createEnvironment(workspace.id, {
          display_name: model.display_name.trim(),
          slug: model.slug,
          tier: model.tier,
          execution_enabled: model.execution_enabled,
        });
        createdId = created.id;
      });
      if (done) {
        // Its page is where its Targets are added, so go straight there.
        await this.router.navigate(['/environments', createdId]);
      }
      return undefined;
    });
  }
}
