import { Component, input } from '@angular/core';
import { RouterLink } from '@angular/router';

/** One step of a breadcrumb: a link when it has somewhere to go. */
export interface Crumb {
  label: string;
  link?: string | unknown[];
}

/**
 * A delivery page's breadcrumb, title and actions (`[pageActions]`). The breadcrumb is either
 * `crumbs`, whose steps link up the hierarchy, or a plain `trail`.
 */
@Component({
  selector: 'app-page-header',
  imports: [RouterLink],
  template: `
    <header class="flex flex-wrap items-end justify-between gap-4">
      <div class="flex flex-col gap-2">
        @if (crumbs(); as steps) {
          <nav aria-label="Breadcrumb" class="text-muted-foreground text-sm">
            <ol class="flex flex-wrap items-center gap-x-1.5">
              @for (step of steps; track $index; let last = $last) {
                <li class="flex items-center gap-x-1.5">
                  @if (step.link) {
                    <a
                      class="text-muted-foreground hover:text-foreground no-underline hover:underline"
                      [routerLink]="step.link"
                      >{{ step.label }}</a
                    >
                  } @else {
                    <span>{{ step.label }}</span>
                  }
                  @if (!last) {
                    <span aria-hidden="true">/</span>
                  }
                </li>
              }
            </ol>
          </nav>
        } @else if (trail()) {
          <span class="text-muted-foreground text-sm">{{ trail() }}</span>
        }
        <h1 class="text-heading text-4xl leading-11 font-bold">{{ heading() }}</h1>
        <ng-content select="[pageMeta]" />
      </div>
      <div class="flex flex-wrap gap-2">
        <ng-content select="[pageActions]" />
      </div>
    </header>
  `,
})
export class PageHeader {
  readonly heading = input.required<string>();
  readonly trail = input<string>();
  readonly crumbs = input<Crumb[]>();
}
