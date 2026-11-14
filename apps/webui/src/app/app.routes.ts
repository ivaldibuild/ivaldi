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
      { path: '', pathMatch: 'full', redirectTo: 'projects' },
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
        path: 'applications/:id/releases/new',
        title: 'Create Release · Ivaldi',
        loadComponent: () =>
          import('./delivery/releases/new-release-page').then((m) => m.NewReleasePage),
      },
      {
        path: 'releases/:id/deploy',
        title: 'Deploy · Ivaldi',
        loadComponent: () => import('./delivery/deployments/deploy-page').then((m) => m.DeployPage),
      },
      {
        path: 'deployments/:id',
        title: 'Deployment · Ivaldi',
        loadComponent: () =>
          import('./delivery/deployments/deployment-page').then((m) => m.DeploymentPage),
      },
      {
        path: 'releases/:id',
        title: 'Release · Ivaldi',
        loadComponent: () => import('./delivery/releases/release-page').then((m) => m.ReleasePage),
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
        path: 'projects/:id/edit',
        title: 'Edit project · Ivaldi',
        loadComponent: () =>
          import('./delivery/pages/project-edit-page').then((m) => m.ProjectEditPage),
      },
      {
        path: 'projects/:id',
        title: 'Project · Ivaldi',
        loadComponent: () => import('./delivery/pages/project-page').then((m) => m.ProjectPage),
      },
      {
        path: 'clusters',
        title: 'Clusters · Ivaldi',
        loadComponent: () =>
          import('./delivery/pages/clusters-page').then((m) => m.ClustersPage),
      },
      {
        path: 'clusters/:id',
        title: 'Cluster · Ivaldi',
        loadComponent: () => import('./delivery/pages/cluster-page').then((m) => m.ClusterPage),
      },
      {
        path: 'environments',
        title: 'Environments · Ivaldi',
        loadComponent: () =>
          import('./delivery/pages/environments-page').then((m) => m.EnvironmentsPage),
      },
      {
        path: 'environments/:id',
        title: 'Environment · Ivaldi',
        loadComponent: () =>
          import('./delivery/pages/environment-page').then((m) => m.EnvironmentPage),
      },
      {
        path: 'settings',
        loadComponent: () => import('./settings/settings-shell').then((m) => m.SettingsShell),
        children: [
          { path: '', pathMatch: 'full', redirectTo: 'general' },
          {
            path: 'general',
            title: 'General · Organisation settings · Ivaldi',
            loadComponent: () => import('./settings/general-page').then((m) => m.GeneralPage),
          },
          {
            path: 'people',
            title: 'People & access · Organisation settings · Ivaldi',
            loadComponent: () => import('./organisation/people-page').then((m) => m.PeoplePage),
          },
          {
            path: 'service-accounts',
            title: 'Service accounts · Organisation settings · Ivaldi',
            loadComponent: () =>
              import('./organisation/service-accounts-page').then((m) => m.ServiceAccountsPage),
          },
          {
            path: 'service-accounts/:id',
            title: 'Service account · Organisation settings · Ivaldi',
            loadComponent: () =>
              import('./organisation/service-account-page').then((m) => m.ServiceAccountPage),
          },
          {
            path: 'agents',
            title: 'Agents · Organisation settings · Ivaldi',
            loadComponent: () => import('./settings/agents-page').then((m) => m.AgentsPage),
          },
          {
            path: 'agents/:id',
            title: 'Agent · Organisation settings · Ivaldi',
            loadComponent: () => import('./settings/agent-page').then((m) => m.AgentPage),
          },
          {
            path: 'workspaces',
            title: 'Workspaces · Organisation settings · Ivaldi',
            loadComponent: () => import('./settings/workspaces-page').then((m) => m.WorkspacesPage),
          },
          {
            path: 'teams',
            title: 'Teams · Organisation settings · Ivaldi',
            loadComponent: () => import('./settings/teams-page').then((m) => m.TeamsPage),
          },
          {
            path: 'connections',
            title: 'Connections · Organisation settings · Ivaldi',
            loadComponent: () =>
              import('./settings/connections-page').then((m) => m.ConnectionsPage),
          },
          {
            // The GitHub App's callback URL: GitHub sends people here after an install.
            path: 'connections/github/callback',
            title: 'Connecting GitHub · Organisation settings · Ivaldi',
            loadComponent: () =>
              import('./settings/github-install-callback').then((m) => m.GitHubInstallCallback),
          },
          {
            path: 'teams/:id',
            title: 'Team · Organisation settings · Ivaldi',
            loadComponent: () => import('./settings/team-page').then((m) => m.TeamPage),
          },
        ],
      },
      // Where these pages used to be.
      { path: 'people', redirectTo: 'settings/people' },
      { path: 'service-accounts', redirectTo: 'settings/service-accounts' },
      { path: 'service-accounts/:id', redirectTo: 'settings/service-accounts/:id' },
      {
        path: 'lifecycles',
        title: 'Lifecycles · Ivaldi',
        loadComponent: () =>
          import('./delivery/pages/lifecycles-page').then((m) => m.LifecyclesPage),
      },
    ],
  },
];
