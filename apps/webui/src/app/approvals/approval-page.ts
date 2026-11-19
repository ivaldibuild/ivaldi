import { Component, computed, inject, input, resource, signal } from '@angular/core';
import { form, FormField, maxLength } from '@angular/forms/signals';
import { RouterLink } from '@angular/router';
import { HlmAlertImports } from '@spartan-ng/helm/alert';
import { HlmBadgeImports } from '@spartan-ng/helm/badge';
import { HlmButtonImports } from '@spartan-ng/helm/button';
import { HlmFieldImports } from '@spartan-ng/helm/field';
import { HlmSpinnerImports } from '@spartan-ng/helm/spinner';
import { ApiError } from '../api/api-client';
import { ApprovalsApi } from '../api/approvals-api';
import {
  ApprovalRequest,
  ApprovalState,
  DeploymentApprovalSummary,
  NamedPrincipal,
} from '../api/models';
import { NotificationsApi } from '../api/notifications-api';
import { DeliveryApi } from '../delivery/delivery-api';
import { PageHeader } from '../delivery/page-header';
import { formatDayAndTime } from '../shared/dates';
import { RequestStatus } from '../shared/request-status';

const stateLabels: Record<ApprovalState, string> = {
  pending: 'Waiting for a decision',
  approved: 'Approved',
  rejected: 'Rejected',
  cancelled: 'Cancelled',
};

const reasonLabels: Record<DeploymentApprovalSummary['reason'], string> = {
  rollout: 'Rollout',
  rollback: 'Rollback',
  redeploy: 'Redeploy',
};

const tierLabels: Record<DeploymentApprovalSummary['environment_tier'], string> = {
  development: 'development',
  staging: 'staging',
  production: 'production',
  other: 'other',
};

/** The longest note the control plane takes. */
const maxNote = 2000;

/** One fact about what waits: a label, its value, and where it links. */
interface Fact {
  label: string;
  value: string;
  link?: unknown[];
}

/**
 * One approval request: what waits for it, who asked, and, for someone who could decide it, the
 * decision. Notifications link here.
 */
@Component({
  selector: 'app-approval-page',
  imports: [
    FormField,
    RouterLink,
    PageHeader,
    HlmAlertImports,
    HlmBadgeImports,
    HlmButtonImports,
    HlmFieldImports,
    HlmSpinnerImports,
  ],
  templateUrl: './approval-page.html',
})
export class ApprovalPage {
  private readonly api = inject(ApprovalsApi);
  private readonly delivery = inject(DeliveryApi);
  private readonly notifications = inject(NotificationsApi);

  /** The `:id` route parameter. */
  readonly id = input.required<string>();

  protected readonly details = resource({
    params: () => this.id(),
    loader: async ({ params: id }) => {
      const request = await this.api.approval(id);
      const [principals, facts] = await Promise.all([
        this.api
          .principals(
            [
              request.requested_by_principal_id,
              request.on_behalf_of_principal_id,
              request.closed_by_principal_id,
            ].filter((p): p is string => !!p),
          )
          .catch(() => [] as NamedPrincipal[]),
        this.describe(request),
      ]);
      return { request, principals, facts };
    },
  });

  private readonly loaded = computed(() =>
    this.details.hasValue() ? this.details.value() : undefined,
  );
  protected readonly request = computed(() => this.loaded()?.request ?? null);
  protected readonly facts = computed(() => this.loaded()?.facts ?? []);
  protected readonly stateLabel = computed(() => {
    const r = this.request();
    return r ? stateLabels[r.state] : '';
  });
  /** The page of what waits, when Ivaldi has one. */
  protected readonly subjectLink = computed(() => {
    const r = this.request();
    return r?.subject_type === 'delivery.deployment' ? ['/deployments', r.subject_id] : null;
  });
  protected readonly pending = computed(() => this.request()?.state === 'pending');
  protected readonly canDecide = computed(() => this.pending() && !!this.request()?.can_decide);
  protected readonly requestedAt = computed(() => {
    const r = this.request();
    return r ? formatDayAndTime(r.created_at) : '';
  });

