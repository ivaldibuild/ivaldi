import { Component } from '@angular/core';
import { RouterLink } from '@angular/router';
import { HlmButtonImports } from '@spartan-ng/helm/button';
import { SetupStep } from '../setup-step';

const docs = 'https://github.com/ivaldibuild/ivaldi/blob/main/docs/self-hosting/sign-in.md';

/**
 * Step 2: sign-in methods. On a self-hosted installation they come from the control
 * plane's configuration, so this step says where to set them.
 */
@Component({
  selector: 'app-sign-in-step',
  imports: [RouterLink, SetupStep, HlmButtonImports],
  template: `
    <app-setup-step step="sign-in" heading="Choose how people sign in">
      <ng-container setupLead>
        Sign-in methods are part of the control plane's configuration, so they're set where you run
        it, not here. Agents never sign in this way: they act through Sessions.
      </ng-container>

      <ul class="border-border flex flex-col border">
        @for (method of methods; track method.name) {
          <li class="border-border flex flex-col gap-2 border-b p-6 last:border-b-0">
            <h2 class="text-heading text-base font-semibold">{{ method.name }}</h2>
            <p class="text-muted-foreground text-sm leading-relaxed">{{ method.description }}</p>
            <code class="bg-muted text-foreground self-start px-2 py-1 text-[13px]">{{
              method.setting
            }}</code>
          </li>
        }
      </ul>

      <p class="text-foreground text-sm leading-relaxed">
        Keep at least two ways in for owners: if a provider goes down, an owner with a password can
        still sign in.
        <a [href]="docs" target="_blank" rel="noopener">How to set up sign-in</a>
      </p>

      <ng-container setupActions>
        <a hlmBtn size="lg" routerLink="/setup/team">Continue</a>
        <a hlmBtn size="lg" variant="outline" routerLink="/setup/organisation">Back</a>
      </ng-container>
    </app-setup-step>
  `,
})
export class SignInStep {
  protected readonly docs = docs;
  protected readonly methods = [
    {
      name: 'Email and password',
      description:
        'For accounts made at bootstrap. There is no self-service sign-up or password reset yet.',
      setting: 'IVALDI_AUTH_BUILTIN_ACCOUNTS',
    },
    {
      name: 'Your own identity provider',
      description:
        'Keycloak, Entra ID, Okta and any other OpenID Connect provider. Limit who may join by email domain, and create accounts on first sign-in if you choose.',
      setting: 'IVALDI_AUTH_OIDC_ISSUER_URL, IVALDI_AUTH_OIDC_CLIENT_ID',
    },
    {
      name: 'GitHub',
      description: 'Limit it to members of your GitHub organisations.',
      setting: 'IVALDI_AUTH_GITHUB_CLIENT_ID, IVALDI_AUTH_GITHUB_ALLOWED_ORGANIZATIONS',
    },
    {
      name: 'Google',
      description: 'Limit it to your Google Workspace domains.',
      setting: 'IVALDI_AUTH_GOOGLE_CLIENT_ID, IVALDI_AUTH_GOOGLE_ALLOWED_DOMAINS',
    },
  ];
}
