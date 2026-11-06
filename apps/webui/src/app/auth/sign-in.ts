import { Component, computed, inject, signal } from '@angular/core';
import { email, form, FormField, required, submit } from '@angular/forms/signals';
import { ParamMap, ActivatedRoute } from '@angular/router';
import { NgIcon, provideIcons } from '@ng-icons/core';
import { lucideGithub, lucideGlobe, lucideKeyRound } from '@ng-icons/lucide';
import { HlmButtonImports } from '@spartan-ng/helm/button';
import { HlmFieldImports } from '@spartan-ng/helm/field';
import { HlmInputImports } from '@spartan-ng/helm/input';
import { HlmSpinnerImports } from '@spartan-ng/helm/spinner';
import { BrowserLocation } from './browser-location';
import { SignInNotice } from './sign-in-notice';
import { LoginRequest, LoginStep, OrganizationOption, SignInApi, SignInError } from './sign-in-api';
import {
  chooseOrganisationUnavailable,
  expired,
  failureNotice,
  noAccount,
  noMethods,
  nothingToContinue,
  Notice,
  providerNotice,
  signedOut,
} from './sign-in-messages';

type View =
  | { kind: 'loading' }
  | { kind: 'form'; request: LoginRequest }
  | { kind: 'organizations'; loginSession: string; organizations: OrganizationOption[] }
  | { kind: 'redirecting' }
  | { kind: 'ended'; notice: Notice };

const providerIcons: Record<string, string> = {
  github: 'lucideGithub',
  google: 'lucideGlobe',
};

/**
 * The sign-in page the control plane sends browsers to with a login challenge
 * (`auth.login_url`). It renders the choices `GET /v1/auth/login` offers, runs each
 * step, and hands the browser back to the control plane to finish. See
 * docs/design/sign-in-flow.md.
 */
@Component({
  selector: 'app-sign-in',
  imports: [
    FormField,
    NgIcon,
    HlmButtonImports,
    HlmFieldImports,
    HlmInputImports,
    HlmSpinnerImports,
    SignInNotice,
  ],
  providers: [provideIcons({ lucideGithub, lucideGlobe, lucideKeyRound })],
  templateUrl: './sign-in.html',
})
export class SignIn {
  private readonly api = inject(SignInApi);
  private readonly location = inject(BrowserLocation);

  protected readonly host = this.location.host;
  protected readonly view = signal<View>({ kind: 'loading' });
  protected readonly notice = signal<Notice | null>(null);
  protected readonly busy = signal(false);

  protected readonly formView = computed(() => {
    const view = this.view();
    return view.kind === 'form' ? view : null;
  });
  protected readonly organizationsView = computed(() => {
    const view = this.view();
    return view.kind === 'organizations' ? view : null;
  });
  protected readonly ended = computed(() => {
    const view = this.view();
    return view.kind === 'ended' ? view.notice : null;
  });

  private readonly credentials = signal({ email: '', password: '' });
  protected readonly loginForm = form(this.credentials, (path) => {
    required(path.email, { message: 'Enter your email.' });
    email(path.email, { message: 'Enter an email address, like you@example.com.' });
    required(path.password, { message: 'Enter your password.' });
  });

  private loginChallenge = '';

  constructor() {
    void this.start(inject(ActivatedRoute).snapshot.queryParamMap);
  }

  protected providerIcon(kind: string): string {
    return providerIcons[kind] ?? 'lucideKeyRound';
  }

  protected async signInWithPassword(event: Event): Promise<void> {
    event.preventDefault();
    if (this.busy()) {
      return;
    }
    await submit(this.loginForm, async () => {
      const { email, password } = this.credentials();
      await this.run(() => this.api.signInWithPassword(this.loginChallenge, email, password));
      return undefined;
    });
  }

  protected async chooseOrganization(organization: OrganizationOption): Promise<void> {
    const view = this.view();
    if (view.kind !== 'organizations' || this.busy()) {
      return;
    }
    await this.run(() =>
      this.api.chooseOrganization(this.loginChallenge, view.loginSession, organization.id),
    );
  }

  private async start(query: ParamMap): Promise<void> {
    const challenge = query.get('login_challenge');
    const error = query.get('error');
    if (!challenge) {
      // Without a challenge there is no sign-in to continue: the person came here
      // directly, signed out, or the authorization request itself was refused.
      const notice = error
        ? error === 'login_expired'
          ? expired
          : providerNotice(error)
        : query.get('logged_out') === 'true'
          ? signedOut
          : nothingToContinue;
      this.view.set({ kind: 'ended', notice });
      return;
    }
    this.loginChallenge = challenge;

    if (query.has('login_session')) {
      // A provider sign-in that still needs an organization. The page has no way to
      // list the person's organizations for this session, so it offers the password
      // step, which does.
      this.notice.set(chooseOrganisationUnavailable);
    } else if (query.has('signup_token')) {
      this.notice.set(noAccount);
    } else if (error) {
      if (error === 'login_expired') {
        this.view.set({ kind: 'ended', notice: expired });
        return;
      }
      this.notice.set(providerNotice(error));
    }

    try {
      const request = await this.api.loginRequest(challenge);
      if (!request.password && request.providers.length === 0) {
        this.view.set({ kind: 'ended', notice: noMethods });
        return;
      }
      this.view.set({ kind: 'form', request });
    } catch (e) {
      this.view.set({ kind: 'ended', notice: this.noticeFor(e) });
    }
  }

  /** Runs one sign-in step and follows its answer. */
  private async run(step: () => Promise<LoginStep>): Promise<void> {
    this.busy.set(true);
    this.notice.set(null);
    try {
      this.follow(await step());
    } catch (e) {
      const notice = this.noticeFor(e);
      if (notice === expired) {
        this.view.set({ kind: 'ended', notice });
      } else {
        this.notice.set(notice);
        this.loginForm.password().reset('');
      }
    } finally {
      this.busy.set(false);
    }
  }

  private follow(step: LoginStep): void {
    if (step.redirect_to) {
      this.view.set({ kind: 'redirecting' });
      this.location.assign(step.redirect_to);
      return;
    }
    if (step.login_session && step.organizations?.length) {
      this.view.set({
        kind: 'organizations',
        loginSession: step.login_session,
        organizations: step.organizations,
      });
      return;
    }
    this.notice.set(failureNotice('unavailable'));
  }

  private noticeFor(error: unknown): Notice {
    return failureNotice(error instanceof SignInError ? error.failure : 'unavailable');
  }
}
