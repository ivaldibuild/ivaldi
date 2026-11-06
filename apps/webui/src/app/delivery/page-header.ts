import { Component, input } from '@angular/core';

/** A delivery page's breadcrumb, title and actions (`[pageActions]`). */
@Component({
  selector: 'app-page-header',
  template: `
    <header class="flex flex-wrap items-end justify-between gap-4">
      <div class="flex flex-col gap-2">
        @if (trail()) {
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
}
