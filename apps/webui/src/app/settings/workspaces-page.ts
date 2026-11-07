import { Component, effect, inject, signal, untracked } from '@angular/core';
import { form, FormField, maxLength, pattern, required, submit } from '@angular/forms/signals';
import { NgIcon, provideIcons } from '@ng-icons/core';
import { lucidePlus } from '@ng-icons/lucide';
import { HlmAlertImports } from '@spartan-ng/helm/alert';
import { HlmBadgeImports } from '@spartan-ng/helm/badge';
import { HlmButtonImports } from '@spartan-ng/helm/button';
import { HlmFieldImports } from '@spartan-ng/helm/field';
import { HlmInputImports } from '@spartan-ng/helm/input';
import { HlmSpinnerImports } from '@spartan-ng/helm/spinner';
import { CurrentUser } from '../api/current-user';
import { Workspace } from '../api/models';
import { WorkspacesApi } from '../api/workspaces-api';
import { slugify } from '../delivery/delivery-api';
import { PageHeader } from '../delivery/page-header';
import { WorkspaceContext } from '../delivery/workspace-context';
import { formatDay } from '../shared/dates';
import { RequestStatus } from '../shared/request-status';

/** A slug the control plane accepts: lower-case letters and digits in words joined by single hyphens. */
const slugPattern = /^[a-z0-9]+(-[a-z0-9]+)*$/;
const slugMessage = 'Use lower-case letters and digits, with single hyphens between words.';

interface WorkspaceFields {
  display_name: string;
  slug: string;
  description: string;
}

const empty: WorkspaceFields = { display_name: '', slug: '', description: '' };

/**
 * The Organisation's Workspaces. Owners and admins create them, rename them, choose the default
 * one new pages start in, and archive the ones no longer used.
 */
@Component({
  selector: 'app-workspaces-page',
  imports: [
    FormField,
    NgIcon,
    PageHeader,
    HlmAlertImports,
    HlmBadgeImports,
    HlmButtonImports,
    HlmFieldImports,
    HlmInputImports,
    HlmSpinnerImports,
  ],
  providers: [provideIcons({ lucidePlus })],
  templateUrl: './workspaces-page.html',
})
export class WorkspacesPage {
  private readonly api = inject(WorkspacesApi);
  private readonly context = inject(WorkspaceContext);
  protected readonly user = inject(CurrentUser);

  protected readonly workspaces = signal<Workspace[]>([]);
  protected readonly showArchived = signal(false);
  protected readonly status = new RequestStatus();
  protected readonly formatDay = formatDay;

  // Creating a Workspace.
  protected readonly creating = signal(false);
  protected readonly createStatus = new RequestStatus();
  private readonly createModel = signal<WorkspaceFields>({ ...empty });
  protected readonly createForm = form(this.createModel, (path) => {
    required(path.display_name, { message: 'Name the Workspace.' });
    maxLength(path.display_name, 255, { message: 'Use at most 255 characters.' });
    required(path.slug, { message: 'Give the Workspace an address.' });
    pattern(path.slug, slugPattern, { message: slugMessage });
    maxLength(path.slug, 63, { message: 'Use at most 63 characters.' });
  });
  /** Whether the slug was typed; until it is, it follows the name. */
  private slugTyped = false;

  // Editing one Workspace at a time.
  protected readonly editing = signal<string | null>(null);
  protected readonly editStatus = new RequestStatus();
  private readonly editModel = signal<WorkspaceFields>({ ...empty });
  protected readonly editForm = form(this.editModel, (path) => {
    required(path.display_name, { message: 'Name the Workspace.' });
    maxLength(path.display_name, 255, { message: 'Use at most 255 characters.' });
    required(path.slug, { message: 'Give the Workspace an address.' });
    pattern(path.slug, slugPattern, { message: slugMessage });
    maxLength(path.slug, 63, { message: 'Use at most 63 characters.' });
  });

  // Making a Workspace the default, archiving and restoring it.
  protected readonly actionStatus = new RequestStatus();
  protected readonly acting = signal<string | null>(null);

  constructor() {
    effect(() => {
      const organizationId = this.user.organization()?.id;
      const includeArchived = this.showArchived();
      if (organizationId) {
        untracked(() => void this.load(includeArchived));
      }
    });
  }

  protected nameChanged(): void {
    if (!this.slugTyped) {
      this.createModel.update((m) => ({ ...m, slug: slugify(m.display_name) }));
    }
  }

  protected slugChanged(): void {
    this.slugTyped = true;
  }

  protected async create(event: Event): Promise<void> {
    event.preventDefault();
    await submit(this.createForm, async () => {
      const { display_name, slug, description } = this.createModel();
      const created = await this.createStatus.run(() =>
        this.api.create({
          display_name: display_name.trim(),
          slug,
          ...(description.trim() ? { description: description.trim() } : {}),
        }),
      );
      if (created) {
        this.cancelCreate();
        await this.refresh();
      }
      return undefined;
    });
  }

  protected cancelCreate(): void {
    this.creating.set(false);
    this.slugTyped = false;
    this.createStatus.error.set(null);
    this.createForm().reset({ ...empty });
  }

  protected edit(workspace: Workspace): void {
    this.editStatus.error.set(null);
    this.editForm().reset({
      display_name: workspace.display_name,
      slug: workspace.slug,
      description: workspace.description ?? '',
    });
    this.editing.set(workspace.id);
  }

  protected cancelEdit(): void {
    this.editing.set(null);
    this.editStatus.error.set(null);
  }

  protected async save(event: Event, workspace: Workspace): Promise<void> {
    event.preventDefault();
    await submit(this.editForm, async () => {
      const { display_name, slug, description } = this.editModel();
      const changes: Partial<WorkspaceFields> = {};
      if (display_name.trim() !== workspace.display_name) {
        changes.display_name = display_name.trim();
      }
      if (slug !== workspace.slug) {
        changes.slug = slug;
      }
      if (description.trim() !== (workspace.description ?? '')) {
        changes.description = description.trim();
      }
      if (Object.keys(changes).length === 0) {
        this.editing.set(null);
        return undefined;
      }
      const saved = await this.editStatus.run(() => this.api.update(workspace.id, changes));
      if (saved) {
        this.editing.set(null);
        await this.refresh();
      }
      return undefined;
    });
  }

  protected makeDefault(workspace: Workspace): Promise<void> {
    return this.act(workspace, () => this.api.makeDefault(workspace.id));
  }

  protected archive(workspace: Workspace): Promise<void> {
    return this.act(workspace, () => this.api.archive(workspace.id));
  }

  protected restore(workspace: Workspace): Promise<void> {
    return this.act(workspace, () => this.api.restore(workspace.id));
  }

  private async act(workspace: Workspace, work: () => Promise<unknown>): Promise<void> {
    this.acting.set(workspace.id);
    const done = await this.actionStatus.run(work);
    this.acting.set(null);
    if (done) {
      await this.refresh();
    }
  }

  /** Reloads this list and the sidebar's Workspace switcher after a change. */
  private async refresh(): Promise<void> {
    await Promise.all([
      this.load(this.showArchived(), 'saving'),
      this.context.reload().catch(() => undefined),
    ]);
  }

  /** Loads the list; a reload after a change keeps the table on screen instead of a spinner. */
  private async load(
    includeArchived: boolean,
    flag: 'loading' | 'saving' = 'loading',
  ): Promise<void> {
    await this.status.run(async () => {
      this.workspaces.set(await this.api.list(includeArchived));
    }, flag);
  }
}
