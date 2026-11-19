import { Component, computed, input, model } from '@angular/core';
import { HlmFieldImports } from '@spartan-ng/helm/field';
import { HlmInputImports } from '@spartan-ng/helm/input';
import { Environment } from '../../api/models';
import { PhaseFields } from './lifecycles-api';

/** A phase with nothing chosen yet. */
export const emptyPhase = (): PhaseFields => ({ name: '', environment_ids: [], optional: false });

/** What stops a phase from being saved, in words, or null. */
export function phaseProblem(phase: PhaseFields, position: number): string | null {
  if (!phase.name.trim()) {
    return `Name phase ${position}.`;
  }
  if (phase.environment_ids.length === 0) {
    return `Choose at least one Environment for ${phase.name.trim()}.`;
  }
  return null;
}

/** The phase as the control plane takes it: a trimmed name, and no count when all are needed. */
export function phaseBody(phase: PhaseFields): PhaseFields {
  const count = phase.min_environments;
  return {
    name: phase.name.trim(),
    environment_ids: phase.environment_ids,
    optional: phase.optional,
    ...(count && count < phase.environment_ids.length ? { min_environments: count } : {}),
  };
}

/**
 * The fields of one Lifecycle phase: its name, its Environments, how many of them need a healthy
 * Deployment, and whether a Release may skip it. An Environment can be in only one phase, so
 * those already in another are shown but can't be chosen.
 */
@Component({
  selector: 'app-phase-form',
  imports: [HlmFieldImports, HlmInputImports],
  template: `
    <div class="grid gap-4 md:grid-cols-2">
      <div hlmField>
        <label hlmFieldLabel [for]="idPrefix() + '-name'">Phase name</label>
        <input
          hlmInput
          [id]="idPrefix() + '-name'"
          placeholder="Staging"
          [value]="phase().name"
          (input)="setName($event)"
        />
      </div>
      <div hlmField>
        @if (phase().environment_ids.length > 1) {
          <label hlmFieldLabel [for]="idPrefix() + '-count'">Healthy Deployments needed</label>
          <select
            class="border-input bg-background text-foreground h-9 border px-2"
            [id]="idPrefix() + '-count'"
            [attr.aria-describedby]="idPrefix() + '-count-hint'"
            (change)="setCount($event)"
          >
            @for (n of counts(); track n) {
              <option [value]="n" [selected]="n === count()">
                {{ n === phase().environment_ids.length ? 'All of them' : n }}
              </option>
            }
          </select>
          <p [id]="idPrefix() + '-count-hint'" class="text-muted-foreground text-[13px]">
            How many of the phase's Environments need one before the next phase opens.
          </p>
        }
      </div>
      <fieldset class="flex flex-col gap-2 md:col-span-2">
        <legend class="text-sm font-semibold">Environments</legend>
        @for (environment of environments(); track environment.id) {
          <label class="flex items-center gap-2 text-sm">
            <input
              type="checkbox"
              class="accent-primary size-4"
              [checked]="phase().environment_ids.includes(environment.id)"
              [disabled]="taken().has(environment.id)"
              (change)="toggle(environment.id, $event)"
            />
            {{ environment.display_name }}
            @if (taken().get(environment.id); as other) {
              <span class="text-muted-foreground">(in {{ other }})</span>
            }
          </label>
        } @empty {
          <p class="text-muted-foreground text-sm">The Workspace has no Environments yet.</p>
        }
      </fieldset>
      <label class="flex items-center gap-2 text-sm md:col-span-2">
        <input
          type="checkbox"
          class="accent-primary size-4"
          [checked]="phase().optional"
          (change)="setOptional($event)"
        />
        Optional: a Release may skip this phase
      </label>
    </div>
  `,
})
export class PhaseForm {
  readonly phase = model.required<PhaseFields>();
  /** The Workspace's active Environments. */
  readonly environments = input.required<Environment[]>();
  /** Environments in other phases, and the phase each is in. */
  readonly taken = input<ReadonlyMap<string, string>>(new Map());
  /** Makes the fields' IDs unique on a page holding several phases. */
  readonly idPrefix = input.required<string>();

  protected readonly count = computed(
    () => this.phase().min_environments ?? this.phase().environment_ids.length,
  );
  protected readonly counts = computed(() =>
    Array.from({ length: this.phase().environment_ids.length }, (_, i) => i + 1),
  );

  protected setName(event: Event): void {
    const name = (event.target as HTMLInputElement).value;
    this.phase.update((p) => ({ ...p, name }));
  }

  protected setCount(event: Event): void {
    const count = Number((event.target as HTMLSelectElement).value);
    this.phase.update((p) => ({ ...p, min_environments: count }));
  }

  protected setOptional(event: Event): void {
    const optional = (event.target as HTMLInputElement).checked;
    this.phase.update((p) => ({ ...p, optional }));
  }

  /** Chooses or drops an Environment; a count above the new total is dropped too. */
  protected toggle(id: string, event: Event): void {
    const checked = (event.target as HTMLInputElement).checked;
    this.phase.update((p) => {
      const environment_ids = checked
        ? [...p.environment_ids, id]
        : p.environment_ids.filter((other) => other !== id);
      const { min_environments, ...rest } = p;
      return min_environments && min_environments < environment_ids.length
        ? { ...rest, environment_ids, min_environments }
        : { ...rest, environment_ids };
    });
  }
}
