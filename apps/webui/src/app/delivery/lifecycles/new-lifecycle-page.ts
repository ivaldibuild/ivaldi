import { Component, computed, inject, resource, signal } from '@angular/core';
import { Router, RouterLink } from '@angular/router';
import { NgIcon, provideIcons } from '@ng-icons/core';
import { lucideArrowDown, lucideArrowUp, lucidePlus } from '@ng-icons/lucide';
import { HlmAlertImports } from '@spartan-ng/helm/alert';
import { HlmButtonImports } from '@spartan-ng/helm/button';
import { HlmFieldImports } from '@spartan-ng/helm/field';
import { HlmInputImports } from '@spartan-ng/helm/input';
import { HlmSpinnerImports } from '@spartan-ng/helm/spinner';
import { ApiError } from '../../api/api-client';
import { RequestStatus } from '../../shared/request-status';
import { DeliveryApi } from '../delivery-api';
import { PageHeader } from '../page-header';
import { WorkspaceContext } from '../workspace-context';
import { LifecyclesApi, PhaseFields } from './lifecycles-api';
import { emptyPhase, PhaseForm, phaseBody, phaseProblem } from './phase-form';

/** A new Lifecycle: its name, and its phases in order, created in one go. */
@Component({
  selector: 'app-new-lifecycle-page',
  imports: [
    NgIcon,
    RouterLink,
    PageHeader,
    PhaseForm,
    HlmAlertImports,
    HlmButtonImports,
    HlmFieldImports,
    HlmInputImports,
    HlmSpinnerImports,
  ],
  providers: [provideIcons({ lucideArrowDown, lucideArrowUp, lucidePlus })],
  templateUrl: './new-lifecycle-page.html',
})
export class NewLifecyclePage {
  private readonly api = inject(LifecyclesApi);
  private readonly delivery = inject(DeliveryApi);
  private readonly router = inject(Router);
  protected readonly context = inject(WorkspaceContext);

  protected readonly environments = resource({
    params: () => this.context.current()?.id,
    loader: ({ params: workspaceId }) => this.delivery.environments(workspaceId),
  });
  protected readonly loadFailure = computed(() => {
    const error = this.environments.error();
    return error ? (error instanceof ApiError ? error.message : 'Something went wrong.') : null;
  });

  protected readonly name = signal('');
  protected readonly description = signal('');
  protected readonly phases = signal<PhaseFields[]>([emptyPhase()]);
  /** Keys the phase forms, so each keeps its own field IDs as phases move. */
  protected readonly keys = signal<number[]>([0]);
  private nextKey = 1;

  protected readonly status = new RequestStatus();
  protected readonly invalid = signal<string | null>(null);

  /** For each phase, the Environments other phases hold and which phase holds each. */
  protected readonly taken = computed(() =>
    this.phases().map((_, i) => {
      const map = new Map<string, string>();
      this.phases().forEach((other, j) => {
        if (j !== i) {
          for (const id of other.environment_ids) {
            map.set(id, other.name.trim() || `phase ${j + 1}`);
          }
        }
      });
      return map;
    }),
  );

  protected setPhase(index: number, phase: PhaseFields): void {
    this.phases.update((phases) => phases.map((p, i) => (i === index ? phase : p)));
  }

  protected addPhase(): void {
    this.phases.update((phases) => [...phases, emptyPhase()]);
    this.keys.update((keys) => [...keys, this.nextKey++]);
  }

  protected removePhase(index: number): void {
    this.phases.update((phases) => phases.filter((_, i) => i !== index));
    this.keys.update((keys) => keys.filter((_, i) => i !== index));
  }

  /** Swaps a phase with the one before (-1) or after (+1) it. */
  protected movePhase(index: number, by: -1 | 1): void {
    const swap = <T>(list: T[]) => {
      const next = [...list];
      [next[index], next[index + by]] = [next[index + by], next[index]];
      return next;
    };
    this.phases.update(swap);
    this.keys.update(swap);
  }

  private problem(): string | null {
    if (!this.name().trim()) {
      return 'Name the Lifecycle.';
    }
    if (this.phases().length === 0) {
      return 'Add at least one phase.';
    }
    for (const [i, phase] of this.phases().entries()) {
      const problem = phaseProblem(phase, i + 1);
      if (problem) {
        return problem;
      }
    }
    return null;
  }

  protected async create(event: Event): Promise<void> {
    event.preventDefault();
    const problem = this.problem();
    this.invalid.set(problem);
    const workspaceId = this.context.current()?.id;
    if (problem || !workspaceId) {
      return;
    }
    let id = '';
    const created = await this.status.run(async () => {
      const lifecycle = await this.api.create(workspaceId, {
        display_name: this.name().trim(),
        description: this.description().trim(),
        phases: this.phases().map(phaseBody),
      });
      id = lifecycle.id;
    });
    if (created) {
      await this.router.navigate(['/lifecycles', id]);
    }
  }
}