  /** Who asked, in words: a person, or an agent Session and the person it acts for. */
  protected readonly askedBy = computed(() => {
    const r = this.request();
    if (!r) {
      return '';
    }
    const asker = this.name(r.requested_by_principal_id);
    return r.on_behalf_of_principal_id
      ? `${asker}, acting for ${this.name(r.on_behalf_of_principal_id)}`
      : asker;
  });

  /** How it was closed, in words, once it is. */
  protected readonly outcome = computed(() => {
    const r = this.request();
    if (!r || r.state === 'pending') {
      return null;
    }
    const by = r.closed_by_principal_id ? ` by ${this.name(r.closed_by_principal_id)}` : '';
    const at = r.closed_at ? ` on ${formatDayAndTime(r.closed_at)}` : '';
    return { heading: `${stateLabels[r.state]}${by}${at}`, note: r.note };
  });

  protected readonly failure = computed(() => {
    const error = this.details.error();
    return error ? (error instanceof ApiError ? error.message : 'Something went wrong.') : null;
  });

  private name(id: string): string {
    const principal = this.loaded()?.principals.find((p) => p.id === id);
    if (!principal) {
      return id;
    }
    return principal.kind === 'agent_session' ? 'An agent Session' : principal.display_name;
  }

  /**
   * What waits, as facts with links. A Deployment's summary names Ivaldi objects by ID, so their
   * names are read; one that can't be read shows its ID.
   */
  private async describe(request: ApprovalRequest): Promise<Fact[]> {
    if (request.subject_type !== 'delivery.deployment') {
      return [{ label: 'Waiting', value: `${request.subject_type} ${request.subject_id}` }];
    }
    const s = request.summary as DeploymentApprovalSummary;
    const [application, environment, targets] = await Promise.all([
      this.delivery.application(s.application_id).catch(() => null),
      this.delivery.environment(s.environment_id).catch(() => null),
      this.delivery.targets(s.environment_id, true).catch(() => []),
    ]);
    const target = targets.find((t) => t.id === s.target_id);
    const facts: Fact[] = [
      {
        label: 'Application',
        value: application?.display_name ?? s.application_id,
        link: ['/applications', s.application_id],
      },
      { label: 'Release', value: s.release_version, link: ['/releases', s.release_id] },
      {
        label: 'Environment',
        value: `${environment?.display_name ?? s.environment_id} (${tierLabels[s.environment_tier]})`,
        link: ['/environments', s.environment_id],
      },
      { label: 'Target', value: target?.display_name ?? s.target_id },
      { label: 'Kind', value: reasonLabels[s.reason] },
    ];
    if (s.restores_deployment_id) {
      facts.push({
        label: 'Restores',
        value: 'The Deployment it rolls back to',
        link: ['/deployments', s.restores_deployment_id],
      });
    }
    if (s.secrets_latest) {
      facts.push({
        label: 'Secrets',
        value: 'The latest versions, not the ones last deployed',
      });
    }
    return facts;
  }

  // Deciding.

  private readonly model = signal({ note: '' });
  protected readonly decisionForm = form(this.model, (path) => {
    maxLength(path.note, maxNote, { message: `Keep the note to ${maxNote} characters.` });
  });
  protected readonly decideStatus = new RequestStatus();
  /** Shown when rejecting without saying why. */
  protected readonly noteMissing = signal(false);

  protected approve(): Promise<void> {
    return this.decide((id, note) => this.api.approve(id, note));
  }

  protected reject(): Promise<void> {
    if (!this.model().note.trim()) {
      this.noteMissing.set(true);
      return Promise.resolve();
    }
    return this.decide((id, note) => this.api.reject(id, note));
  }

  private async decide(op: (id: string, note: string) => Promise<ApprovalRequest>): Promise<void> {
    this.noteMissing.set(false);
    if (this.decisionForm().invalid()) {
      return;
    }
    const note = this.model().note.trim();
    if (await this.decideStatus.run(() => op(this.id(), note))) {
      this.model.set({ note: '' });
      this.details.reload();
      void this.notifications.refreshCount().catch(() => undefined);
    }
  }
}
