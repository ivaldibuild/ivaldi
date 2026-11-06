import { Component, inject, signal } from '@angular/core';
import { form, FormField, pattern, required, submit } from '@angular/forms/signals';
import { Router } from '@angular/router';
import { HlmAlertImports } from '@spartan-ng/helm/alert';
import { HlmButtonImports } from '@spartan-ng/helm/button';
import { HlmFieldImports } from '@spartan-ng/helm/field';
import { HlmInputImports } from '@spartan-ng/helm/input';
import { HlmSpinnerImports } from '@spartan-ng/helm/spinner';
import { OnboardingApi, Workspace } from '../onboarding-api';
import { SetupStep } from '../setup-step';
import { StepStatus } from './step-status';

const slugPattern = /^[a-z0-9]+(?:-[a-z0-9]+)*$/;

/** Step 1: the Organisation, from the bootstrap settings, and its default Workspace. */
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

  protected readonly status = new StepStatus();
  protected readonly workspace = signal<Workspace | null>(null);

  private readonly model = signal({ display_name: '', slug: '' });
  protected readonly workspaceForm = form(this.model, (path) => {
    required(path.display_name, { message: 'Name the Workspace.' });
    required(path.slug, { message: 'Give the Workspace an address.' });
    pattern(path.slug, slugPattern, {
      message: 'Use lower-case letters, digits and single hyphens, like platform-team.',
    });
  });

  constructor() {
    void this.status.run(async () => {
      const workspace = await this.api.defaultWorkspace();
      this.workspace.set(workspace);
      if (workspace) {
        this.model.set({ display_name: workspace.display_name, slug: workspace.slug });
      }
    }, 'loading');
  }

  protected async save(event: Event): Promise<void> {
    event.preventDefault();
    const workspace = this.workspace();
    if (!workspace) {
      await this.next();
      return;
    }
    await submit(this.workspaceForm, async () => {
      const change = this.model();
      const changed =
        change.display_name !== workspace.display_name || change.slug !== workspace.slug;
      if (
        !changed ||
        (await this.status.run(() => this.api.updateWorkspace(workspace.id, change)))
      ) {
        await this.next();
      }
      return undefined;
    });
  }

  private next(): Promise<boolean> {
    return this.router.navigate(['/setup', 'sign-in']);
  }
}
