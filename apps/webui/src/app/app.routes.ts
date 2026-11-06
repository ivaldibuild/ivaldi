import { Routes } from '@angular/router';
import { requireSignIn } from './auth/auth';

export const routes: Routes = [
  {
    path: 'login',
    title: 'Sign in · Ivaldi',
    loadComponent: () => import('./auth/sign-in').then((m) => m.SignIn),
  },
  {
    path: 'auth/callback',
    title: 'Signing in · Ivaldi',
    loadComponent: () => import('./auth/auth-callback').then((m) => m.AuthCallback),
  },
  {
    path: 'setup',
    canActivate: [requireSignIn],
    loadComponent: () => import('./onboarding/onboarding-shell').then((m) => m.OnboardingShell),
    children: [
      { path: '', pathMatch: 'full', redirectTo: 'organisation' },
      {
        path: 'organisation',
        title: 'Organisation · Setup · Ivaldi',
        loadComponent: () =>
          import('./onboarding/steps/organisation-step').then((m) => m.OrganisationStep),
      },
      {
        path: 'sign-in',
        title: 'Sign-in methods · Setup · Ivaldi',
        loadComponent: () => import('./onboarding/steps/sign-in-step').then((m) => m.SignInStep),
      },
      {
        path: 'team',
        title: 'First Team · Setup · Ivaldi',
        loadComponent: () => import('./onboarding/steps/team-step').then((m) => m.TeamStep),
      },
      {
        path: 'environments',
        title: 'Environments & Lifecycle · Setup · Ivaldi',
        loadComponent: () =>
          import('./onboarding/steps/environments-step').then((m) => m.EnvironmentsStep),
      },
      {
        path: 'cluster',
        title: 'Connect a cluster · Setup · Ivaldi',
        loadComponent: () => import('./onboarding/steps/cluster-step').then((m) => m.ClusterStep),
      },
      {
        path: 'done',
        title: 'Setup done · Ivaldi',
        loadComponent: () => import('./onboarding/setup-done').then((m) => m.SetupDone),
      },
    ],
  },
  { path: '', pathMatch: 'full', redirectTo: 'setup' },
];
