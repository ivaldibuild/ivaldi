import { Component, computed, effect, inject, input, signal, untracked } from '@angular/core';
import { form, FormField, pattern, required, submit } from '@angular/forms/signals';
import { RouterLink } from '@angular/router';
import { NgIcon, provideIcons } from '@ng-icons/core';
import { lucidePlus } from '@ng-icons/lucide';
import { HlmAlertImports } from '@spartan-ng/helm/alert';
import { HlmBadgeImports } from '@spartan-ng/helm/badge';
import { HlmButtonImports } from '@spartan-ng/helm/button';
import { HlmFieldImports } from '@spartan-ng/helm/field';
import { HlmInputImports } from '@spartan-ng/helm/input';
import { HlmSpinnerImports } from '@spartan-ng/helm/spinner';
import { ApiError } from '../../api/api-client';
import {
  Channel,
  Environment,
  Target,
  Variable,
  VariableScope,
  VariableValue,
} from '../../api/models';
import { PermissionsApi } from '../../api/permissions-api';
import { RequestStatus } from '../../shared/request-status';
import { VariableOwner, VariablesApi } from './variables-api';

type Dimension = keyof VariableScope;

/** A value being added to a variable (`valueId` null) or changed. */
interface ValueEditor {
  variableId: string;
  valueId: string | null;
}

const emptyScope = (): VariableScope => ({ environment_ids: [], target_ids: [], channel_ids: [] });

/**
 * The variables a Project's Applications, or one Application, deploy with: each a name with
 * values scoped to Environments, Targets, and Channels. An Application's list also shows the
 * variables its Project gives it, which it can override by declaring one with the same name.
 */
@Component({
  selector: 'app-variable-list',
  imports: [
    FormField,
    NgIcon,
    RouterLink,
    HlmAlertImports,
    HlmBadgeImports,
    HlmButtonImports,
    HlmFieldImports,
    HlmInputImports,
    HlmSpinnerImports,
  ],
  providers: [provideIcons({ lucidePlus })],
  templateUrl: './variable-list.html',
})
export class VariableList {
  private readonly api = inject(VariablesApi);
  private readonly permissionsApi = inject(PermissionsApi);

  /** The Project that owns the variables, or the owning Application's Project. */
  readonly projectId = input.required<string>();
  /** Set to manage one Application's variables rather than the Project's. */
  readonly applicationId = input<string | null>(null);
  readonly workspaceId = input.required<string>();
  /** The Application's Channels, which only its own variables can be scoped to. */
  readonly channels = input<Channel[]>([]);
  /** Names of the Project's Applications, which all deploy with its variables. */
  readonly applications = input<string[]>([]);
  /** An archived owner's variables can't be changed. */
  readonly archived = input(false);

  private readonly owner = computed<VariableOwner>(() => {
    const applicationId = this.applicationId();
    return applicationId ? { applicationId } : { projectId: this.projectId() };
  });

  protected readonly variables = signal<Variable[]>([]);
  protected readonly environments = signal<Environment[]>([]);
  protected readonly targets = signal<Target[]>([]);
  protected readonly status = new RequestStatus();

  /**
   * Whether the person may change variables here (`variable.manage` on the Project). Values
   * scoped to an Environment need it there too, which the control plane checks on saving.
   */
  private readonly canManage = signal(false);
  protected readonly editable = computed(() => this.canManage() && !this.archived());

  /** The owner's own variables, by name. */
  protected readonly own = computed(() =>
    this.variables().filter((v) => this.isOwn(v) && !v.archived_at),
  );

  /** The Project's variables an Application deploys with, and whether it overrides each. */
  protected readonly inherited = computed(() => {
    const overridden = new Set(this.own().map((v) => v.name.toLowerCase()));
    return this.variables()
      .filter((v) => !this.isOwn(v) && !v.archived_at)
      .map((variable) => ({ variable, overridden: overridden.has(variable.name.toLowerCase()) }));
  });

  constructor() {
    effect(() => {
      this.owner();
      this.workspaceId();
      untracked(() => void this.load());
    });
  }

  private isOwn(variable: Variable): boolean {
    const applicationId = this.applicationId();
    return applicationId ? variable.application_id === applicationId : !!variable.project_id;
  }

  private async load(): Promise<void> {
    this.canManage.set(false);
    await this.status.run(async () => {
      // Someone who may read the Project's variables but not the Workspace's Environments still
      // gets the list; what a value is scoped to then shows without its name.
      const [variables, options] = await Promise.all([
        this.api.list(this.owner()),
        this.api.scopeOptions(this.workspaceId()).catch((e: unknown) => {
          if (e instanceof ApiError && e.status === 403) {
            return { environments: [], targets: [] };
          }
          throw e;
        }),
      ]);
      this.variables.set(variables);
      this.environments.set(options.environments);
      this.targets.set(options.targets);
    }, 'loading');
    try {
      const granted = await this.permissionsApi.granted(this.projectId(), ['variable.manage']);
      this.canManage.set(granted.has('variable.manage'));
    } catch {
      // Without an answer, nothing is offered; reading still works.
    }
  }

  private async reload(): Promise<void> {
    this.variables.set(await this.api.list(this.owner()));
  }

  /** What a value applies to, in words. */
  protected appliesTo(value: VariableValue): string {
    const parts = [
      this.names('Environment', value.scope.environment_ids, this.environments()),
      this.names('Target', value.scope.target_ids, this.targets()),
      this.names('Channel', value.scope.channel_ids, this.channels()),
    ].filter(Boolean);
    return parts.length > 0 ? parts.join(' · ') : 'Every Deployment';
  }

