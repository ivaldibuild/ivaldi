import { Component, computed, inject, input, linkedSignal, resource, signal } from '@angular/core';
import { form, FormField, maxLength, pattern, required, submit } from '@angular/forms/signals';
import { RouterLink } from '@angular/router';
import { NgIcon, provideIcons } from '@ng-icons/core';
import { lucideArchive } from '@ng-icons/lucide';
import { HlmAlertImports } from '@spartan-ng/helm/alert';
import { HlmBadgeImports } from '@spartan-ng/helm/badge';
import { HlmButtonImports } from '@spartan-ng/helm/button';
import { HlmFieldImports } from '@spartan-ng/helm/field';
import { HlmInputImports } from '@spartan-ng/helm/input';
import { HlmSpinnerImports } from '@spartan-ng/helm/spinner';
import { ApiError } from '../../api/api-client';
import { CurrentUser } from '../../api/current-user';
import { Environment, Target, Tier } from '../../api/models';
import { RequestStatus } from '../../shared/request-status';
import { DeliveryApi, EnvironmentFields, TargetFields } from '../delivery-api';
import { PageHeader } from '../page-header';
import { tierLabels, tierOptions } from './environments-page';
import { slugPattern } from './new-project-page';

const emptyTarget: TargetFields = { display_name: '', cluster: '', namespace: '' };

/** A Target's fields as a form edits them. */
function targetFields(target: Target): TargetFields {
  return {
    display_name: target.display_name,
    cluster: String(target.locator['cluster'] ?? ''),
    namespace: String(target.locator['namespace'] ?? ''),
  };
}

/**
 * One Environment: its name, address, tier and whether Ivaldi deploys there; archiving and
 * restoring it; and its Targets, the namespaces in clusters it runs on.
 */
@Component({
  selector: 'app-environment-page',
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
  providers: [provideIcons({ lucideArchive })],
  templateUrl: './environment-page.html',
})
export class EnvironmentPage {
  private readonly api = inject(DeliveryApi);
  private readonly user = inject(CurrentUser);

  /** The `:id` route parameter. */
  readonly id = input.required<string>();

  /** Environments are managed with environment.manage, which owners and admins hold. */
  protected readonly canManage = this.user.canManageOrganization;
  protected readonly tierOptions = tierOptions;

  protected readonly details = resource({
    params: () => this.id(),
    loader: async ({ params: id }) => {
      const [environment, targets] = await Promise.all([
        this.api.environment(id),
        this.api.targets(id, true),
      ]);
      return { environment, targets };
    },
  });

  /**
   * The loaded value, or undefined while there is none: a resource's value() throws while it's in
   * an error state, which would stop the page from rendering its error.
   */
  private readonly loaded = computed(() =>
    this.details.hasValue() ? this.details.value() : undefined,
  );

  protected readonly environment = computed(() => this.loaded()?.environment ?? null);
  protected readonly archived = computed(() => !!this.environment()?.deleted_at);
  protected readonly targets = computed(() => this.loaded()?.targets ?? []);
  protected readonly failure = computed(() => {
    const error = this.details.error();
    return error ? (error instanceof ApiError ? error.message : 'Something went wrong.') : null;
  });

  protected tierLabel(tier: Tier): string {
    return tierLabels[tier] ?? tier;
  }

  // Changing its details.

  private readonly detailsModel = linkedSignal<EnvironmentFields & { slug: string }>(() => {
    const e = this.environment();
    return {
      display_name: e?.display_name ?? '',
      slug: e?.slug ?? '',
      tier: e?.tier ?? 'development',
      execution_enabled: e?.execution_enabled ?? true,
    };
  });
  protected readonly detailsForm = form(this.detailsModel, (path) => {
    required(path.display_name, { message: 'Name the Environment.' });
    maxLength(path.display_name, 255, { message: 'Use at most 255 characters.' });
    required(path.slug, { message: 'Give the Environment an address.' });
    pattern(path.slug, slugPattern, {
      message: 'Use lower-case letters and digits, with single hyphens between words.',
    });
    maxLength(path.slug, 63, { message: 'Use at most 63 characters.' });
  });
  protected readonly detailsStatus = new RequestStatus();
  /** What the last save did, for the status beside the button. */
  protected readonly detailsNote = signal('');

  protected async saveDetails(event: Event): Promise<void> {
    event.preventDefault();
    const environment = this.environment();
    if (!environment) {
      return;
    }
    await submit(this.detailsForm, async () => {
      const changes = changed(environment, this.detailsModel());
      if (Object.keys(changes).length === 0) {
        this.detailsNote.set('Nothing to save.');
        return undefined;
      }
      this.detailsNote.set('');
      if (await this.detailsStatus.run(() => this.api.updateEnvironment(environment.id, changes))) {
        this.detailsNote.set('Saved.');
        this.details.reload();
      }
      return undefined;
    });
  }

