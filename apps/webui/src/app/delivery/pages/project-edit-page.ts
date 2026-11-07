import { Component, computed, inject, input, linkedSignal, resource, signal } from '@angular/core';
import { form, FormField, maxLength, pattern, required, submit } from '@angular/forms/signals';
import { RouterLink } from '@angular/router';
import { NgIcon, provideIcons } from '@ng-icons/core';
import { lucideArchive, lucideLock } from '@ng-icons/lucide';
import { HlmAlertImports } from '@spartan-ng/helm/alert';
import { HlmButtonImports } from '@spartan-ng/helm/button';
import { HlmFieldImports } from '@spartan-ng/helm/field';
import { HlmInputImports } from '@spartan-ng/helm/input';
import { HlmSpinnerImports } from '@spartan-ng/helm/spinner';
import { ApiError } from '../../api/api-client';
import { Project } from '../../api/models';
import { RequestStatus } from '../../shared/request-status';
import { DeliveryApi } from '../delivery-api';
import { PageHeader } from '../page-header';
import { defaultLifecycleKey, ProjectsApi } from '../projects-api';
import { slugPattern } from './new-project-page';

/**
 * Editing a Project: its name, address and description, the Team that owns it, the Lifecycle its
 * new Applications follow by default, and archiving or restoring it.
 */
@Component({
  selector: 'app-project-edit-page',
  imports: [
    FormField,
    NgIcon,
    RouterLink,
    PageHeader,
    HlmAlertImports,
    HlmButtonImports,
    HlmFieldImports,
    HlmInputImports,
    HlmSpinnerImports,
  ],
  providers: [provideIcons({ lucideArchive, lucideLock })],
  templateUrl: './project-edit-page.html',
})
export class ProjectEditPage {
  private readonly projects = inject(ProjectsApi);
  private readonly delivery = inject(DeliveryApi);

  /** The `:id` route parameter. */
  readonly id = input.required<string>();

  protected readonly details = resource({
    params: () => this.id(),
    loader: async ({ params: id }) => {
      const project = await this.projects.project(id);
      const [teams, lifecycles, resolved, stored] = await Promise.all([
        this.delivery.teams(),
        this.delivery.lifecycles(project.workspace_id),
        this.projects.resolved(project, defaultLifecycleKey),
        this.projects.stored(project, defaultLifecycleKey),
      ]);
      return { project, teams, lifecycles, resolved, stored };
    },
  });

  /**
   * The loaded value, or undefined while there is none: a resource's value() throws while it's in
   * an error state, which would stop the page from rendering its error.
   */
  private readonly loaded = computed(() =>
    this.details.hasValue() ? this.details.value() : undefined,
  );

  protected readonly project = computed(() => this.loaded()?.project ?? null);
  protected readonly archived = computed(() => !!this.project()?.archived_at);
  protected readonly teams = computed(() => this.loaded()?.teams ?? []);
  protected readonly lifecycles = computed(() => this.loaded()?.lifecycles ?? []);

  protected readonly failure = computed(() => {
    const error = this.details.error();
    return error ? (error instanceof ApiError ? error.message : 'Something went wrong.') : null;
  });

  // The default Lifecycle for new Applications.

  protected readonly defaultLifecycle = computed(() => {
    const d = this.loaded();
    if (!d) {
      return null;
    }
    const id = typeof d.resolved.value === 'string' ? d.resolved.value : '';
    const lifecycle = d.lifecycles.find((l) => l.id === id);
    return {
      name: lifecycle?.display_name ?? (id ? 'A Lifecycle that no longer exists' : ''),
      /** Where the value comes from, for the page to say. */
      source:
        d.resolved.level === 'project'
          ? 'Set on this Project.'
          : d.resolved.level
            ? `Set on the ${d.resolved.level === 'workspace' ? 'Workspace' : 'Organisation'}.`
            : '',
      locked: d.resolved.locked,
      storedHere: d.stored !== null,
    };
  });
  protected readonly lifecycleChoice = linkedSignal(() => {
    const d = this.loaded();
    const value = d?.resolved.value;
    return typeof value === 'string' ? value : (d?.lifecycles[0]?.id ?? '');
  });
  protected readonly lifecycleStatus = new RequestStatus();