  private names(
    kind: string,
    ids: string[],
    known: { id: string; display_name: string }[],
  ): string {
    if (ids.length === 0) {
      return '';
    }
    const labels = ids.map((id) => known.find((k) => k.id === id)?.display_name ?? id);
    return `${kind}${ids.length > 1 ? 's' : ''} ${labels.join(', ')}`;
  }

  /** A refusal's message, with the IDs it names replaced by what people call them. */
  private explain(error: unknown): string {
    if (!(error instanceof ApiError)) {
      return 'Something went wrong. Try again.';
    }
    const known = [...this.environments(), ...this.targets(), ...this.channels()];
    return error.message.replace(
      /\b(?:env|tgt|chn)_[0-9A-Z]{26}\b/g,
      (id) => known.find((k) => k.id === id)?.display_name ?? id,
    );
  }

  /** Runs a change, then shows the variables as they now are. */
  private async change(work: () => Promise<unknown>): Promise<boolean> {
    this.saving.set(true);
    this.error.set(null);
    try {
      await work();
      await this.reload();
      return true;
    } catch (e) {
      this.error.set(this.explain(e));
      return false;
    } finally {
      this.saving.set(false);
    }
  }

  protected readonly saving = signal(false);
  /** Why the last change was refused, shown in the form that asked for it. */
  protected readonly error = signal<string | null>(null);

  // Adding a variable.
  protected readonly adding = signal(false);
  private readonly newModel = signal({ name: '', values_path: '', description: '', value: '' });
  protected readonly newForm = form(this.newModel, (path) => {
    required(path.name, { message: 'Name the variable, such as Database.Host.' });
    pattern(path.name, /^[A-Za-z0-9._-]*$/, {
      message: 'Use letters, digits, dots, hyphens, and underscores.',
    });
  });

  /** Opens the form, empty or set to override one of the Project's variables. */
  protected startAdding(overrides?: Variable): void {
    this.closeForms();
    this.newForm().reset({
      name: overrides?.name ?? '',
      values_path: overrides?.values_path ?? '',
      description: '',
      value: '',
    });
    this.adding.set(true);
  }

  protected async add(event: Event): Promise<void> {
    event.preventDefault();
    await submit(this.newForm, async () => {
      const fields = this.newModel();
      const created = await this.change(() =>
        this.api.create(this.owner(), {
          name: fields.name.trim(),
          values_path: fields.values_path.trim() || undefined,
          description: fields.description.trim() || undefined,
          values: fields.value ? [{ value: fields.value, scope: emptyScope() }] : [],
        }),
      );
      if (created) {
        this.adding.set(false);
      }
      return undefined;
    });
  }

  // Changing where the chart receives a variable, and its description.
  protected readonly editing = signal<string | null>(null);
  protected readonly editPath = signal('');
  protected readonly editDescription = signal('');

  protected startEditing(variable: Variable): void {
    this.closeForms();
    this.editPath.set(variable.values_path ?? '');
    this.editDescription.set(variable.description ?? '');
    this.editing.set(variable.id);
  }

  protected async saveVariable(event: Event, variable: Variable): Promise<void> {
    event.preventDefault();
    const saved = await this.change(() =>
      this.api.update(variable.id, {
        values_path: this.editPath().trim(),
        description: this.editDescription().trim(),
      }),
    );
    if (saved) {
      this.editing.set(null);
    }
  }

  // Adding or changing one value.
  protected readonly valueEditor = signal<ValueEditor | null>(null);
  protected readonly valueText = signal('');
  protected readonly valueScope = signal<VariableScope>(emptyScope());

  protected startValue(variable: Variable, value?: VariableValue): void {
    this.closeForms();
    this.valueText.set(value?.value ?? '');
    this.valueScope.set(
      value
        ? {
            environment_ids: [...value.scope.environment_ids],
            target_ids: [...value.scope.target_ids],
            channel_ids: [...value.scope.channel_ids],
          }
        : emptyScope(),
    );
    this.valueEditor.set({ variableId: variable.id, valueId: value?.id ?? null });
  }

  protected editingValue(variable: Variable): ValueEditor | null {
    const editor = this.valueEditor();
    return editor?.variableId === variable.id ? editor : null;
  }

  protected scoped(dimension: Dimension, id: string): boolean {
    return this.valueScope()[dimension].includes(id);
  }

  protected toggleScope(dimension: Dimension, id: string, checked: boolean): void {
    this.valueScope.update((scope) => ({
      ...scope,
      [dimension]: checked
        ? [...scope[dimension], id]
        : scope[dimension].filter((scopedId) => scopedId !== id),
    }));
  }

  protected async saveValue(event: Event): Promise<void> {
    event.preventDefault();
    const editor = this.valueEditor();
    if (!editor) {
      return;
    }
    const fields = { value: this.valueText(), scope: this.valueScope() };
    const saved = await this.change(() =>
      editor.valueId
        ? this.api.updateValue(editor.variableId, editor.valueId, fields)
        : this.api.addValue(editor.variableId, fields),
    );
    if (saved) {
      this.valueEditor.set(null);
    }
  }

  // Removing a value or archiving a variable, one at a time, after a confirmation.
  protected readonly confirming = signal<string | null>(null);

  protected confirm(id: string): void {
    this.closeForms();
    this.confirming.set(id);
  }

  protected async removeValue(variable: Variable, value: VariableValue): Promise<void> {
    if (await this.change(() => this.api.removeValue(variable.id, value.id))) {
      this.confirming.set(null);
    }
  }

  protected async archive(variable: Variable): Promise<void> {
    if (await this.change(() => this.api.archive(variable.id))) {
      this.confirming.set(null);
    }
  }

  protected closeForms(): void {
    this.adding.set(false);
    this.editing.set(null);
    this.valueEditor.set(null);
    this.confirming.set(null);
    this.error.set(null);
  }
}
