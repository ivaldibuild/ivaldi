import { Component, computed, effect, inject, input, signal, untracked } from '@angular/core';
import { HlmAlertImports } from '@spartan-ng/helm/alert';
import { HlmButtonImports } from '@spartan-ng/helm/button';
import { HlmSpinnerImports } from '@spartan-ng/helm/spinner';
import { ApiError } from '../../api/api-client';
import {
  ReleaseVariables as ReleaseVariablesView,
  ReleaseVariablesRefresh,
} from '../../api/models';
import { formatDayAndTime } from '../../shared/dates';
import { VariablesApi } from './variables-api';

/**
 * The variables a Release deploys with: the snapshot taken when it was created. Given a Target, it
 * shows the value of each a Deployment there would get and how many others that value beat.
 */
@Component({
  selector: 'app-release-variables',
  imports: [HlmAlertImports, HlmButtonImports, HlmSpinnerImports],
  templateUrl: './release-variables.html',
})
export class ReleaseVariables {
  private readonly api = inject(VariablesApi);

  readonly releaseId = input.required<string>();
  /** The Target to resolve the variables for; without one, the snapshot is shown as taken. */
  readonly targetId = input<string | null>(null);
  /**
   * One of the Release's Deployments, to show the revision it used rather than the current one:
   * what a rollback that restores it deploys.
   */
  readonly deploymentId = input<string | null>(null);
  /** Whether to offer taking a new revision from the Application's variables. */
  readonly refreshable = input(false);

  protected readonly view = signal<ReleaseVariablesView | null>(null);
  protected readonly error = signal<string | null>(null);

  protected readonly revision = computed(() => this.view()?.revision ?? null);
  protected readonly taken = computed(() => {
    const revision = this.revision();
    return revision ? formatDayAndTime(revision.created_at) : '';
  });

  /** One row per variable as the snapshot holds it, with the Project's below the Application's. */
  protected readonly snapshot = computed(() =>
    (this.revision()?.variables ?? []).map((variable) => ({
      name: variable.name,
      source: variable.source === 'project' ? 'Project' : 'Application',
      path: variable.values_path,
      values: variable.values.length,
    })),
  );

  /** One row per variable as the Target gets it. */
  protected readonly resolved = computed(() =>
    (this.view()?.resolved ?? []).map((variable) => ({
      name: variable.name,
      path: variable.values_path,
      value: variable.winner
        ? variable.winner.secret
          ? `Secret at ${variable.winner.secret.path}`
          : variable.winner.value
        : null,
      secret: !!variable.winner?.secret,
      source: variable.winner?.source === 'project' ? 'Project' : 'Application',
      beaten: variable.beaten.length,
    })),
  );

  constructor() {
    effect(() => {
      const releaseId = this.releaseId();
      const targetId = this.targetId();
      const deploymentId = this.deploymentId();
      untracked(() => void this.load(releaseId, targetId, deploymentId));
    });
  }

  // Updating the Release's variables: preview what would change, then take the revision.
  protected readonly preview = signal<ReleaseVariablesRefresh | null>(null);
  protected readonly refreshing = signal(false);
  protected readonly refreshError = signal<string | null>(null);

  /** How many production Targets the update would change, which stops it. */
  protected readonly productionTargets = computed(() => this.preview()?.production.length ?? 0);
  protected readonly productionVariables = computed(() =>
    [...new Set((this.preview()?.production ?? []).flatMap((change) => change.variables))].join(
      ', ',
    ),
  );

  protected async previewRefresh(): Promise<void> {
    await this.refresh(true);
  }

  protected async applyRefresh(): Promise<void> {
    if (await this.refresh(false)) {
      this.preview.set(null);
      await this.load(this.releaseId(), this.targetId(), this.deploymentId());
    }
  }

  private async refresh(preview: boolean): Promise<boolean> {
    this.refreshing.set(true);
    this.refreshError.set(null);
    try {
      const result = await this.api.refreshReleaseVariables(this.releaseId(), preview);
      if (preview) {
        this.preview.set(result);
      }
      return true;
    } catch (e) {
      this.refreshError.set(e instanceof ApiError ? e.message : 'Something went wrong.');
      return false;
    } finally {
      this.refreshing.set(false);
    }
  }

  private async load(
    releaseId: string,
    targetId: string | null,
    deploymentId: string | null,
  ): Promise<void> {
    this.error.set(null);
    try {
      this.view.set(
        await this.api.releaseVariables(
          releaseId,
          targetId ?? undefined,
          deploymentId ?? undefined,
        ),
      );
    } catch (e) {
      this.view.set(null);
      this.error.set(e instanceof ApiError ? e.message : 'Something went wrong.');
    }
  }
}
