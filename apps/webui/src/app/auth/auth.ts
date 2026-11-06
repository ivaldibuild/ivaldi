import { HttpInterceptorFn } from '@angular/common/http';
import { inject } from '@angular/core';
import { CanActivateFn } from '@angular/router';
import { from, switchMap } from 'rxjs';
import { API_BASE_URL } from '../api-base-url';
import { Session } from './session';

/** Lets a route open only when signed in, starting a sign-in otherwise. */
export const requireSignIn: CanActivateFn = async (_route, state) => {
  const session = inject(Session);
  if (await session.accessToken()) {
    return true;
  }
  await session.signIn(state.url);
  return false;
};

/** Adds the access token to control-plane API calls, except the public sign-in ones. */
export const authInterceptor: HttpInterceptorFn = (request, next) => {
  const base = inject(API_BASE_URL);
  if (!request.url.startsWith(`${base}/v1/`) || request.url.startsWith(`${base}/v1/auth/`)) {
    return next(request);
  }
  return from(inject(Session).accessToken()).pipe(
    switchMap((token) =>
      next(token ? request.clone({ setHeaders: { Authorization: `Bearer ${token}` } }) : request),
    ),
  );
};
