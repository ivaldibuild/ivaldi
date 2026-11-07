import { NgOptimizedImage } from '@angular/common';
import { Component, computed, inject } from '@angular/core';
import { CurrentUser } from '../api/current-user';

/** The signed-in person: their initials, name and role on the Organisation. */
@Component({
  selector: 'app-user-badge',
  template: `
    @if (user.profile()) {
      <div class="flex min-w-0 items-center gap-3">
        <span
          aria-hidden="true"
          class="bg-secondary text-secondary-foreground inline-flex size-8 shrink-0 items-center justify-center rounded-[19.5%] text-xs font-semibold"
        >
          {{ user.initials() }}
        </span>
        <span class="flex min-w-0 flex-col group-data-[collapsible=icon]:hidden">
          <span class="text-heading truncate text-sm">{{ user.displayName() }}</span>
          <span class="text-muted-foreground truncate text-xs">
            {{ user.roleLabel() || user.email() }}
          </span>
        </span>
      </div>
    }
  `,
})
export class UserBadge {
  protected readonly user = inject(CurrentUser);
}

/** The signed-in person's Organisation: its logo, or its initial, and its name. */
@Component({
  selector: 'app-organisation-badge',
  imports: [NgOptimizedImage],
  template: `
    @if (organization(); as org) {
      <div class="flex min-w-0 items-center gap-3">
        @if (org.logo_url) {
          <img
            [ngSrc]="org.logo_url"
            width="32"
            height="32"
            alt=""
            class="size-8 shrink-0 rounded-[19.5%] object-cover"
          />
        } @else {
          <span
            aria-hidden="true"
            class="bg-primary text-primary-foreground inline-flex size-8 shrink-0 items-center justify-center rounded-[19.5%] text-[13px] font-bold"
          >
            {{ initial() }}
          </span>
        }
        <span
          class="text-heading truncate text-sm font-semibold group-data-[collapsible=icon]:hidden"
          >{{ org.display_name }}</span
        >
      </div>
    }
  `,
})
export class OrganisationBadge {
  private readonly user = inject(CurrentUser);
  protected readonly organization = this.user.organization;
  protected readonly initial = computed(
    () => this.organization()?.display_name.trim().charAt(0).toUpperCase() ?? '',
  );
}
