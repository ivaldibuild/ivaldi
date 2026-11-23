import { signal } from '@angular/core';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { ApiError } from '../../api/api-client';
import { SecretStore } from '../../api/models';
import { PermissionsApi } from '../../api/permissions-api';
import { DeliveryApi } from '../delivery-api';
import { SecretStoreFields, SecretStoresApi } from '../secret-stores-api';
import { WorkspaceContext } from '../workspace-context';
import { SecretStoresPage } from './secret-stores-page';

const production = 'env_01JZ0000000000000000000001';
const store = (changes: Partial<SecretStore>): SecretStore => ({
  id: 'sst_1',
  organization_id: 'org_1',
  workspace_id: 'wsp_1',
  slug: 'in-cluster',
  display_name: 'In-cluster',
  kind: 'kubernetes',
  settings: {},
  access: { mode: 'agent' },
  environment_ids: [],
  created_at: '2026-11-15T10:00:00Z',
  updated_at: '2026-11-15T10:00:00Z',
  archived_at: null,
  ...changes,
});

describe('SecretStoresPage', () => {
  let created: SecretStoreFields[];
  let archive: (id: string) => Promise<SecretStore>;

  async function render(
    granted = ['secret_store.manage'],
  ): Promise<{ fixture: ComponentFixture<SecretStoresPage>; el: HTMLElement }> {
    created = [];
    archive = () => Promise.resolve(store({}));
    TestBed.configureTestingModule({
      imports: [SecretStoresPage],
      providers: [
        {
          provide: SecretStoresApi,
          useValue: {
            list: () =>
              Promise.resolve([
                store({}),
                store({
                  id: 'sst_2',
                  display_name: 'Vault',
                  kind: 'vault',
                  settings: {
                    vault: { address: 'https://vault.example.com', mount: 'kv' },
                  },
                  access: { mode: 'agent', role: 'ivaldi' },
                  environment_ids: [production],
                }),
              ]),
            create: (_workspaceId: string, fields: SecretStoreFields) => {
              created.push(fields);
              return Promise.resolve(store({}));
            },
            archive: (id: string) => archive(id),
          },
        },
        {
          provide: DeliveryApi,
          useValue: {
            environments: () => Promise.resolve([{ id: production, display_name: 'Production' }]),
          },
        },
        { provide: PermissionsApi, useValue: { granted: () => Promise.resolve(new Set(granted)) } },
        {
          provide: WorkspaceContext,
          useValue: { current: signal({ id: 'wsp_1', display_name: 'Platform' }) },
        },
      ],
    });
    const fixture = TestBed.createComponent(SecretStoresPage);
    fixture.detectChanges();
    await settle(fixture);
    return { fixture, el: fixture.nativeElement as HTMLElement };
  }

  async function settle(fixture: ComponentFixture<unknown>): Promise<void> {
    for (let i = 0; i < 4; i++) {
      await new Promise((resolve) => setTimeout(resolve));
      fixture.detectChanges();
    }
  }

  function button(el: HTMLElement, label: string): HTMLButtonElement {
    const found = [...el.querySelectorAll('button')].find(
      (b) => b.getAttribute('aria-label') === label || b.textContent?.trim() === label,
    );
    expect(found, `a button labelled "${label}"`).toBeTruthy();
    return found!;
  }

  function type(el: HTMLElement, selector: string, value: string): void {
    const input = el.querySelector<HTMLInputElement>(selector)!;
    input.value = value;
    input.dispatchEvent(new Event('input'));
  }

  it('says where each store is and which Environments may read it', async () => {
    const { el } = await render();
    const items = [...el.querySelectorAll('li')].map((li) => li.textContent?.replace(/\s+/g, ' '));
    expect(items[0]).toContain('In-cluster Kubernetes Secrets');
    expect(items[0]).toContain("Secrets in each Target's namespace");
    expect(items[0]).toContain('Read by Every Environment');
    expect(items[1]).toContain('https://vault.example.com, mount kv, role ivaldi');
    expect(items[1]).toContain('Read by Production');
  });

  it("asks for the kind's settings, and creates the store for the ticked Environments", async () => {
    const { fixture, el } = await render();
    button(el, 'Add secret store').click();
    fixture.detectChanges();

    type(el, '#store-name', 'Production secrets');
    const kind = el.querySelector<HTMLSelectElement>('#store-kind')!;
    kind.value = 'aws_secrets_manager';
    kind.dispatchEvent(new Event('change'));
    fixture.detectChanges();
    el.querySelector('form')!.dispatchEvent(new Event('submit'));
    await settle(fixture);
    expect(el.querySelector('form [role="alert"]')?.textContent).toContain('AWS region');
    expect(created).toEqual([]);

    type(el, '#store-region', 'eu-west-1');
    el.querySelector<HTMLInputElement>('fieldset input[type="checkbox"]')!.click();
    el.querySelector('form')!.dispatchEvent(new Event('submit'));
    await settle(fixture);
    expect(created).toEqual([
      {
        display_name: 'Production secrets',
        kind: 'aws_secrets_manager',
        settings: { aws: { region: 'eu-west-1' } },
        access: { mode: 'agent', role_arn: undefined },
        environment_ids: [production],
      },
    ]);
  });

  it('creates a Doppler store with the credential the agent holds its token under', async () => {
    const { fixture, el } = await render();
    button(el, 'Add secret store').click();
    fixture.detectChanges();

    type(el, '#store-name', 'Doppler production');
    const kind = el.querySelector<HTMLSelectElement>('#store-kind')!;
    kind.value = 'doppler';
    kind.dispatchEvent(new Event('change'));
    fixture.detectChanges();
    type(el, '#store-credential', 'doppler-production');
    type(el, '#store-project', 'payments');
    el.querySelector<HTMLInputElement>('fieldset input[type="checkbox"]')!.click();
    el.querySelector('form')!.dispatchEvent(new Event('submit'));
    await settle(fixture);
    expect(el.querySelector('form [role="alert"]')?.textContent).toContain('project and config');
    expect(created).toEqual([]);

    type(el, '#store-config', 'prd');
    el.querySelector('form')!.dispatchEvent(new Event('submit'));
    await settle(fixture);
    expect(created).toEqual([
      {
        display_name: 'Doppler production',
        kind: 'doppler',
        settings: { doppler: { project: 'payments', config: 'prd' } },
        access: { mode: 'agent', credential: 'doppler-production' },
        environment_ids: [production],
      },
    ]);
  });

  it('says why a store could not be archived', async () => {
    const { fixture, el } = await render();
    archive = () => Promise.reject(new ApiError(409, 'In use: 2 variables have a value in it.'));
    button(el, 'Archive In-cluster').click();
    fixture.detectChanges();
    button(el, 'Archive secret store').click();
    await settle(fixture);
    expect(el.querySelector('[role="alertdialog"] [role="alert"]')?.textContent).toContain(
      '2 variables have a value in it.',
    );
  });

  it('is read-only without secret_store.manage on the Workspace', async () => {
    const { el } = await render([]);
    expect(el.textContent).toContain('In-cluster');
    expect(el.querySelectorAll('button').length).toBe(0);
  });
});
