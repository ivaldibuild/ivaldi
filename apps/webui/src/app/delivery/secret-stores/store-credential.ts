import { DatePipe } from '@angular/common';
import { Component, computed, inject, input, output, signal } from '@angular/core';
import { form, FormField, submit } from '@angular/forms/signals';
import { HlmAlertImports } from '@spartan-ng/helm/alert';
import { HlmButtonImports } from '@spartan-ng/helm/button';
import { HlmFieldImports } from '@spartan-ng/helm/field';
import { HlmInputImports } from '@spartan-ng/helm/input';
import { HlmSpinnerImports } from '@spartan-ng/helm/spinner';
import { MintingCredential, SecretStore } from '../../api/models';
import { RequestStatus } from '../../shared/request-status';
import { SecretStoresApi } from './secret-stores-api';
import { CredentialKey, credentialFields } from './store-access';

const emptyCredential = (): Record<CredentialKey, string> => ({
  token: '',
  role_id: '',
  secret_id: '',
  client_id: '',
  client_secret: '',
});

/**
 * A brokered store's minting credential: whether it is set, and by whom, and setting, replacing,
 * or removing it. It is write-only: the control plane keeps it encrypted and never shows it again,
 * so the form is emptied once it is saved.
 */
@Component({
  selector: 'app-store-credential',
  imports: [
    DatePipe,
    FormField,
    HlmAlertImports,
    HlmButtonImports,
    HlmFieldImports,
    HlmInputImports,
    HlmSpinnerImports,
  ],
  template: `
    <div class="flex flex-col gap-3">
      @if (store().credential_set_at; as setAt) {
        <p class="text-sm">
          <span class="text-muted-foreground">Minting credential set</span>
          {{ setAt | date: 'medium' }}
          @if (setBy(); as name) {
            <span class="text-muted-foreground">by</span> {{ name }}
          }
        </p>
      } @else {
        <p class="text-destructive-text text-sm">
          No minting credential yet: Deployments that read this store are refused until one is set.
        </p>
      }

      @if (canManage() && !editing() && !removing()) {
        <div class="flex flex-wrap gap-2">
          <button
            hlmBtn
            variant="outline"
            size="sm"
            type="button"
            [attr.aria-label]="setLabel() + ' for ' + store().display_name"
            (click)="startEditing()"
          >
            {{ setLabel() }}
          </button>
          @if (store().credential_set_at) {
            <button
              hlmBtn
              variant="outline"
              size="sm"
              type="button"
              [attr.aria-label]="'Remove credential for ' + store().display_name"
              (click)="removing.set(true)"
            >
              Remove credential
            </button>
          }
        </div>
      }

      @if (editing()) {
        <form
          class="grid gap-3 md:grid-cols-2"
          novalidate
          [attr.aria-label]="setLabel() + ' for ' + store().display_name"
          (submit)="save($event)"
        >
          @if (invalid() || status.error(); as message) {
            <div hlmAlert variant="destructive" role="alert" class="md:col-span-2">
              <p hlmAlertDescription>{{ message }}</p>
            </div>
          }
          @for (field of fields(); track field.key) {
            <div hlmField>
              <label hlmFieldLabel [for]="store().id + '-' + field.key">{{ field.label }}</label>
              <input
                hlmInput
                autocomplete="off"
                spellcheck="false"
                [id]="store().id + '-' + field.key"
                [type]="field.secret ? 'password' : 'text'"
                [formField]="credential[field.key]"
              />
            </div>
          }
          <p class="text-muted-foreground text-[13px] md:col-span-2">
            It's kept encrypted and never shown again. Changing where the store is, or its mode,
            removes it.
          </p>
          <div class="flex flex-wrap gap-2 md:col-span-2">
            <button hlmBtn size="sm" type="submit" [disabled]="status.saving()">
              @if (status.saving()) {
                <hlm-spinner aria-hidden="true" /> Saving…
              } @else {
                Save credential
              }
            </button>
            <button hlmBtn variant="outline" size="sm" type="button" (click)="editing.set(false)">
              Cancel
            </button>
          </div>
        </form>
      }

      @if (removing()) {
        <div
          hlmAlert
          role="alertdialog"
          [attr.aria-label]="'Remove credential for ' + store().display_name"
        >
          <p hlmAlertDescription>
            Deployments that read {{ store().display_name }} are refused until a credential is set
            again.
          </p>
          @if (status.error(); as message) {
            <p class="text-destructive-text mt-2 text-sm" role="alert">{{ message }}</p>
          }
          <div class="mt-3 flex flex-wrap gap-2">
            <button hlmBtn size="sm" type="button" [disabled]="status.saving()" (click)="remove()">
              Remove credential
            </button>
            <button hlmBtn variant="outline" size="sm" type="button" (click)="removing.set(false)">
              Keep it
            </button>
          </div>
        </div>
      }
    </div>
  `,
})
export class StoreCredential {
  private readonly api = inject(SecretStoresApi);

  readonly store = input.required<SecretStore>();
  readonly canManage = input(false);
  /** The name of whoever set the credential, when known. */
  readonly setBy = input<string | null>(null);
  readonly changed = output<SecretStore>();

  protected readonly fields = computed(() => credentialFields(this.store().kind));
  protected readonly setLabel = computed(() =>
    this.store().credential_set_at ? 'Replace credential' : 'Set credential',
  );

  private readonly model = signal(emptyCredential());
  protected readonly credential = form(this.model);
  protected readonly editing = signal(false);
  protected readonly removing = signal(false);
  protected readonly status = new RequestStatus();
  protected readonly invalid = signal<string | null>(null);

  protected startEditing(): void {
    this.credential().reset(emptyCredential());
    this.invalid.set(null);
    this.status.error.set(null);
    this.editing.set(true);
  }

  protected async save(event: Event): Promise<void> {
    event.preventDefault();
    await submit(this.credential, async () => {
      const values = this.model();
      const missing = this.fields().find((f) => !values[f.key].trim());
      this.invalid.set(missing ? `Give the ${missing.label.toLowerCase()}.` : null);
      if (missing) {
        return undefined;
      }
      const body: MintingCredential = {};
      for (const f of this.fields()) {
        body[f.key] = values[f.key].trim();
      }
      let saved: SecretStore | undefined;
      await this.status.run(async () => {
        saved = await this.api.setCredential(this.store().id, body);
      });
      if (saved) {
        // Nothing the credential held stays in the page.
        this.credential().reset(emptyCredential());
        this.editing.set(false);
        this.changed.emit(saved);
      }
      return undefined;
    });
  }

  protected async remove(): Promise<void> {
    let saved: SecretStore | undefined;
    await this.status.run(async () => {
      saved = await this.api.removeCredential(this.store().id);
    });
    if (saved) {
      this.removing.set(false);
      this.changed.emit(saved);
    }
  }
}
