import {
  Component,
  computed,
  DestroyRef,
  effect,
  inject,
  input,
  linkedSignal,
  OnInit,
  output,
  signal,
} from '@angular/core';
import { DatePipe } from '@angular/common';
import { form, FormField, maxLength, pattern, required, submit } from '@angular/forms/signals';
import { Router } from '@angular/router';
import { NgIcon, provideIcons } from '@ng-icons/core';
import {
  lucideCheck,
  lucideCircleCheck,
  lucideCopy,
  lucideInfo,
  lucideServer,
  lucideX,
} from '@ng-icons/lucide';
import { HlmAlertImports } from '@spartan-ng/helm/alert';
import { HlmButtonImports } from '@spartan-ng/helm/button';
import { HlmFieldImports } from '@spartan-ng/helm/field';
import { HlmInputImports } from '@spartan-ng/helm/input';
import { HlmSpinnerImports } from '@spartan-ng/helm/spinner';
import { ApiError } from '../../api/api-client';
import { AgentStatus, Cluster, Environment, IssuedEnrolment } from '../../api/models';
import { RequestStatus } from '../../shared/request-status';
import { ClustersApi } from '../clusters-api';
import { DeliveryApi, slugify } from '../delivery-api';
import { slugPattern } from './new-project-page';
import {
  AgentChoices,
  agentValues,
  enrolmentSecretName,
  namespaceList,
} from './cluster-guide-values';

const emptyCluster = { display_name: '', slug: '' };
const dnsLabel = /^[a-z0-9]+(?:-[a-z0-9]+)*$/;
/** How often step 4 polls the agent's status. */
const pollMs = 3000;
/** How long a step may make no progress before the guide offers help. */
const stallMs = 2 * 60 * 1000;

/**
 * The guided path from "I have a cluster" to "Ivaldi sees it": name the
 * Cluster, choose what the agent may do, install the chart, wait for the
 * first heartbeat, and add Targets. It is the first-run setup's cluster step
 * and the Clusters pages' connect and re-enrol flow.
 */
@Component({
  selector: 'app-cluster-guide',
  imports: [
    DatePipe,
    FormField,
    NgIcon,
    HlmAlertImports,
    HlmButtonImports,
    HlmFieldImports,
    HlmInputImports,
    HlmSpinnerImports,
  ],
  providers: [
    provideIcons({ lucideCheck, lucideCircleCheck, lucideCopy, lucideInfo, lucideServer, lucideX }),
  ],
  templateUrl: './cluster-guide.html',
})
export class ClusterGuide implements OnInit {
  private readonly clusters = inject(ClustersApi);
  private readonly delivery = inject(DeliveryApi);
  private readonly router = inject(Router);
  private readonly destroyRef = inject(DestroyRef);

  readonly mode = input<'setup' | 'manage'>('manage');
  readonly cluster = input<Cluster | null>(null);
  readonly environments = input<Environment[]>([]);
  readonly startAt = input<number>(3);
  readonly done = output<void>();

  protected readonly step = linkedSignal(() => this.startAt());

  /** Moves to a step. Leaving step 4 stops its polling. */
  protected goTo(step: number): void {
    if (step !== 4) {
      this.stopPolling();
    }
    this.step.set(step);
  }
  protected readonly current = linkedSignal<Cluster | null>(() => this.cluster());

  // Step 1: name the cluster.
  private readonly model = signal({ ...emptyCluster });
  protected readonly clusterForm = form(this.model, (path) => {
    required(path.display_name, { message: 'Name the cluster, such as Prod EU.' });
    maxLength(path.display_name, 255, { message: 'Use at most 255 characters.' });
    required(path.slug, { message: 'Give the cluster an address.' });
    pattern(path.slug, slugPattern, {
      message: 'Use lower-case letters and digits, with single hyphens between words.',
    });
    maxLength(path.slug, 63, { message: 'Use at most 63 characters.' });
  });
  protected readonly creating = new RequestStatus();
  private slugTyped = false;

  protected nameChanged(): void {
    if (!this.slugTyped) {
      this.model.update((m) => ({ ...m, slug: slugify(m.display_name) }));
    }
  }

