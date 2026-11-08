import { Component, inject, signal } from '@angular/core';
import { ActivatedRoute, ParamMap, Router, RouterLink } from '@angular/router';
import { HlmButtonImports } from '@spartan-ng/helm/button';
import { HlmSpinnerImports } from '@spartan-ng/helm/spinner';
import { ApiError } from '../api/api-client';
import { ConnectionsApi } from '../api/connections-api';
import { BrowserLocation } from '../auth/browser-location';

/** Why the install could not be finished, and whether starting again could help. */
interface Failure {
  message: string;
  canRestart: boolean;
}

/**
 * Where GitHub sends the person back after installing Ivaldi's GitHub App: the App's callback
 * URL, `<web app>/settings/connections/github/callback`. With "Request user authorization
 * (OAuth) during installation" on, GitHub adds `code`, `installation_id` and the `state` from
 * `POST /v1/connections/installations`. The control plane checks the state is this person's,
 * and that GitHub lets them access the installation, before connecting it.
 *
 * The page sits behind sign-in, so the completion carries the person's own token; a lapsed
 * session signs in and comes back here with the query intact.
 */
@Component({
  selector: 'app-github-install-callback',
  imports: [RouterLink, HlmButtonImports, HlmSpinnerImports],
  template: `
    @if (failure(); as f) {
      <div class="flex max-w-150 flex-col gap-4">
        <h1 class="text-heading text-2xl font-bold">GitHub isn't connected</h1>
        <p class="text-foreground text-base leading-relaxed" role="alert">{{ f.message }}</p>
        @if (restartError(); as message) {
          <p class="text-destructive text-sm" role="alert">{{ message }}</p>
        }
        <div class="flex flex-wrap gap-2">
          @if (f.canRestart) {
            <button hlmBtn type="button" [disabled]="restarting()" (click)="restart()">
              @if (restarting()) {
                <hlm-spinner aria-hidden="true" /> Opening GitHub…
              } @else {
                Start again
              }
            </button>
          }
          <a hlmBtn variant="outline" routerLink="/settings/connections">Back to Connections</a>
        </div>
      </div>
    } @else {
      <p class="text-muted-foreground flex items-center gap-3 text-sm" role="status">
        <hlm-spinner aria-hidden="true" />
        Connecting your GitHub account…
      </p>
    }
  `,
})
export class GitHubInstallCallback {
  private readonly api = inject(ConnectionsApi);
  private readonly router = inject(Router);
  private readonly location = inject(BrowserLocation);

  protected readonly failure = signal<Failure | null>(null);
  protected readonly restarting = signal(false);
  protected readonly restartError = signal<string | null>(null);

  constructor() {
    void this.complete(inject(ActivatedRoute).snapshot.queryParamMap);
  }

  protected async restart(): Promise<void> {
    this.restarting.set(true);
    this.restartError.set(null);
    try {
      this.location.assign((await this.api.startInstallation('github')).url);
    } catch (e) {
      this.restarting.set(false);
      this.restartError.set(e instanceof ApiError ? e.message : 'Something went wrong. Try again.');
    }
  }

  private async complete(query: ParamMap): Promise<void> {
    const failure = missing(query);
    if (failure) {
      this.failure.set(failure);
      return;
    }
    try {
      const connection = await this.api.completeInstallation({
        provider: 'github',
        state: query.get('state')!,
        code: query.get('code')!,
        installation_id: query.get('installation_id')!,
      });
      // Replacing the URL keeps the single-use code out of the address bar and history.
      await this.router.navigate(['/settings/connections'], {
        queryParams: {
          connected: connection.external_account_name ?? connection.external_account_id,
        },
        replaceUrl: true,
      });
    } catch (e) {
      this.failure.set(refused(e));
    }
  }
}

/** Explains a callback that lacks what the control plane needs, or null when it has it all. */
function missing(query: ParamMap): Failure | null {
  const error = query.get('error');
  if (error) {
    return {
      message:
        error === 'access_denied'
          ? 'The install was cancelled on GitHub.'
          : `GitHub stopped the install (${error}).`,
      canRestart: true,
    };
  }
  if (!query.get('state')) {
    return {
      message:
        'This install was not started from Ivaldi, so Ivaldi cannot tell who it is for. If the App is already installed on the account, start again from Ivaldi to connect it.',
      canRestart: true,
    };
  }
  if (!query.get('code')) {
    return {
      message:
        'GitHub did not confirm who installed the App. Ask whoever runs Ivaldi to turn on "Request user authorization (OAuth) during installation" for its GitHub App.',
      canRestart: false,
    };
  }
  if (!query.get('installation_id')) {
    return { message: 'GitHub did not say which installation to connect.', canRestart: true };
  }
  return null;
}

function refused(e: unknown): Failure {
  if (e instanceof ApiError && e.status === 409) {
    return {
      message:
        'This GitHub account is already connected, to this Organisation or another. An account can be connected to one Organisation at a time.',
      canRestart: false,
    };
  }
  return {
    message: e instanceof ApiError ? e.message : 'Something went wrong. Try again.',
    canRestart: !(e instanceof ApiError && e.status === 403),
  };
}
