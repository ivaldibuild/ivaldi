import { Component, computed, inject, resource, signal } from '@angular/core';
import { NgIcon, provideIcons } from '@ng-icons/core';
import { lucidePlus } from '@ng-icons/lucide';
import { HlmAlertImports } from '@spartan-ng/helm/alert';
import { HlmBadgeImports } from '@spartan-ng/helm/badge';
import { HlmButtonImports } from '@spartan-ng/helm/button';
import { HlmFieldImports } from '@spartan-ng/helm/field';
import { HlmInputImports } from '@spartan-ng/helm/input';
import { HlmSpinnerImports } from '@spartan-ng/helm/spinner';
import { ApiError } from '../../api/api-client';
import { SecretStore, SecretStoreKind } from '../../api/models';
import { PermissionsApi } from '../../api/permissions-api';
import { RequestStatus } from '../../shared/request-status';
import { DeliveryApi } from '../delivery-api';
import { PageHeader } from '../page-header';
import { SecretStoresApi } from '../secret-stores-api';
import { WorkspaceContext } from '../workspace-context';

const kinds: { value: SecretStoreKind; label: string; reads: string }[] = [
  {
    value: 'kubernetes',
    label: 'Kubernetes Secrets',
    reads: 'Secrets in the namespace it deploys to',
  },
  {
    value: 'aws_secrets_manager',
    label: 'AWS Secrets Manager',
    reads: 'secrets its AWS identity is allowed to read',
  },
  { value: 'vault', label: 'HashiCorp Vault', reads: 'a KV v2 mount, signing in with its role' },
  { value: 'doppler', label: 'Doppler', reads: 'one config, with a token kept in its namespace' },
];

const emptyFields = () => ({
  name: '',
  kind: 'kubernetes' as SecretStoreKind,
  region: '',
  roleArn: '',
  address: '',
  mount: '',
  authRole: '',
  tokenSecretName: '',
  tokenSecretKey: 'token',
});

/**
 * The Workspace's Secret Stores: the secret managers its Clusters' agents read secret variables
 * from. A store says where secrets are and which Environments may read them; it never holds a
 * secret or a credential.
 */
@Component({
  selector: 'app-secret-stores-page',
  imports: [
    NgIcon,
    PageHeader,
    HlmAlertImports,
    HlmBadgeImports,
    HlmButtonImports,
    HlmFieldImports,
    HlmInputImports,
    HlmSpinnerImports,
  ],
  providers: [provideIcons({ lucidePlus })],
  templateUrl: './secret-stores-page.html',
})
export class SecretStoresPage {
  private readonly api = inject(SecretStoresApi);
  private readonly delivery = inject(DeliveryApi);
  private readonly permissions = inject(PermissionsApi);
  protected readonly context = inject(WorkspaceContext);

  protected readonly kinds = kinds;

  protected readonly data = resource({
    params: () => this.context.current()?.id,
    loader: async ({ params: workspaceId }) => {
      const [stores, environments, granted] = await Promise.all([
        this.api.list(workspaceId),
        this.delivery.environments(workspaceId),
        this.permissions.granted(workspaceId, ['secret_store.manage']).catch(() => new Set()),
      ]);
      return { stores, environments, canManage: granted.has('secret_store.manage') };
    },
  });

  private readonly loaded = computed(() => (this.data.hasValue() ? this.data.value() : undefined));
  protected readonly environments = computed(() => this.loaded()?.environments ?? []);
  protected readonly canManage = computed(() => this.loaded()?.canManage ?? false);

  protected readonly stores = computed(() =>
    (this.loaded()?.stores ?? []).map((store) => ({
      store,
      kind: kinds.find((k) => k.value === store.kind)?.label ?? store.kind,
      where: this.where(store),
      environments:
        store.environment_ids.length === 0
          ? 'Every Environment'
          : store.environment_ids
              .map((id) => this.environments().find((e) => e.id === id)?.display_name ?? id)
              .join(', '),
    })),
  );

  protected readonly failure = computed(() => {
    const error = this.data.error();
    return error ? (error instanceof ApiError ? error.message : 'Something went wrong.') : null;
  });

