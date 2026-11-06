import { Component, computed, input } from '@angular/core';
import { NgIcon, provideIcons } from '@ng-icons/core';
import { lucideCircleAlert, lucideInfo } from '@ng-icons/lucide';
import { HlmAlertImports } from '@spartan-ng/helm/alert';
import { Notice } from './sign-in-messages';

/** A sign-in notice: an ember alert for errors, a quiet one for information. */
@Component({
  selector: 'app-sign-in-notice',
  imports: [NgIcon, HlmAlertImports],
  providers: [provideIcons({ lucideCircleAlert, lucideInfo })],
  template: `
    <div
      hlmAlert
      [variant]="isError() ? 'destructive' : 'default'"
      [attr.role]="isError() ? 'alert' : 'status'"
    >
      <ng-icon [name]="isError() ? 'lucideCircleAlert' : 'lucideInfo'" aria-hidden="true" />
      <h2 hlmAlertTitle>{{ notice().title }}</h2>
      <p hlmAlertDescription>{{ notice().body }}</p>
    </div>
  `,
})
export class SignInNotice {
  readonly notice = input.required<Notice>();
  protected readonly isError = computed(() => this.notice().tone === 'error');
}