  protected slugChanged(): void {
    this.slugTyped = true;
  }

  protected async createCluster(event: Event): Promise<void> {
    event.preventDefault();
    await submit(this.clusterForm, async () => {
      const model = this.model();
      let created: Cluster | null = null;
      const ok = await this.creating.run(async () => {
        created = await this.clusters.createCluster({
          display_name: model.display_name.trim(),
          slug: model.slug,
        });
      });
      if (ok && created) {
        this.current.set(created);
        this.goTo(2);
      }
      return undefined;
    });
  }

  // Step 2: what the agent may do. These only shape the chart values; the
  // cluster's RBAC is the control.
  protected readonly choices = signal<AgentChoices>({
    observeAll: true,
    observeNamespaces: '',
    executeNamespaces: '',
    allowRBAC: false,
    agentNamespace: 'ivaldi-system',
    authentication: 'bearer',
  });
  protected readonly observeAll = computed(() => this.choices().observeAll);
  protected readonly agentNamespace = computed(() => this.choices().agentNamespace);
  protected setObserveAll(value: boolean): void {
    this.choices.update((c) => ({ ...c, observeAll: value }));
  }
  protected setObserveNamespaces(value: string): void {
    this.choices.update((c) => ({ ...c, observeNamespaces: value }));
  }
  protected setExecuteNamespaces(value: string): void {
    this.choices.update((c) => ({ ...c, executeNamespaces: value }));
  }
  protected setAllowRBAC(value: boolean): void {
    this.choices.update((c) => ({ ...c, allowRBAC: value }));
  }
  protected setAgentNamespace(value: string): void {
    this.choices.update((c) => ({ ...c, agentNamespace: value }));
  }
  protected setAuthentication(value: 'bearer' | 'kubernetes'): void {
    this.choices.update((c) => ({ ...c, authentication: value }));
  }

  /**
   * What is wrong with the step 2 choices, if anything. Namespaces go into
   * values.yaml as they are, so anything but a Kubernetes namespace name
   * would corrupt the file.
   */
  protected readonly choicesError = computed(() => {
    const c = this.choices();
    if (!dnsLabel.test(c.agentNamespace)) {
      return 'The agent namespace must be a Kubernetes namespace: lower-case letters, digits and hyphens.';
    }
    const named = [
      ...(c.observeAll ? [] : namespaceList(c.observeNamespaces)),
      ...namespaceList(c.executeNamespaces),
    ];
    const bad = named.filter((ns) => !dnsLabel.test(ns));
    if (bad.length > 0) {
      return `${bad.join(', ')} ${bad.length === 1 ? 'is not a' : 'are not'} Kubernetes namespace${bad.length === 1 ? '' : 's'}: use lower-case letters, digits and hyphens.`;
    }
    return null;
  });

  // Step 3: mint a token and show how to install the chart.
  /** The latest enrolment, token included. It is cleared on leaving step 3. */
  protected readonly enrolment = signal<IssuedEnrolment | null>(null);
  protected readonly minting = new RequestStatus();
  protected readonly copied = signal<string | null>(null);
  protected readonly copyError = signal<string | null>(null);
  /**
   * The Cluster's agent when the token was minted, so step 4 waits for a new
   * one: re-enrolling a Cluster must not tick off the old agent's reports.
   */
  private readonly previousAgent = signal<string | null>(null);
  /** When the token was minted and when it expires, kept after it is cleared. */
  private readonly mintedAt = signal(0);
  private readonly expiresAt = signal(0);

  protected async mint(): Promise<void> {
    const cluster = this.current();
    if (!cluster) {
      return;
    }
    await this.minting.run(async () => {
      const issued = await this.clusters.createEnrolment(cluster.id, this.choices().authentication);
      this.previousAgent.set(
        this.status()?.agent_principal_id ?? cluster.agent_principal_id ?? null,
      );
      this.mintedAt.set(Date.now());
      this.expiresAt.set(Date.parse(issued.expires_at));
      this.enrolment.set(issued);
    });
  }

