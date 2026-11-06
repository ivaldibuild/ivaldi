import { Component, computed, inject, signal } from '@angular/core';
import { form, FormField, pattern, required, submit } from '@angular/forms/signals';
import { Router } from '@angular/router';
import { HlmAlertImports } from '@spartan-ng/helm/alert';
import { HlmButtonImports } from '@spartan-ng/helm/button';
import { HlmFieldImports } from '@spartan-ng/helm/field';
import { HlmInputImports } from '@spartan-ng/helm/input';
import { HlmSpinnerImports } from '@spartan-ng/helm/spinner';
import { CurrentUser } from '../../api/current-user';
import { Organization, Workspace } from '../../api/models';
import { RequestStatus } from '../../shared/request-status';
import { OnboardingApi } from '../onboarding-api';
import { SetupStep } from '../setup-step';

const slugPattern = /^[a-z0-9]+(?:-[a-z0-9]+)*$/;
const slugMessage = 'Use lower-case letters, digits and single hyphens, like northwind-labs.';

type OrganizationFields = Pick<Organization, 'display_name' | 'name' | 'slug'> & {
  logo_url: string;
};

/** Step 1: the Organisation's name and address, and its default Workspace. */
@Component({
  selector: 'app-organisation-step',
  imports: [
    FormField,
    SetupStep,
    HlmAlertImports,
    HlmButtonImports,
    HlmFieldImports,
    HlmInputImports,
    HlmSpinnerImports,
  ],
  templateUrl: './organisation-step.html',
})
export class OrganisationStep {
  private readonly api = inject(OnboardingApi);
  private readonly router = inject(Router);
  protected readonly user = inject(CurrentUser);

  protected readonly status = new RequestStatus();
  protected readonly organization = signal<Organization | null>(null);
  protected readonly workspace = signal<Workspace | null>(null);

  protected readonly heading = computed(() =>
    this.user.firstName() ? `Welcome to Ivaldi, ${this.user.firstName()}` : 'Welcome to Ivaldi',
  );

  private readonly organizationModel = signal<OrganizationFields>({
    display_name: '',
    name: '',
    slug: '',
    logo_url: '',
  });
  protected readonly organizationForm = form(this.organizationModel, (path) => {
    required(path.display_name, { message: 'Name the Organisation.' });
    required(path.name, { message: 'Give its full name, such as its legal name.' });
    required(path.slug, { message: 'Give the Organisation an address.' });
    pattern(path.slug, slugPattern, { message: slugMessage });
    pattern(path.logo_url, /^(https:\/\/\S+)?$/, {
      message: 'Use an https address, or leave it empty.',
    });
  });

  /** Changing the address breaks links and sign-in hints that name the old one. */
  protected readonly slugChanged = computed(() => {
    const organization = this.organization();
    return !!organization && this.organizationModel().slug !== organization.slug;
  });

  private readonly workspaceModel = signal({ display_name: '', slug: '' });
  protected readonly workspaceForm = form(this.workspaceModel, (path) => {
    required(path.display_name, { message: 'Name the Workspace.' });
    required(path.slug, { message: 'Give the Workspace an address.' });
    pattern(path.slug, slugPattern, { message: slugMessage });
  });

  constructor() {
    void this.status.run(async () => {
      const [organization, workspace] = await Promise.all([
        this.api.organization(),
        this.api.defaultWorkspace(),
      ]);
      this.organization.set(organization);
      this.organizationModel.set({
        display_name: organization.display_name,
        name: organization.name,
        slug: organization.slug,
        logo_url: organization.logo_url ?? '',
      });
      this.workspace.set(workspace);
      if (workspace) {
        this.workspaceModel.set({ display_name: workspace.display_name, slug: workspace.slug });
      }
    }, 'loading');
  }

  protected async save(event: Event): Promise<void> {
    event.preventDefault();
    const editingOrganization = this.user.canManageOrganization() && !!this.organization();
    const valid =
      (!editingOrganization || (await submit(this.organizationForm, async () => undefined))) &&
      (!this.workspace() || (await submit(this.workspaceForm, async () => undefined)));
    if (!valid) {
      return;
    }
    const saved = await this.status.run(async () => {
      if (editingOrganization) {
        const change = this.organizationChange();
        if (Object.keys(change).length > 0) {
          this.organization.set(await this.api.updateOrganization(change));
          this.user.reload();
        }
      }
      const workspace = this.workspace();
      const fields = this.workspaceModel();
      if (
        workspace &&
        (fields.display_name !== workspace.display_name || fields.slug !== workspace.slug)
      ) {
        this.workspace.set(await this.api.updateWorkspace(workspace.id, fields));
      }
    });
    if (saved) {
      await this.router.navigate(['/setup', 'sign-in']);
    }
  }

  /** Only the fields the person changed, so a save never rewrites the rest. */
  private organizationChange(): Partial<OrganizationFields> {
    const before = this.organization()!;
    const after = this.organizationModel();
    const change: Partial<OrganizationFields> = {};
    for (const key of ['display_name', 'name', 'slug'] as const) {
      if (after[key].trim() !== before[key]) {
        change[key] = after[key].trim();
      }
    }
    if (after.logo_url.trim() !== (before.logo_url ?? '')) {
      change.logo_url = after.logo_url.trim();
    }
    return change;
  }
}
