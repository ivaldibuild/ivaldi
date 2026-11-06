import { Component, computed, inject } from '@angular/core';
import { toSignal } from '@angular/core/rxjs-interop';
import { NavigationEnd, Router, RouterLink, RouterOutlet } from '@angular/router';
import { NgIcon, provideIcons } from '@ng-icons/core';
import { lucideCheck } from '@ng-icons/lucide';
import { filter, map } from 'rxjs';
import { OrganisationBadge, UserBadge } from '../shared/identity';
import { setupSteps } from './setup-steps';

/** The first-run setup: the step rail beside the current step. */
@Component({
  selector: 'app-onboarding-shell',
  imports: [RouterOutlet, RouterLink, NgIcon, OrganisationBadge, UserBadge],
  providers: [provideIcons({ lucideCheck })],
  templateUrl: './onboarding-shell.html',
})
export class OnboardingShell {
  private readonly router = inject(Router);

  private readonly url = toSignal(
    this.router.events.pipe(
      filter((e) => e instanceof NavigationEnd),
      map(() => this.router.url),
    ),
    { initialValue: this.router.url },
  );

  /** The current step, 1 to 5, or 6 when setup is finished. */
  protected readonly current = computed(() => {
    const path = this.url().split('?')[0].split('/').pop();
    const index = setupSteps.findIndex((s) => s.path === path);
    return index < 0 ? setupSteps.length + 1 : index + 1;
  });

  protected readonly progress = computed(
    () => `${Math.round(((Math.min(this.current(), 6) - 1) / setupSteps.length) * 100)}%`,
  );

  protected readonly steps = computed(() =>
    setupSteps.map((step, i) => ({
      ...step,
      number: i + 1,
      state: i + 1 < this.current() ? 'done' : i + 1 === this.current() ? 'current' : 'next',
    })),
  );

  protected readonly total = setupSteps.length;
}
