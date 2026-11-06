/** The first-run setup, in order. Each is a child route of /setup. */
export const setupSteps = [
  { path: 'organisation', title: 'Organisation', description: 'Name and default Workspace' },
  { path: 'sign-in', title: 'Sign-in methods', description: 'How people sign in' },
  { path: 'team', title: 'First Team', description: 'People and agents who own work' },
  {
    path: 'environments',
    title: 'Environments & Lifecycle',
    description: 'Where Releases go, in what order',
  },
  { path: 'cluster', title: 'Connect a cluster', description: 'Where your software runs' },
] as const;

export type SetupStepPath = (typeof setupSteps)[number]['path'];

export function stepNumber(path: SetupStepPath): number {
  return setupSteps.findIndex((s) => s.path === path) + 1;
}
