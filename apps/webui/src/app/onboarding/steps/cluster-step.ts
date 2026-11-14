import { Component, inject, signal } from '@angular/core';
import { RouterLink } from '@angular/router';
import { HlmAlertImports } from '@spartan-ng/helm/alert';
import { HlmButtonImports } from '@spartan-ng/helm/button';
import { HlmSpinnerImports } from '@spartan-ng/helm/spinner';
import { Environment, OnboardingApi } from '../onboarding-api';
import { ClusterGuide } from '../../delivery/pages/cluster-guide';
import { ApiError } from '../../api/api-client';
import { SetupStep } from '../setup-step';

/**
 * Step 5: connect the first cluster. The Cluster onboarding guide installs
 * the agent and adds the first Targets; the setup's Environments are the ones
 * a Target can belong to.
 */
@Component({
  selector: 'app-cluster-step',
  imports: [RouterLink, SetupStep, ClusterGuide, HlmAlertImports, HlmButtonImports, HlmSpinnerImports],
  templateUrl: './cluster-step.html',
})
export class ClusterStep {
  private readonly api = inject(OnboardingApi);

  protected readonly loading = signal(true);
  protected readonly failure = signal<string | null>(null);
  protected readonly environments = signal<Environment[]>([]);

  constructor() {
    void this.load();
  }

  private async load(): Promise<void> {
    try {
      const workspace = await this.api.defaultWorkspace();
      this.environments.set(workspace ? await this.api.environments(workspace.id) : []);
    } catch (error) {
      this.failure.set(
        error instanceof ApiError ? error.message : 'Environments did not load. Try again.',
      );
    } finally {
      this.loading.set(false);
    }
  }
}
