import { Component, computed, DOCUMENT, inject, resource, signal } from '@angular/core';
import { NgIcon, provideIcons } from '@ng-icons/core';
import { lucidePlus } from '@ng-icons/lucide';
import { HlmAlertImports } from '@spartan-ng/helm/alert';
import { HlmBadgeImports } from '@spartan-ng/helm/badge';
import { HlmButtonImports } from '@spartan-ng/helm/button';
import { HlmSpinnerImports } from '@spartan-ng/helm/spinner';
import { ApiError } from '../../api/api-client';
import { ApprovalsApi } from '../../api/approvals-api';
import { SecretStore } from '../../api/models';
import { PermissionsApi } from '../../api/permissions-api';
import { RequestStatus } from '../../shared/request-status';
import { DeliveryApi } from '../delivery-api';
import { PageHeader } from '../page-header';
import { WorkspaceContext } from '../workspace-context';
import { SecretStoresApi } from './secret-stores-api';
import {
  credentialFields,
  federationAudience,
  federationSubject,
  kinds,
  storeLocation,
  storeOpening,
} from './store-access';
import { StoreCredential } from './store-credential';
import { SecretStoreForm } from './store-form';

/**
 * The Workspace's Secret Stores: the secret managers its Clusters' agents read secret variables
 * from. A store says where secrets are, how it is opened, and which Environments may read them.
 * It never holds a secret; a brokered store's minting credential is kept encrypted and never
 * shown again.
 */
@Component({
  selector: 'app-secret-stores-page',
  imports: [
    NgIcon,
    PageHeader,
    SecretStoreForm,
    StoreCredential,
    HlmAlertImports,
    HlmBadgeImports,
    HlmButtonImports,
    HlmSpinnerImports,
  ],
  providers: [provideIcons({ lucidePlus })],
  templateUrl: './secret-stores-page.html',
})
export class SecretStoresPage {
  private readonly api = inject(SecretStoresApi);
  private readonly delivery = inject(DeliveryApi);
  private readonly permissions = inject(PermissionsApi);
  private readonly principals = inject(ApprovalsApi);
  private readonly document = inject(DOCUMENT);
  protected readonly context = inject(WorkspaceContext);

  protected readonly data = resource({
    params: () => this.context.current()?.id,
    loader: async ({ params: workspaceId }) => {
      const [stores, environments, granted] = await Promise.all([
        this.api.list(workspaceId),
        this.delivery.environments(workspaceId),
        this.permissions.granted(workspaceId, ['secret_store.manage']).catch(() => new Set()),
      ]);
      const setters = await this.principals
        .principals(stores.flatMap((s) => (s.credential_set_by ? [s.credential_set_by] : [])))
        .catch(() => []);
      return { stores, environments, setters, canManage: granted.has('secret_store.manage') };
    },
  });

  private readonly loaded = computed(() => (this.data.hasValue() ? this.data.value() : undefined));
  protected readonly environments = computed(() => this.loaded()?.environments ?? []);
  protected readonly canManage = computed(() => this.loaded()?.canManage ?? false);
  /** Where the control plane's federation issuer is: it serves this web app (ADR 0016). */
  protected readonly issuer = computed(() => `${this.document.location.origin}/federation`);

  protected readonly stores = computed(() =>
    (this.loaded()?.stores ?? []).map((store) => ({
      store,
      kind: kinds.find((k) => k.value === store.kind)?.label ?? store.kind,
      location: storeLocation(store),
      opening: storeOpening(store),
      environments:
        store.environment_ids.length === 0
          ? 'Every Environment'
          : store.environment_ids.map((id) => this.environmentName(id)).join(', '),
      mints: store.access.mode === 'brokered' && credentialFields(store.kind).length > 0,
      federation:
        store.access.mode === 'federated'
          ? {
              audience: federationAudience(store),
              subjects: (store.environment_ids.length > 0
                ? store.environment_ids
                : this.environments().map((e) => e.id)
              ).map((id) => ({
                environment: this.environmentName(id),
                subject: federationSubject(store, id),
              })),
            }
          : null,
      setBy:
        this.loaded()?.setters.find((p) => p.id === store.credential_set_by)?.display_name ??
        store.credential_set_by ??
        null,
    })),
  );

  protected readonly failure = computed(() => {
    const error = this.data.error();
    return error ? (error instanceof ApiError ? error.message : 'Something went wrong.') : null;
  });

  private environmentName(id: string): string {
    return this.environments().find((e) => e.id === id)?.display_name ?? id;
  }

  // Adding a store.
  protected readonly adding = signal(false);

  protected added(): void {
    this.adding.set(false);
    this.data.reload();
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
