import { NgTemplateOutlet } from '@angular/common';
import { Component, computed, inject, input, resource, signal } from '@angular/core';
import { NgIcon, provideIcons } from '@ng-icons/core';
import { lucideArrowDown, lucideArrowUp, lucidePlus } from '@ng-icons/lucide';
import { HlmAlertImports } from '@spartan-ng/helm/alert';
import { HlmBadgeImports } from '@spartan-ng/helm/badge';
import { HlmButtonImports } from '@spartan-ng/helm/button';
import { HlmFieldImports } from '@spartan-ng/helm/field';
import { HlmInputImports } from '@spartan-ng/helm/input';
import { HlmSpinnerImports } from '@spartan-ng/helm/spinner';
import { ApiError } from '../../api/api-client';
import { Lifecycle, LifecyclePhase } from '../../api/models';
import { PermissionsApi } from '../../api/permissions-api';
import { RequestStatus } from '../../shared/request-status';
import { DeliveryApi } from '../delivery-api';
import { PageHeader } from '../page-header';
import { LifecyclesApi, PhaseFields } from './lifecycles-api';
import { emptyPhase, PhaseForm, phaseBody, phaseProblem } from './phase-form';

/**
 * One Lifecycle: its phases in order, and, for someone who may manage Lifecycles, renaming it,
 * changing its phases, making it the default for new Applications, and archiving it. Each change
 * saves on its own.
 */
@Component({
  selector: 'app-lifecycle-page',
  imports: [
    NgIcon,
    NgTemplateOutlet,
    PageHeader,
    PhaseForm,
    HlmAlertImports,
    HlmBadgeImports,
    HlmButtonImports,
    HlmFieldImports,
    HlmInputImports,
    HlmSpinnerImports,
  ],
  providers: [provideIcons({ lucideArrowDown, lucideArrowUp, lucidePlus })],
  templateUrl: './lifecycle-page.html',
})
export class LifecyclePage {
  private readonly api = inject(LifecyclesApi);
  private readonly delivery = inject(DeliveryApi);
  private readonly permissions = inject(PermissionsApi);

  /** The `:id` route parameter. */
  readonly id = input.required<string>();

  protected readonly data = resource({
    params: () => this.id(),
    loader: async ({ params: id }) => {
      const lifecycle = await this.api.get(id);
      const [environments, granted, setting] = await Promise.all([
        this.delivery.environments(lifecycle.workspace_id, true),
        this.permissions
          .granted(lifecycle.workspace_id, ['lifecycle.manage'])
          .catch(() => new Set<string>()),
        this.api.workspaceDefault(lifecycle.workspace_id).catch(() => null),
      ]);
      return { lifecycle, environments, canManage: granted.has('lifecycle.manage'), setting };
    },
  });

  /** The loaded value, kept while a reload is under way so the page doesn't flicker. */
  private readonly loaded = computed(() => (this.data.hasValue() ? this.data.value() : undefined));
  protected readonly lifecycle = computed(() => this.loaded()?.lifecycle ?? null);
  protected readonly archived = computed(() => !!this.lifecycle()?.deleted_at);
  /** Changes are offered to someone who may make them, on a Lifecycle that isn't archived. */
  protected readonly canEdit = computed(() => !!this.loaded()?.canManage && !this.archived());
  protected readonly canManage = computed(() => !!this.loaded()?.canManage);
  protected readonly isDefault = computed(
    () => !!this.lifecycle() && this.loaded()?.setting?.value === this.lifecycle()!.id,
  );
  /** The Workspace's active Environments, which phases may choose from. */
  protected readonly activeEnvironments = computed(() =>
    (this.loaded()?.environments ?? []).filter((e) => !e.deleted_at),
  );
  protected readonly phases = computed(() =>
    [...(this.lifecycle()?.phases ?? [])].sort((a, b) => a.position - b.position),
  );

  protected readonly failure = computed(() => {
    const error = this.data.error();
    return error ? (error instanceof ApiError ? error.message : 'Something went wrong.') : null;
  });

  /** A phase's Environments by name; an archived one keeps its name. */
  protected environmentNames(phase: LifecyclePhase): string {
    const environments = this.loaded()?.environments ?? [];
    return phase.environment_ids
      .map((id) => environments.find((e) => e.id === id)?.display_name ?? id)
      .join(', ');
  }

  /** What a phase needs before the next one opens, in words. */
  protected needs(phase: LifecyclePhase): string {
    const total = phase.environment_ids.length;
    const count = phase.min_environments ?? total;
    if (total === 1) {
      return 'A healthy Deployment';
    }
    return count === total
      ? `Healthy Deployments in all ${total}`
      : `Healthy Deployments in ${count} of ${total}`;
  }

