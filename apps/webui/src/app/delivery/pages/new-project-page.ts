import { Component, computed, effect, inject, resource, signal } from '@angular/core';
import { form, FormField, maxLength, pattern, required, submit } from '@angular/forms/signals';
import { Router, RouterLink } from '@angular/router';
import { HlmAlertImports } from '@spartan-ng/helm/alert';
import { HlmButtonImports } from '@spartan-ng/helm/button';
import { HlmFieldImports } from '@spartan-ng/helm/field';
import { HlmInputImports } from '@spartan-ng/helm/input';
import { HlmSpinnerImports } from '@spartan-ng/helm/spinner';
import { RequestStatus } from '../../shared/request-status';
import { DeliveryApi, slugify } from '../delivery-api';
import { PageHeader } from '../page-header';
import { WorkspaceContext } from '../workspace-context';

/** A slug the control plane accepts: lower-case letters and digits in words joined by single hyphens. */
export const slugPattern = /^[a-z0-9]+(-[a-z0-9]+)*$/;

/** Creates a Project in the current Workspace, owned by a Team. */
@Component({
  selector: 'app-new-project-page',
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
  templateUrl: './new-project-page.html',
})
export class NewProjectPage {
  private readonly api = inject(DeliveryApi);
  private readonly router = inject(Router);
  protected readonly context = inject(WorkspaceContext);
  protected readonly status = new RequestStatus();

  private readonly teamsResource = resource({ loader: () => this.api.teams() });
  /** The Teams, or none while they load or if they didn't. */
  protected readonly teams = computed(() =>
    this.teamsResource.hasValue() ? this.teamsResource.value() : [],
  );
  protected readonly teamsLoading = computed(
    () => this.teamsResource.isLoading() && !this.teamsResource.hasValue(),
  );
  protected readonly teamsFailed = computed(() => !!this.teamsResource.error());

  private readonly model = signal({
    display_name: '',
    slug: '',
    owner_team_id: '',
    description: '',
  });
  protected readonly projectForm = form(this.model, (path) => {
    required(path.display_name, { message: 'Name the Project.' });
    maxLength(path.display_name, 255, { message: 'Use at most 255 characters.' });
    required(path.slug, { message: 'Give the Project an address.' });
    pattern(path.slug, slugPattern, {
      message: 'Use lower-case letters and digits, with single hyphens between words.',
    });
    maxLength(path.slug, 63, { message: 'Use at most 63 characters.' });
    required(path.owner_team_id, { message: 'Choose the Team responsible for it.' });
  });

  /** Whether the slug was typed; until it is, it follows the name. */
  private slugTyped = false;

  constructor() {
    effect(() => {
      const first = this.teams()[0];
      if (first && !this.model().owner_team_id) {
        this.model.update((m) => ({ ...m, owner_team_id: first.id }));
      }
    });
  }

  protected nameChanged(): void {
    if (!this.slugTyped) {
      this.model.update((m) => ({ ...m, slug: slugify(m.display_name) }));
    }
  }

  protected slugChanged(): void {
    this.slugTyped = true;
  }

  protected async create(event: Event): Promise<void> {
    event.preventDefault();
    const workspace = this.context.current();
    if (!workspace) {
      return;
    }
    await submit(this.projectForm, async () => {
      const model = this.model();
      let projectId = '';
      const done = await this.status.run(async () => {
        const project = await this.api.createProject(workspace.id, {
          display_name: model.display_name.trim(),
          slug: model.slug,
          owner_team_id: model.owner_team_id,
          description: model.description.trim(),
        });
        projectId = project.id;
      });
      if (done) {
        await this.router.navigate(['/projects', projectId]);
      }
      return undefined;
    });
  }
}
