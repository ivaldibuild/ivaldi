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
  {
    path: '',
    canActivate: [requireSignIn],
    loadComponent: () => import('./delivery/app-shell').then((m) => m.AppShell),
    children: [
      { path: '', pathMatch: 'full', redirectTo: 'applications' },
      {
        path: 'applications',
        title: 'Applications · Ivaldi',
        loadComponent: () =>
          import('./delivery/pages/applications-page').then((m) => m.ApplicationsPage),
      },
      {
        path: 'applications/new',
        title: 'New application · Ivaldi',
        loadComponent: () =>
          import('./delivery/pages/new-application-page').then((m) => m.NewApplicationPage),
      },
      {
        path: 'applications/:id',
        title: 'Application · Ivaldi',
        loadComponent: () =>
          import('./delivery/pages/application-page').then((m) => m.ApplicationPage),
      },
      {
        path: 'projects',
        title: 'Projects · Ivaldi',
        loadComponent: () => import('./delivery/pages/projects-page').then((m) => m.ProjectsPage),
      },
      {
        path: 'projects/new',
        title: 'New project · Ivaldi',
        loadComponent: () =>
          import('./delivery/pages/new-project-page').then((m) => m.NewProjectPage),
      },
      {
        path: 'projects/:id',
        title: 'Project · Ivaldi',
        loadComponent: () => import('./delivery/pages/project-page').then((m) => m.ProjectPage),
      },
      {
        path: 'environments',
        title: 'Environments · Ivaldi',
        loadComponent: () =>
          import('./delivery/pages/environments-page').then((m) => m.EnvironmentsPage),
      },
      {
        path: 'lifecycles',
        title: 'Lifecycles · Ivaldi',
        loadComponent: () =>
          import('./delivery/pages/lifecycles-page').then((m) => m.LifecyclesPage),
      },
    ],
  },
];
