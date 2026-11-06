import { Component, computed, input } from '@angular/core';
import { SetupStepPath, setupSteps, stepNumber } from './setup-steps';

/** The heading and body of one setup step. Actions go in `[setupActions]`. */
@Component({
  selector: 'app-setup-step',
  template: `
    <div class="flex max-w-205 flex-col gap-8">
      <header class="flex flex-col gap-3">
        <span class="text-eyebrow text-xs font-semibold tracking-[0.24em] uppercase">
          Step {{ number() }} · {{ title() }}
        </span>
        <h1 class="text-heading text-4xl leading-11 font-bold">{{ heading() }}</h1>
        <p class="text-foreground text-base leading-relaxed">
          <ng-content select="[setupLead]" />
        </p>
      </header>
      <ng-content />
      <div class="flex flex-wrap gap-3">
        <ng-content select="[setupActions]" />
      </div>
    </div>
  `,
})
export class SetupStep {
  readonly step = input.required<SetupStepPath>();
  readonly heading = input.required<string>();

  protected readonly number = computed(() => stepNumber(this.step()));
  protected readonly title = computed(
    () => setupSteps.find((s) => s.path === this.step())?.title ?? '',
  );
}