  protected async install(): Promise<void> {
    this.enrolment.set(null);
    this.copied.set(null);
    this.goTo(3);
    await this.mint();
    if (!this.enrolment()) {
      this.goTo(2);
    }
  }

  protected valuesYaml(): string {
    const install = this.enrolment()?.install;
    return install ? agentValues(install.server_url, this.choices()) : '';
  }

  protected namespaceCommand(): string {
    return `kubectl create namespace ${this.agentNamespace()} --dry-run=client -o yaml | kubectl apply -f -`;
  }

  /**
   * Puts the token in a Secret without writing it into the shell's history:
   * `read -s` takes it from a prompt, and the variable is unset afterwards.
   */
  protected secretCommand(): string {
    return [
      'read -rs IVALDI_ENROLMENT_TOKEN  # paste the token, then press Enter',
      `kubectl -n ${this.agentNamespace()} create secret generic ${enrolmentSecretName} --from-literal=token="$IVALDI_ENROLMENT_TOKEN" --dry-run=client -o yaml | kubectl apply -f -`,
      'unset IVALDI_ENROLMENT_TOKEN',
    ].join('\n');
  }

  /** Leaves out --version when this control plane is not a release build. */
  protected helmCommand(): string {
    const install = this.enrolment()?.install;
    const version = install?.chart_version ? ` --version ${install.chart_version}` : '';
    return `helm upgrade --install ivaldi-agent ${install?.chart ?? '<chart>'}${version} --namespace ${this.agentNamespace()} --create-namespace -f values.yaml`;
  }

  protected async copy(text: string, key: string): Promise<void> {
    this.copyError.set(null);
    try {
      if (!navigator.clipboard) {
        throw new Error('no clipboard');
      }
      await navigator.clipboard.writeText(text);
      this.copied.set(key);
    } catch {
      this.copied.set(null);
      this.copyError.set('Copying was blocked. Select the text and copy it yourself.');
    }
  }

  // Step 4: wait for the agent.
  protected readonly status = signal<AgentStatus | null>(null);
  private timer: ReturnType<typeof setInterval> | undefined;
  /** The clock step 4 reads, advanced by each poll. */
  private readonly now = signal(Date.now());
  /** When step 4 last saw a check pass, for noticing a stall. */
  private readonly progressAt = signal(0);
  private passing = 0;

  protected startWaiting(): void {
    // The token is shown only on step 3: leaving it forgets the token.
    this.enrolment.set(null);
    this.copied.set(null);
    this.goTo(4);
    this.passing = 0;
    this.now.set(Date.now());
    this.progressAt.set(Date.now());
    this.stopPolling();
    void this.refresh();
    this.timer = setInterval(() => void this.refresh(), pollMs);
  }

  private async refresh(): Promise<void> {
    const cluster = this.current();
    if (!cluster) {
      return;
    }
    try {
      this.status.set(await this.clusters.agentStatus(cluster.id));
    } catch {
      // A failed poll is transient; the next one tries again.
    }
    this.now.set(Date.now());
    const passing = this.checks().filter((check) => check.ok).length;
    if (passing !== this.passing) {
      this.passing = passing;
      this.progressAt.set(this.now());
    }
    if (passing === this.checks().length) {
      this.stopPolling();
    }
  }

  private stopPolling(): void {
    if (this.timer) {
      clearInterval(this.timer);
      this.timer = undefined;
    }
  }

  /**
   * The status of the agent enrolled with this guide's token, or null while
   * the Cluster still has the agent it had before: re-enrolment waits for a
   * new principal, which every redeem creates.
   */
  private readonly fresh = computed(() => {
    const s = this.status();
    return s?.agent_principal_id && s.agent_principal_id !== this.previousAgent() ? s : null;
  });

  protected readonly checks = computed(() => {
    const s = this.fresh();
    return [
      { label: 'Enrolment token redeemed', ok: !!s?.redeemed_at },
      { label: 'First heartbeat received', ok: !!s?.last_seen_at },
      {
        label: s?.kubernetes_version
          ? `Kubernetes version reported: ${s.kubernetes_version}`
          : 'Kubernetes version reported',
        ok: !!s?.kubernetes_version,
      },
      {
        label: s?.agent_version
          ? `Agent version reported: ${s.agent_version}`
          : 'Agent version reported',
        ok: !!s?.agent_version,
      },
    ];
  });

