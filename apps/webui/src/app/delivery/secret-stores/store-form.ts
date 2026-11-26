import { Component, computed, inject, input, linkedSignal, output, signal } from '@angular/core';
import { form, FormField, submit } from '@angular/forms/signals';
import { HlmAlertImports } from '@spartan-ng/helm/alert';
import { HlmButtonImports } from '@spartan-ng/helm/button';
import { HlmFieldImports } from '@spartan-ng/helm/field';
import { HlmInputImports } from '@spartan-ng/helm/input';
import { HlmSpinnerImports } from '@spartan-ng/helm/spinner';
import { Environment, SecretStore } from '../../api/models';
import { RequestStatus } from '../../shared/request-status';
import { WorkspaceContext } from '../workspace-context';
import { SecretStoresApi } from './secret-stores-api';
import {
  credentialDropped,
  emptyStore,
  fieldsOf,
  infisicalMethods,
  kinds,
  modes,
  storeAccess,
  storeProblem,
  StoreFields,
  storeSettings,
  withChange,
} from './store-access';

/**
 * Adds a Secret Store, or changes one: the secret manager, where it is, how it is opened, and
 * which Environments may read it. Each mode asks only what its kind needs; a brokered store's
 * minting credential is set once the store exists. A store's kind never changes.
 */
@Component({
  selector: 'app-secret-store-form',
  imports: [
    FormField,
    HlmAlertImports,
    HlmButtonImports,
    HlmFieldImports,
    HlmInputImports,
    HlmSpinnerImports,
  ],
  templateUrl: './store-form.html',
})
export class SecretStoreForm {
  private readonly api = inject(SecretStoresApi);
  private readonly context = inject(WorkspaceContext);

  readonly environments = input.required<Environment[]>();
  /** The store to change; a new one is added without it. */
  readonly existing = input<SecretStore | null>(null);
  readonly saved = output<SecretStore>();
  readonly cancelled = output<void>();

  protected readonly kinds = kinds;
  private readonly model = linkedSignal(() => {
    const store = this.existing();
    return store ? fieldsOf(store) : emptyStore();
  });
  protected readonly store = form(this.model);
  /** The fields, with the mode and method kept to ones the kind can use. */
  protected readonly fields = computed(() => withChange(this.model(), {}));
  protected readonly kind = computed(() => kinds.find((k) => k.value === this.fields().kind)!);
  protected readonly modes = computed(() =>
    modes.filter((m) => this.kind().modes.includes(m.value)),
  );
  protected readonly methods = computed(() =>
    this.fields().kind === 'infisical' ? infisicalMethods[this.fields().mode] : [],
  );

  protected readonly chosen = linkedSignal(() => this.existing()?.environment_ids ?? []);
  /** Ids are per form, since one store's form can be open beside another's. */
  protected readonly headingId = computed(() => `store-form-${this.existing()?.id ?? 'new'}`);
  /** Whether saving removes the store's minting credential, which must then be set again. */
  protected readonly dropsCredential = computed(() => {
    const store = this.existing();
    return store !== null && credentialDropped(store, this.fields());
  });
  protected readonly status = new RequestStatus();
  protected readonly invalid = signal<string | null>(null);

  protected setMethod(event: Event): void {
    const method = (event.target as HTMLSelectElement).value as StoreFields['method'];
    this.model.update((fields) => ({ ...fields, method }));
  }

  protected toggleEnvironment(id: string, event: Event): void {
    const checked = (event.target as HTMLInputElement).checked;
    this.chosen.update((ids) => (checked ? [...ids, id] : ids.filter((other) => other !== id)));
  }

  protected async save(event: Event): Promise<void> {
    event.preventDefault();
    await submit(this.store, async () => {
      const fields = this.fields();
      const problem = storeProblem(fields, this.chosen());
      this.invalid.set(problem);
      const workspaceId = this.context.current()?.id;
      if (problem || !workspaceId) {
        return undefined;
      }
      const changes = {
        display_name: fields.name.trim(),
        settings: storeSettings(fields),
        access: storeAccess(fields),
        environment_ids: this.chosen(),
      };
      const existing = this.existing();
      let done: SecretStore | undefined;
      await this.status.run(async () => {
        done = existing
          ? await this.api.update(existing.id, changes)
          : await this.api.create(workspaceId, { ...changes, kind: fields.kind });
      });
      if (done) {
        this.saved.emit(done);
      }
      return undefined;
    });
  }
}