  // The name and description.

  protected readonly editingDetails = signal(false);
  protected readonly nameField = signal('');
  protected readonly descriptionField = signal('');
  protected readonly detailsStatus = new RequestStatus();

  protected editDetails(): void {
    const lifecycle = this.lifecycle();
    if (!lifecycle) {
      return;
    }
    this.nameField.set(lifecycle.display_name);
    this.descriptionField.set(lifecycle.description ?? '');
    this.detailsStatus.error.set(null);
    this.editingDetails.set(true);
  }

  protected async saveDetails(event: Event): Promise<void> {
    event.preventDefault();
    if (!this.nameField().trim()) {
      this.detailsStatus.error.set('Name the Lifecycle.');
      return;
    }
    const saved = await this.detailsStatus.run(() =>
      this.api.update(this.id(), {
        display_name: this.nameField().trim(),
        description: this.descriptionField().trim(),
      }),
    );
    if (saved) {
      this.editingDetails.set(false);
      this.data.reload();
    }
  }

  // Phases: one is edited, added, or confirmed for removal at a time.

  /** The phase being edited, `new` for one being added, or null. */
  protected readonly editing = signal<string | null>(null);
  protected readonly draft = signal<PhaseFields>(emptyPhase());
  protected readonly removing = signal<string | null>(null);
  protected readonly phaseStatus = new RequestStatus();

  /** The Environments in phases other than the one being edited, and the phase each is in. */
  protected readonly taken = computed(() => {
    const map = new Map<string, string>();
    for (const phase of this.phases()) {
      if (phase.id !== this.editing()) {
        for (const id of phase.environment_ids) {
          map.set(id, phase.name);
        }
      }
    }
    return map;
  });

  protected editPhase(phase: LifecyclePhase): void {
    this.draft.set({
      name: phase.name,
      environment_ids: [...phase.environment_ids],
      optional: phase.optional ?? false,
      ...(phase.min_environments ? { min_environments: phase.min_environments } : {}),
    });
    this.startEditing(phase.id);
  }

  protected addPhase(): void {
    this.draft.set(emptyPhase());
    this.startEditing('new');
  }

  private startEditing(id: string): void {
    this.phaseStatus.error.set(null);
    this.removing.set(null);
    this.editing.set(id);
  }

  protected async savePhase(event: Event): Promise<void> {
    event.preventDefault();
    const editing = this.editing();
    const position = editing === 'new' ? this.phases().length + 1 : this.position(editing) + 1;
    const problem = phaseProblem(this.draft(), position);
    if (problem) {
      this.phaseStatus.error.set(problem);
      return;
    }
    const body = phaseBody(this.draft());
    await this.change(() =>
      editing === 'new'
        ? this.api.addPhase(this.id(), body)
        : this.api.updatePhase(this.id(), editing!, body),
    );
  }

  /** Moves a phase one place earlier (-1) or later (+1). */
  protected movePhase(phase: LifecyclePhase, by: -1 | 1): Promise<void> {
    return this.change(() => this.api.movePhase(this.id(), phase.id, this.position(phase.id) + by));
  }

  protected removePhase(phase: LifecyclePhase): Promise<void> {
    return this.change(() => this.api.removePhase(this.id(), phase.id));
  }

  protected confirmRemove(phase: LifecyclePhase): void {
    this.phaseStatus.error.set(null);
    this.editing.set(null);
    this.removing.set(phase.id);
  }

  private position(phaseId: string | null): number {
    return this.phases().findIndex((p) => p.id === phaseId);
  }

  private async change(work: () => Promise<Lifecycle>): Promise<void> {
    if (await this.phaseStatus.run(work)) {
      this.editing.set(null);
      this.removing.set(null);
      this.data.reload();
    }
  }

  // The Workspace's default, archiving, and restoring.

  protected readonly defaultStatus = new RequestStatus();
  protected readonly confirmingArchive = signal(false);
  protected readonly archiveStatus = new RequestStatus();

  protected async makeDefault(): Promise<void> {
    const lifecycle = this.lifecycle();
    if (!lifecycle) {
      return;
    }
    const version = this.loaded()?.setting?.version ?? 0;
    if (
      await this.defaultStatus.run(() =>
        this.api.makeDefault(lifecycle.workspace_id, lifecycle.id, version),
      )
    ) {
      this.data.reload();
    }
  }

  protected async archive(): Promise<void> {
    if (await this.archiveStatus.run(() => this.api.archive(this.id()))) {
      this.confirmingArchive.set(false);
      this.data.reload();
    }
  }

  protected async restore(): Promise<void> {
    if (await this.archiveStatus.run(() => this.api.restore(this.id()))) {
      this.data.reload();
    }
  }
}
