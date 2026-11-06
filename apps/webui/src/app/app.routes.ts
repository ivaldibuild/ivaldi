import { Routes } from '@angular/router';

export const routes: Routes = [
  {
    path: 'login',
    title: 'Sign in · Ivaldi',
    loadComponent: () => import('./auth/sign-in').then((m) => m.SignIn),
  },
  { path: '', pathMatch: 'full', redirectTo: 'login' },
];