  protected readonly connected = computed(() => !!this.fresh()?.last_seen_at);

  /** A refused redeem since this guide's token was minted. */
  protected readonly refusal = computed(() => {
    const failure = this.status()?.last_failure;
    return failure && Date.parse(failure.at) >= this.mintedAt() - 1000 ? failure : null;
  });

  /** The token expired before an agent redeemed it. */
  protected readonly tokenExpired = computed(
    () => !this.fresh()?.redeemed_at && this.expiresAt() > 0 && this.now() > this.expiresAt(),
  );

  /**
   * What to check when the guide has seen no progress for two minutes, keyed
   * to the first step that has not happened.
   */
  protected readonly troubleshooting = computed<string[]>(() => {
    if (this.connected() || this.tokenExpired() || this.now() - this.progressAt() < stallMs) {
      return [];
    }
    const ns = this.agentNamespace();
    if (!this.fresh()?.redeemed_at) {
      const tips = [
        `Is the agent's pod running? kubectl -n ${ns} get pods,events`,
        'Can the cluster reach the control plane? Check egress rules, any proxy, and that the cluster trusts its TLS certificate.',
        'Was the token already used, or is the Secret missing? Mint a new token and create the Secret again.',
      ];
      if (this.choices().authentication === 'kubernetes') {
        tips.push(
          "Does this control plane trust the cluster's OIDC issuer? It must be listed in runners.kubernetes_issuers.",
        );
      }
      return tips;
    }
    return [
      `The agent enrolled but has not reported. Check its logs: kubectl -n ${ns} logs deploy/ivaldi-agent`,
      'Can the cluster still reach the control plane? Check egress rules and any proxy.',
    ];
  });

  // Step 5: add Targets.
  private readonly targetModel = signal({ environment_id: '', display_name: '', namespace: '' });
  protected readonly targetForm = form(this.targetModel, (path) => {
    required(path.environment_id, { message: 'Choose an Environment.' });
    required(path.display_name, { message: 'Name the Target.' });
    required(path.namespace, { message: 'Give the namespace.' });
    pattern(path.namespace, dnsLabel, {
      message: 'Use a Kubernetes namespace: lower-case letters, digits and hyphens.',
    });
  });
  protected readonly addingTarget = new RequestStatus();
  protected readonly addedTarget = signal('');

  /** Opening on the install step, as re-enrolling does, mints its token. */
  ngOnInit(): void {
    if (this.step() === 3 && !this.enrolment()) {
      void this.install();
    }
  }

  constructor() {
    this.destroyRef.onDestroy(() => this.stopPolling());
    effect(() => {
      const environments = this.environments();
      if (environments.length > 0 && !this.targetModel().environment_id) {
        this.targetModel.update((m) => ({ ...m, environment_id: environments[0].id }));
      }
    });
  }

  protected async addTarget(event: Event): Promise<void> {
    event.preventDefault();
    await submit(this.targetForm, async () => {
      const cluster = this.current();
      const model = this.targetModel();
      if (!cluster) {
        return undefined;
      }
      await this.addingTarget.run(async () => {
        const target = await this.delivery.createTarget(model.environment_id, {
          display_name: model.display_name.trim(),
          cluster: cluster.slug,
          namespace: model.namespace.trim(),
        });
        this.addedTarget.set(target.display_name);
      });
      return undefined;
    });
  }

  protected finish(): void {
    this.stopPolling();
    if (this.mode() === 'setup') {
      void this.router.navigate(['/setup/done']);
      return;
    }
    this.done.emit();
  }

  protected errorOf(error: unknown): string | null {
    return error ? (error instanceof ApiError ? error.message : 'Something went wrong.') : null;
  }

  protected readonly mintError = computed(() => this.errorOf(this.minting.error()));
  protected readonly createError = computed(() => this.errorOf(this.creating.error()));
  protected readonly targetError = computed(() => this.errorOf(this.addingTarget.error()));
}
