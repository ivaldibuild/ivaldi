import { Component, computed, inject, signal } from '@angular/core';
import { applyEach, disabled, form, FormField, minLength, submit } from '@angular/forms/signals';
import { Router, RouterLink } from '@angular/router';
import { NgIcon, provideIcons } from '@ng-icons/core';
import { lucideArrowRight, lucidePlus, lucideX } from '@ng-icons/lucide';
import { HlmAlertImports } from '@spartan-ng/helm/alert';
import { HlmButtonImports } from '@spartan-ng/helm/button';
import { HlmInputImports } from '@spartan-ng/helm/input';
import { HlmSpinnerImports } from '@spartan-ng/helm/spinner';
import { Environment, Lifecycle, OnboardingApi, Tier, Workspace } from '../onboarding-api';
import { SetupStep } from '../setup-step';
import { RequestStatus } from '../../shared/request-status';

interface DraftEnvironment {
  display_name: string;
  tier: Tier;
  execution_enabled: boolean;
}

const starter: DraftEnvironment[] = [
  { display_name: 'Development', tier: 'development', execution_enabled: true },
  { display_name: 'Staging', tier: 'staging', execution_enabled: true },
  { display_name: 'Production', tier: 'production', execution_enabled: true },
];

/**
 * Step 4: the Workspace's Environments and a Lifecycle through them. A new Workspace starts
 * from development → staging → production, which the person can change before creating.
 */
@Component({
  selector: 'app-environments-step',
  imports: [
    FormField,
    NgIcon,
    RouterLink,
    SetupStep,
    HlmAlertImports,
    HlmButtonImports,
    HlmInputImports,
    HlmSpinnerImports,
  ],
  providers: [provideIcons({ lucideArrowRight, lucidePlus, lucideX })],
  templateUrl: './environments-step.html',
})
export class EnvironmentsStep {
  private readonly api = inject(OnboardingApi);
  private readonly router = inject(Router);

  protected readonly status = new RequestStatus();
  protected readonly workspace = signal<Workspace | null>(null);
  protected readonly existing = signal<Environment[]>([]);
  protected readonly lifecycles = signal<Lifecycle[]>([]);

  protected readonly tiers: { value: Tier; label: string }[] = [
    { value: 'development', label: 'Development' },
    { value: 'staging', label: 'Staging' },
    { value: 'production', label: 'Production' },
    { value: 'other', label: 'Other' },
  ];

  private readonly model = signal({ environments: starter.map((e) => ({ ...e })), hotfix: true });
  protected readonly plan = form(this.model, (path) => {
    applyEach(path.environments, (environment) => {
      minLength(environment.display_name, 3, { message: 'Use at least 3 characters.' });
      disabled(environment, () => this.locked());
    });
  });

  /** The Hotfix Lifecycle needs somewhere to test and somewhere to ship. */
  protected readonly canHotfix = computed(() => {
    const tiers = this.model().environments.map((e) => e.tier);
    return tiers.includes('staging') && tiers.includes('production');
  });

  /** Set once an Environment exists, so the list can't shift under a retry. */
  protected readonly locked = signal(false);
  private created = new Map<number, Environment>();
  private createdLifecycles = new Set<string>();

  constructor() {
    void this.status.run(async () => {
      const workspace = await this.api.defaultWorkspace();
      this.workspace.set(workspace);
      if (workspace) {
        const [environments, lifecycles] = await Promise.all([
          this.api.environments(workspace.id),
          this.api.lifecycles(workspace.id),
        ]);
        this.existing.set(environments);
        this.lifecycles.set(lifecycles);
      }
    }, 'loading');
  }

  protected environmentName(id: string): string {
    return this.existing().find((e) => e.id === id)?.display_name ?? 'Unknown';
  }

  protected tierLabel(tier: Tier): string {
    return this.tiers.find((t) => t.value === tier)?.label ?? tier;
  }

  protected add(): void {
    this.model.update((m) => ({
      ...m,
      environments: [
        ...m.environments,
        { display_name: '', tier: 'other', execution_enabled: true },
      ],
    }));
  }

  protected remove(index: number): void {
    this.model.update((m) => ({
      ...m,
      environments: m.environments.filter((_, i) => i !== index),
    }));
  }

  protected async create(event: Event): Promise<void> {
    event.preventDefault();
    const workspace = this.workspace();
    if (!workspace || this.model().environments.length === 0) {
      return;
    }
    await submit(this.plan, async () => {
      // A retry after a partial failure picks up where the last attempt stopped.
      const done = await this.status.run(async () => {
        const drafts = this.model().environments;
        for (const [index, draft] of drafts.entries()) {
          if (!this.created.has(index)) {
            this.created.set(
              index,
              await this.api.createEnvironment(workspace.id, {
                display_name: draft.display_name.trim(),
                tier: draft.tier,
                execution_enabled: draft.execution_enabled,
              }),
            );
            this.locked.set(true);
          }
        }
        const environments = drafts.map((_, i) => this.created.get(i)!);
        if (!this.createdLifecycles.has('standard')) {
          await this.api.createLifecycle(workspace.id, {
            display_name: 'Standard',
            description: 'Every Release, through each Environment in order.',
            phases: environments.map((e) => ({ name: e.display_name, environment_ids: [e.id] })),
          });
          this.createdLifecycles.add('standard');
        }
        if (this.model().hotfix && this.canHotfix() && !this.createdLifecycles.has('hotfix')) {
          const ofTier = (tier: Tier) =>
            environments.filter((e) => e.tier === tier).map((e) => e.id);
          await this.api.createLifecycle(workspace.id, {
            display_name: 'Hotfix',
            description: 'Urgent fixes: Staging, then Production.',
            phases: [
              { name: 'Staging', environment_ids: ofTier('staging') },
              { name: 'Production', environment_ids: ofTier('production') },
            ],
          });
          this.createdLifecycles.add('hotfix');
        }
      });
      if (done) {
        await this.router.navigate(['/setup', 'cluster']);
      }
      return undefined;
    });
  }
}
