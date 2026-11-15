import { Component, computed, input } from '@angular/core';
import { NgIcon, provideIcons } from '@ng-icons/core';
import {
  lucideBan,
  lucideCheck,
  lucideCircleAlert,
  lucideClock,
  lucideLoader,
  lucideMinus,
  lucideTriangleAlert,
} from '@ng-icons/lucide';
import { HlmBadgeImports } from '@spartan-ng/helm/badge';
import { DeploymentCheck, DeploymentStatus } from '../../api/models';

export const deploymentStatusLabels: Record<DeploymentStatus, string> = {
  pending: 'Pending',
  awaiting_approval: 'Awaiting approval',
  rejected: 'Rejected',
  cancelled: 'Cancelled',
  applying: 'Applying',
  progressing: 'Progressing',
  healthy: 'Healthy',
  degraded: 'Degraded',
  failed: 'Failed',
  superseded: 'Superseded',
};

/** Statuses a Deployment moves on from by itself, so a page showing one should look again. */
export function inFlight(status: DeploymentStatus): boolean {
  return ['pending', 'awaiting_approval', 'applying', 'progressing'].includes(status);
}

/** Statuses from which a Deployment can still be cancelled: it hasn't started applying. */
export function cancellable(status: DeploymentStatus): boolean {
  return status === 'pending' || status === 'awaiting_approval';
}

const checkLabels: Record<string, string> = {
  release_published: 'The Release is published',
  target_active: 'The Target is active',
  environment_active: 'The Environment is active',
  execution_enabled: 'The Environment runs Deployments',
  same_workspace: 'The Target is in the Application’s Workspace',
  placement: 'The Application is placed on the Target',
  apply_config: 'The placement says how to apply Releases',
  agent_assigned: 'A runtime agent serves the Target’s Cluster',
  agent_capable: 'The Cluster’s agent can deploy in this namespace',
  no_in_flight: 'Nothing else is deploying there',
  environment_in_lifecycle: 'The Environment is in the Channel’s Lifecycle',
  phase_open: 'Its Lifecycle phase is open',
  variables_defined: 'Every variable a value binds has a value here',
  secret_stores_allowed: 'Its secrets are in stores this Environment and its agent can read',
};

/** A check's name in words, or its machine name for one the web app doesn't know yet. */
export function checkLabel(check: DeploymentCheck): string {
  return checkLabels[check.name] ?? check.name.replace(/_/g, ' ');
}

const looks: Record<
  DeploymentStatus,
  { icon: string; variant: 'default' | 'secondary' | 'destructive' | 'outline' }
> = {
  pending: { icon: 'lucideClock', variant: 'secondary' },
  awaiting_approval: { icon: 'lucideClock', variant: 'default' },
  rejected: { icon: 'lucideBan', variant: 'outline' },
  cancelled: { icon: 'lucideMinus', variant: 'outline' },
  applying: { icon: 'lucideLoader', variant: 'secondary' },
  progressing: { icon: 'lucideLoader', variant: 'secondary' },
  healthy: { icon: 'lucideCheck', variant: 'outline' },
  degraded: { icon: 'lucideTriangleAlert', variant: 'destructive' },
  failed: { icon: 'lucideCircleAlert', variant: 'destructive' },
  superseded: { icon: 'lucideMinus', variant: 'outline' },
};

/** A Deployment's status as a badge with an icon, so it doesn't rely on colour alone. */
@Component({
  selector: 'app-deployment-status',
  imports: [NgIcon, HlmBadgeImports],
  providers: [
    provideIcons({
      lucideBan,
      lucideCheck,
      lucideCircleAlert,
      lucideClock,
      lucideLoader,
      lucideMinus,
      lucideTriangleAlert,
    }),
  ],
  template: `
    <span hlmBadge class="gap-1.5 whitespace-nowrap" [variant]="look().variant">
      <ng-icon [name]="look().icon" aria-hidden="true" />{{ label() }}
    </span>
  `,
})
export class DeploymentStatusBadge {
  readonly status = input.required<DeploymentStatus>();
  protected readonly look = computed(() => looks[this.status()]);
  protected readonly label = computed(() => deploymentStatusLabels[this.status()]);
}
