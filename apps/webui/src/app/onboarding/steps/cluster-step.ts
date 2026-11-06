import { Component, inject, signal } from '@angular/core';
import { form, FormField, minLength, pattern, required, submit } from '@angular/forms/signals';
import { RouterLink } from '@angular/router';
import { NgIcon, provideIcons } from '@ng-icons/core';
import { lucideCircleCheck, lucideInfo } from '@ng-icons/lucide';
import { HlmAlertImports } from '@spartan-ng/helm/alert';
import { HlmButtonImports } from '@spartan-ng/helm/button';
import { HlmFieldImports } from '@spartan-ng/helm/field';
import { HlmInputImports } from '@spartan-ng/helm/input';
import { HlmSpinnerImports } from '@spartan-ng/helm/spinner';
import { Environment, OnboardingApi, Target } from '../onboarding-api';
import { SetupStep } from '../setup-step';
import { RequestStatus } from '../../shared/request-status';

const dnsLabel = /^[a-z0-9]+(?:-[a-z0-9]+)*$/;

/**
 * Step 5: a Target, the Kubernetes namespace an Environment runs in. The runtime agent that
 * reports on it and applies Deployments isn't released yet, so this only records where.
 */
@Component({
  selector: 'app-cluster-step',
  imports: [
    FormField,
    NgIcon,
    RouterLink,
    SetupStep,
    HlmAlertImports,
    HlmButtonImports,
    HlmFieldImports,
    HlmInputImports,
    HlmSpinnerImports,
  ],
  providers: [provideIcons({ lucideCircleCheck, lucideInfo })],
  templateUrl: './cluster-step.html',
})
export class ClusterStep {
  private readonly api = inject(OnboardingApi);

  protected readonly status = new RequestStatus();
  protected readonly environments = signal<Environment[]>([]);
  protected readonly target = signal<Target | null>(null);

  private readonly model = signal({
    environment_id: '',
    display_name: '',
    cluster: '',
    namespace: '',
  });
  protected readonly targetForm = form(this.model, (path) => {
    required(path.environment_id, { message: 'Choose an Environment.' });
    minLength(path.display_name, 3, { message: 'Use at least 3 characters.' });
    required(path.cluster, { message: 'Name the cluster.' });
    pattern(path.cluster, /^\S+$/, { message: 'Leave out spaces.' });
    required(path.namespace, { message: 'Give the namespace.' });
    pattern(path.namespace, dnsLabel, {
      message: 'Use a Kubernetes namespace: lower-case letters, digits and hyphens.',
    });
  });

  constructor() {
    void this.status.run(async () => {
      const workspace = await this.api.defaultWorkspace();
      const environments = workspace ? await this.api.environments(workspace.id) : [];
      this.environments.set(environments);
      const first = environments.find((e) => e.tier === 'staging') ?? environments[0];
      if (first) {
        this.model.update((m) => ({ ...m, environment_id: first.id }));
      }
    }, 'loading');
  }

  protected environmentName(id: string): string {
    return this.environments().find((e) => e.id === id)?.display_name ?? '';
  }

  protected async create(event: Event): Promise<void> {
    event.preventDefault();
    await submit(this.targetForm, async () => {
      const { environment_id, ...target } = this.model();
      await this.status.run(async () =>
        this.target.set(
          await this.api.createTarget(environment_id, {
            display_name: target.display_name.trim(),
            cluster: target.cluster.trim(),
            namespace: target.namespace.trim(),
          }),
        ),
      );
      return undefined;
    });
  }
}