  /** Where a store's secrets are, from its settings. */
  private where(store: SecretStore): string {
    const { aws, vault, doppler } = store.settings;
    if (aws) {
      return aws.role_arn ? `${aws.region}, as ${aws.role_arn}` : aws.region;
    }
    if (vault) {
      return `${vault.address}, mount ${vault.mount}, role ${vault.auth_role}`;
    }
    if (doppler) {
      return `Token in Secret ${doppler.token_secret_name}, key ${doppler.token_secret_key}`;
    }
    return "Secrets in each Target's namespace";
  }

  // Adding a store.
  protected readonly adding = signal(false);
  protected readonly fields = signal(emptyFields());
  protected readonly chosen = signal<string[]>([]);
  protected readonly status = new RequestStatus();
  protected readonly invalid = signal<string | null>(null);

  protected readonly kindReads = computed(
    () => kinds.find((k) => k.value === this.fields().kind)?.reads ?? '',
  );

  protected startAdding(): void {
    this.fields.set(emptyFields());
    this.chosen.set([]);
    this.invalid.set(null);
    this.status.error.set(null);
    this.adding.set(true);
  }

  protected set(field: keyof ReturnType<typeof emptyFields>, event: Event): void {
    const value = (event.target as HTMLInputElement | HTMLSelectElement).value;
    this.fields.update((fields) => ({ ...fields, [field]: value }));
  }

  protected toggleEnvironment(id: string, event: Event): void {
    const checked = (event.target as HTMLInputElement).checked;
    this.chosen.update((ids) => (checked ? [...ids, id] : ids.filter((other) => other !== id)));
  }

  /** What is missing before the store can be created, in words, or null. */
  private missing(): string | null {
    const f = this.fields();
    if (!f.name.trim()) {
      return 'Name the store.';
    }
    if (f.kind === 'aws_secrets_manager' && !f.region.trim()) {
      return 'Give the AWS region the secrets are in.';
    }
    if (f.kind === 'vault' && !(f.address.trim() && f.mount.trim() && f.authRole.trim())) {
      return "Give Vault's address, the KV v2 mount, and the Kubernetes auth role.";
    }
    if (f.kind === 'doppler' && !(f.tokenSecretName.trim() && f.tokenSecretKey.trim())) {
      return 'Name the Secret and key that hold the Doppler token.';
    }
    if (f.kind === 'doppler' && this.chosen().length === 0) {
      return 'A Doppler token serves one config: tick the Environments that config is for.';
    }
    return null;
  }

  private settings(): SecretStore['settings'] {
    const f = this.fields();
    switch (f.kind) {
      case 'aws_secrets_manager':
        return { aws: { region: f.region.trim(), role_arn: f.roleArn.trim() || undefined } };
      case 'vault':
        return {
          vault: {
            address: f.address.trim(),
            mount: f.mount.trim(),
            auth_role: f.authRole.trim(),
          },
        };
      case 'doppler':
        return {
          doppler: {
            token_secret_name: f.tokenSecretName.trim(),
            token_secret_key: f.tokenSecretKey.trim(),
          },
        };
      default:
        return {};
    }
  }

  protected async add(event: Event): Promise<void> {
    event.preventDefault();
    const missing = this.missing();
    this.invalid.set(missing);
    const workspaceId = this.context.current()?.id;
    if (missing || !workspaceId) {
      return;
    }
    const created = await this.status.run(() =>
      this.api.create(workspaceId, {
        display_name: this.fields().name.trim(),
        kind: this.fields().kind,
        settings: this.settings(),
        environment_ids: this.chosen(),
      }),
    );
    if (created) {
      this.adding.set(false);
      this.data.reload();
    }
  }

  // Archiving a store, after a confirmation.
  protected readonly confirming = signal<string | null>(null);
  protected readonly archiveStatus = new RequestStatus();

  protected confirm(id: string): void {
    this.archiveStatus.error.set(null);
    this.confirming.set(id);
  }

  protected async archive(store: SecretStore): Promise<void> {
    if (await this.archiveStatus.run(() => this.api.archive(store.id))) {
      this.confirming.set(null);
      this.data.reload();
    }
  }
}