  protected async saveLifecycle(event: Event): Promise<void> {
    event.preventDefault();
    const d = this.loaded();
    if (!d || !this.lifecycleChoice()) {
      return;
    }
    const saved = await this.lifecycleStatus.run(() =>
      this.projects.set(
        d.project,
        defaultLifecycleKey,
        this.lifecycleChoice(),
        d.stored?.version ?? 0,
      ),
    );
    if (saved) {
      this.details.reload();
    }
  }

  protected async useWorkspaceLifecycle(): Promise<void> {
    const d = this.loaded();
    if (!d?.stored) {
      return;
    }
    const cleared = await this.lifecycleStatus.run(() =>
      this.projects.clear(d.project, defaultLifecycleKey, d.stored!.version),
    );
    if (cleared) {
      this.details.reload();
    }
  }

  // Changing its name, address, and description.

  private readonly detailsModel = linkedSignal(() => {
    const p = this.project();
    return {
      display_name: p?.display_name ?? '',
      slug: p?.slug ?? '',
      description: p?.description ?? '',
    };
  });
  protected readonly detailsForm = form(this.detailsModel, (path) => {
    required(path.display_name, { message: 'Name the Project.' });
    maxLength(path.display_name, 255, { message: 'Use at most 255 characters.' });
    required(path.slug, { message: 'Give the Project an address.' });
    pattern(path.slug, slugPattern, {
      message: 'Use lower-case letters and digits, with single hyphens between words.',
    });
  });
  protected readonly detailsStatus = new RequestStatus();
  /** What the last save did, for the status beside the button. */
  protected readonly detailsNote = signal('');

  protected async saveDetails(event: Event): Promise<void> {
    event.preventDefault();
    const project = this.project();
    if (!project) {
      return;
    }
    await submit(this.detailsForm, async () => {
      const changes = changed(project, this.detailsModel());
      if (Object.keys(changes).length === 0) {
        this.detailsNote.set('Nothing to save.');
        return undefined;
      }
      this.detailsNote.set('');
      if (await this.detailsStatus.run(() => this.projects.update(project.id, changes))) {
        this.detailsNote.set('Saved.');
        this.details.reload();
      }
      return undefined;
    });
  }

  // Handing it to another Team.

  protected readonly ownerChoice = linkedSignal(() => this.project()?.owner_team_id ?? '');
  protected readonly transferStatus = new RequestStatus();

  protected async transfer(event: Event): Promise<void> {
    event.preventDefault();
    const project = this.project();
    if (!project || this.ownerChoice() === project.owner_team_id) {
      return;
    }
    if (
      await this.transferStatus.run(() => this.projects.transfer(project.id, this.ownerChoice()))
    ) {
      this.details.reload();
    }
  }

  // Archiving and restoring.

  protected readonly confirmingArchive = signal(false);
  protected readonly archiveStatus = new RequestStatus();

  protected async archive(): Promise<void> {
    const project = this.project();
    if (!project) {
      return;
    }
    if (await this.archiveStatus.run(() => this.projects.archive(project.id))) {
      this.confirmingArchive.set(false);
      this.details.reload();
    }
  }

  protected async restore(): Promise<void> {
    const project = this.project();
    if (project && (await this.archiveStatus.run(() => this.projects.restore(project.id)))) {
      this.details.reload();
    }
  }
}

/** The fields of `model` that differ from the Project's, trimmed. */
function changed(
  project: Project,
  model: { display_name: string; slug: string; description: string },
): { display_name?: string; slug?: string; description?: string } {
  const out: { display_name?: string; slug?: string; description?: string } = {};
  if (model.display_name.trim() !== project.display_name) {
    out.display_name = model.display_name.trim();
  }
  if (model.slug !== project.slug) {
    out.slug = model.slug;
  }
  if (model.description.trim() !== project.description) {
    out.description = model.description.trim();
  }
  return out;
}
