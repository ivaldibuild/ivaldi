import { Component } from '@angular/core';
import { RouterLink } from '@angular/router';
import { HlmButtonImports } from '@spartan-ng/helm/button';

/** The end of the first-run setup. */
@Component({
  selector: 'app-setup-done',
  imports: [RouterLink, HlmButtonImports],
  template: `
    <div class="flex max-w-205 flex-col gap-8">
      <header class="flex flex-col gap-3">
        <span class="text-eyebrow text-xs font-semibold tracking-[0.24em] uppercase"
          >Setup done</span
        >
        <h1 class="text-heading text-4xl leading-11 font-bold">Your forge is ready</h1>
        <p class="text-foreground text-base leading-relaxed">
          Your Organisation has a Workspace, a Team to own the work, and a path to production. Next
          come Projects and the Applications they ship; the web app's delivery screens are on their
          way.
        </p>
      </header>
      <div class="flex flex-wrap gap-3">
        <a hlmBtn size="lg" routerLink="/applications">Go to Applications</a>
        <a hlmBtn size="lg" variant="outline" routerLink="/setup/organisation">Review setup</a>
      </div>
    </div>
  `,
})
export class SetupDone {}
