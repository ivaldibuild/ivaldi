import { Component, computed, inject, signal } from '@angular/core';
import { disabled, form, FormField, pattern, required, submit } from '@angular/forms/signals';
import { RouterLink } from '@angular/router';
import { HlmAlertImports } from '@spartan-ng/helm/alert';
import { HlmButtonImports } from '@spartan-ng/helm/button';
import { HlmFieldImports } from '@spartan-ng/helm/field';
import { HlmInputImports } from '@spartan-ng/helm/input';
import { HlmSpinnerImports } from '@spartan-ng/helm/spinner';
import { CurrentUser } from '../api/current-user';
import { Organization } from '../api/models';
import { PageHeader } from '../delivery/page-header';
import { OnboardingApi } from '../onboarding/onboarding-api';
import { RequestStatus } from '../shared/request-status';

const slugPattern = /^[a-z0-9]+(?:-[a-z0-9]+)*$/;

interface Fields {
  display_name: string;
  name: string;
  slug: string;
  logo_url: string;
}

/** The Organisation's names, address and logo. Owners and admins change them. */
@Component({
  selector: 'app-general-page',
  imports: [
    FormField,
    PageHeader,
    RouterLink,
    HlmAlertImports,
    HlmButtonImports,
    HlmFieldImports,
    HlmInputImports,
    HlmSpinnerImports,
  ],
  template: `
    <div class="flex max-w-205 flex-col gap-8">
      <app-page-header
        heading="General"
        [trail]="(user.organization()?.display_name ?? 'Organisation') + ' / Organisation settings'"
      />

      @if (status.loading()) {
        <p class="text-muted-foreground flex items-center gap-3 text-sm" role="status">
          <hlm-spinner aria-hidden="true" /> Loading the Organisation…
        </p>
      } @else {
        <form class="grid gap-5 md:grid-cols-2" novalidate (submit)="save($event)">
          @if (status.error(); as message) {
            <div hlmAlert variant="destructive" role="alert" class="md:col-span-2">
              <p hlmAlertDescription>{{ message }}</p>
            </div>
          }
          @if (saved()) {
            <div hlmAlert role="status" class="md:col-span-2">
              <p hlmAlertDescription>{{ saved() }}</p>
            </div>
          }
          <div hlmField>
            <label hlmFieldLabel for="org-display-name">Name</label>
            <input hlmInput id="org-display-name" [formField]="orgForm.display_name" />
            @if (orgForm.display_name().touched() && orgForm.display_name().invalid()) {
              <hlm-field-error forceShow>{{
                orgForm.display_name().errors()[0].message
              }}</hlm-field-error>
            }
          </div>
          <div hlmField>
            <label hlmFieldLabel for="org-name">Full name</label>
            <input hlmInput id="org-name" [formField]="orgForm.name" />
            @if (orgForm.name().touched() && orgForm.name().invalid()) {
              <hlm-field-error forceShow>{{ orgForm.name().errors()[0].message }}</hlm-field-error>
            }
          </div>
          <div hlmField>
            <label hlmFieldLabel for="org-slug">Address</label>
            <input hlmInput id="org-slug" [formField]="orgForm.slug" />
            @if (orgForm.slug().touched() && orgForm.slug().invalid()) {
              <hlm-field-error forceShow>{{ orgForm.slug().errors()[0].message }}</hlm-field-error>
            } @else if (slugChanged()) {
              <p hlmFieldDescription>
                Changing it breaks links and sign-in hints that use the old address.
              </p>
            }
          </div>
          <div hlmField>
            <label hlmFieldLabel for="org-logo">Logo address (optional)</label>
            <input hlmInput id="org-logo" placeholder="https://" [formField]="orgForm.logo_url" />
            @if (orgForm.logo_url().touched() && orgForm.logo_url().invalid()) {
              <hlm-field-error forceShow>{{
                orgForm.logo_url().errors()[0].message
              }}</hlm-field-error>
            }
          </div>
          @if (user.canManageOrganization()) {
            <div class="flex flex-wrap gap-2 md:col-span-2">
              <button hlmBtn type="submit" [disabled]="status.saving()">
                @if (status.saving()) {
                  <hlm-spinner aria-hidden="true" /> Saving…
                } @else {
                  Save
                }
              </button>
            </div>
          } @else {
            <p class="text-muted-foreground text-sm md:col-span-2">
              Owners and admins can change these.
            </p>
          }
        </form>

        @if (user.canManageOrganization()) {
          <section class="flex flex-col gap-3" aria-labelledby="setup-heading">
            <h2 id="setup-heading" class="text-heading text-xl font-semibold">Setup guide</h2>
            <p class="text-foreground text-sm">
              Walk through sign-in methods, a first Team, Environments and a cluster again.
            </p>
            <a hlmBtn variant="outline" class="self-start" routerLink="/setup"
              >Open the setup guide</a
            >
          </section>
        }
      }
    </div>
  `,
})
export class GeneralPage {
  private readonly api = inject(OnboardingApi);
  protected readonly user = inject(CurrentUser);

  protected readonly status = new RequestStatus();
  protected readonly saved = signal<string | null>(null);
  private readonly organization = signal<Organization | null>(null);

  private readonly model = signal<Fields>({ display_name: '', name: '', slug: '', logo_url: '' });
  protected readonly orgForm = form(this.model, (path) => {
    disabled(path, () => !this.user.canManageOrganization());
    required(path.display_name, { message: 'Name the Organisation.' });
    required(path.name, { message: 'Give its full name, such as its legal name.' });
    required(path.slug, { message: 'Give the Organisation an address.' });
    pattern(path.slug, slugPattern, {
      message: 'Use lower-case letters, digits and single hyphens, like northwind-labs.',
    });
    pattern(path.logo_url, /^(https:\/\/\S+)?$/, {
      message: 'Use an https address, or leave it empty.',
    });
  });

  protected readonly slugChanged = computed(() => {
    const organization = this.organization();
    return !!organization && this.model().slug !== organization.slug;
  });

  constructor() {
    void this.status.run(async () => this.fill(await this.api.organization()), 'loading');
  }

  protected async save(event: Event): Promise<void> {
    event.preventDefault();
    this.saved.set(null);
    await submit(this.orgForm, async () => {
      const change = this.change();
      if (Object.keys(change).length === 0) {
        this.saved.set('Nothing to save.');
        return undefined;
      }
      const ok = await this.status.run(async () =>
        this.fill(await this.api.updateOrganization(change)),
      );
      if (ok) {
        this.user.reload();
        this.saved.set('Saved.');
      }
      return undefined;
    });
  }

  private fill(organization: Organization): void {
    this.organization.set(organization);
    this.model.set({
      display_name: organization.display_name,
      name: organization.name,
      slug: organization.slug,
      logo_url: organization.logo_url ?? '',
    });
  }

  /** Only the fields that changed, so a save never rewrites the rest. */
  private change(): Partial<Fields> {
    const before = this.organization()!;
    const after = this.model();
    const change: Partial<Fields> = {};
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
