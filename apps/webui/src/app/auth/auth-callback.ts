import { Component, inject, signal } from '@angular/core';
import { ActivatedRoute, Router } from '@angular/router';
import { HlmButtonImports } from '@spartan-ng/helm/button';
import { HlmSpinnerImports } from '@spartan-ng/helm/spinner';
import { Session } from './session';

/**
 * Where the control plane sends the browser back after a sign-in
 * (`auth.web_client.redirect_uris`). It exchanges the code and returns to the page that
 * asked for the sign-in.
 */
@Component({
  selector: 'app-auth-callback',
  imports: [HlmButtonImports, HlmSpinnerImports],
  template: `
    <main class="bg-background text-foreground flex min-h-dvh items-center justify-center px-4">
      @if (failure(); as message) {
        <div class="flex w-full max-w-100 flex-col gap-4">
          <h1 class="text-heading text-4xl leading-11 font-bold">Sign-in didn't finish</h1>
          <p class="text-foreground text-base leading-relaxed">{{ message }}</p>
          <button hlmBtn size="lg" type="button" class="self-start" (click)="retry()">
            Sign in again
          </button>
        </div>
      } @else {
        <p class="text-muted-foreground flex items-center gap-3 text-sm" role="status">
          <hlm-spinner aria-hidden="true" />
          Signing you in…
        </p>
      }
    </main>
  `,
})
export class AuthCallback {
  private readonly session = inject(Session);
  private readonly router = inject(Router);
  protected readonly failure = signal<string | null>(null);

  constructor() {
    void this.complete(inject(ActivatedRoute).snapshot.queryParamMap);
  }

  protected retry(): void {
    void this.session.signIn('/');
  }

  private async complete(query: { get(name: string): string | null }): Promise<void> {
    const error = query.get('error');
    const code = query.get('code');
    const state = query.get('state');
    if (error || !code || !state) {
      this.failure.set(
        error === 'access_denied'
          ? 'The sign-in was cancelled.'
          : 'The control plane did not send back a sign-in to finish.',
      );
      return;
    }
    try {
      await this.router.navigateByUrl(await this.session.completeSignIn(code, state), {
        replaceUrl: true,
      });
    } catch (e) {
      this.failure.set(
        e instanceof Error && e.message.startsWith('This sign-in')
          ? e.message
          : 'The control plane could not finish the sign-in. Try again.',
      );
    }
  }
}