  // Archiving and restoring.

  protected readonly confirmingArchive = signal(false);
  protected readonly archiveStatus = new RequestStatus();

  protected async archive(): Promise<void> {
    const environment = this.environment();
    if (
      environment &&
      (await this.archiveStatus.run(() => this.api.archiveEnvironment(environment.id)))
    ) {
      this.confirmingArchive.set(false);
      this.details.reload();
    }
  }

  protected async restore(): Promise<void> {
    const environment = this.environment();
    if (
      environment &&
      (await this.archiveStatus.run(() => this.api.restoreEnvironment(environment.id)))
    ) {
      this.details.reload();
    }
  }

  // Adding a Target.

  protected readonly addingTarget = signal(false);
  protected readonly targetStatus = new RequestStatus();
  private readonly targetModel = signal<TargetFields>({ ...emptyTarget });
  protected readonly targetForm = form(this.targetModel, (path) => {
    required(path.display_name, { message: 'Name the Target, such as staging-eu-1.' });
    required(path.cluster, { message: 'Give the cluster it is in.' });
    required(path.namespace, { message: 'Give the namespace Ivaldi deploys to.' });
  });

  protected cancelTarget(): void {
    this.addingTarget.set(false);
    this.targetStatus.error.set(null);
    this.targetForm().reset({ ...emptyTarget });
  }

  protected async addTarget(event: Event): Promise<void> {
    event.preventDefault();
    await submit(this.targetForm, async () => {
      const target = trimmed(this.targetModel());
      if (await this.targetStatus.run(() => this.api.createTarget(this.id(), target))) {
        this.cancelTarget();
        this.details.reload();
      }
      return undefined;
    });
  }

  // Changing, archiving, and restoring a Target.

  /** The Target being edited, if any. */
  protected readonly editingTarget = signal<string | null>(null);
  private readonly editModel = signal<TargetFields>({ ...emptyTarget });
  protected readonly editForm = form(this.editModel, (path) => {
    required(path.display_name, { message: 'Name the Target.' });
    required(path.cluster, { message: 'Give the cluster it is in.' });
    required(path.namespace, { message: 'Give the namespace Ivaldi deploys to.' });
  });
  /** Errors and progress for changes to existing Targets. */
  protected readonly targetChange = new RequestStatus();
  protected readonly changingTarget = signal<string | null>(null);

  protected editTarget(target: Target): void {
    this.targetChange.error.set(null);
    this.editForm().reset(targetFields(target));
    this.editingTarget.set(target.id);
  }

  protected cancelEdit(): void {
    this.editingTarget.set(null);
    this.targetChange.error.set(null);
  }

  protected async saveTarget(event: Event, target: Target): Promise<void> {
    event.preventDefault();
    await submit(this.editForm, async () => {
      const fields = trimmed(this.editModel());
      if (await this.targetChange.run(() => this.api.updateTarget(target.id, fields))) {
        this.editingTarget.set(null);
        this.details.reload();
      }
      return undefined;
    });
  }

  protected async archiveTarget(target: Target): Promise<void> {
    await this.changeTarget(target, () => this.api.archiveTarget(target.id));
  }

  protected async restoreTarget(target: Target): Promise<void> {
    await this.changeTarget(target, () => this.api.restoreTarget(target.id));
  }

  private async changeTarget(target: Target, work: () => Promise<unknown>): Promise<void> {
    this.changingTarget.set(target.id);
    const done = await this.targetChange.run(work);
    this.changingTarget.set(null);
    if (done) {
      this.details.reload();
    }
  }
}

/** The fields of `model` that differ from the Environment's, with the name trimmed. */
function changed(
  environment: Environment,
  model: EnvironmentFields & { slug: string },
): Partial<EnvironmentFields> {
  const out: Partial<EnvironmentFields> = {};
  if (model.display_name.trim() !== environment.display_name) {
    out.display_name = model.display_name.trim();
  }
  if (model.slug !== environment.slug) {
    out.slug = model.slug;
  }
  if (model.tier !== environment.tier) {
    out.tier = model.tier;
  }
  if (model.execution_enabled !== environment.execution_enabled) {
    out.execution_enabled = model.execution_enabled;
  }
  return out;
}

function trimmed(target: TargetFields): TargetFields {
  return {
    display_name: target.display_name.trim(),
    cluster: target.cluster.trim(),
    namespace: target.namespace.trim(),
  };
}
