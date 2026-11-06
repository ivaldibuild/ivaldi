import { HttpClient, HttpErrorResponse } from '@angular/common/http';
import { inject, Service } from '@angular/core';
import { firstValueFrom, Observable } from 'rxjs';
import { API_BASE_URL } from '../api-base-url';

/** A provider the person can sign in with, from `GET /v1/auth/login`. */
export interface SignInProvider {
  kind: string;
  name: string;
  start_url: string;
}

/** What the control plane says about a sign-in it has started. */
export interface LoginRequest {
  client_id: string;
  client_name: string;
  password: boolean;
  sign_up: boolean;
  providers: SignInProvider[];
}

export interface OrganizationOption {
  id: string;
  slug: string;
  name: string;
}

/** The answer to each sign-in step: go on to `redirect_to`, or choose an organization. */
export interface LoginStep {
  redirect_to?: string;
  login_session?: string;
  organizations?: OrganizationOption[];
}

/**
 * Why a step was refused, from the status of its `application/problem+json` answer
 * (see docs/design/sign-in-flow.md).
 */
export type SignInFailure =
  | 'expired' // 400: the challenge, session or token expired or doesn't match
  | 'credentials' // 401: wrong email or password, never saying which
  | 'forbidden' // 403: the account, provider or organization refuses the sign-in
  | 'conflict' // 409
  | 'invalid' // 422
  | 'unavailable'; // network failure or a server error

export class SignInError extends Error {
  readonly failure: SignInFailure;

  constructor(failure: SignInFailure) {
    super(`sign-in failed: ${failure}`);
    this.failure = failure;
  }
}

/** The control plane's `/v1/auth` operations that the sign-in screens call. */
@Service()
export class SignInApi {
  private readonly http = inject(HttpClient);
  private readonly base = `${inject(API_BASE_URL)}/v1/auth`;

  loginRequest(loginChallenge: string): Promise<LoginRequest> {
    return call(
      this.http.get<LoginRequest>(`${this.base}/login`, {
        params: { login_challenge: loginChallenge },
      }),
    );
  }

  signInWithPassword(loginChallenge: string, email: string, password: string): Promise<LoginStep> {
    return call(
      this.http.post<LoginStep>(`${this.base}/login/password`, {
        login_challenge: loginChallenge,
        email,
        password,
      }),
    );
  }

  chooseOrganization(
    loginChallenge: string,
    loginSession: string,
    organizationId: string,
  ): Promise<LoginStep> {
    return call(
      this.http.post<LoginStep>(`${this.base}/login/organization`, {
        login_challenge: loginChallenge,
        login_session: loginSession,
        organization_id: organizationId,
      }),
    );
  }
}

async function call<T>(request: Observable<T>): Promise<T> {
  try {
    return await firstValueFrom(request);
  } catch (error) {
    throw new SignInError(failureOf(error));
  }
}

function failureOf(error: unknown): SignInFailure {
  if (!(error instanceof HttpErrorResponse)) {
    return 'unavailable';
  }
  switch (error.status) {
    case 400:
      return 'expired';
    case 401:
      return 'credentials';
    case 403:
      return 'forbidden';
    case 409:
      return 'conflict';
    case 422:
      return 'invalid';
    default:
      return 'unavailable';
  }
}
