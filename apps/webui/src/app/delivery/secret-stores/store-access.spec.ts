import { SecretStore } from '../../api/models';
import {
  credentialDropped,
  emptyStore,
  fieldsOf,
  storeAccess,
  storeProblem,
  StoreFields,
  storeSettings,
  withChange,
} from './store-access';

const fields = (change: Partial<StoreFields>): StoreFields => withChange(emptyStore(), change);

describe('store access', () => {
  it('keeps the mode and method to ones the kind can use', () => {
    expect(fields({ kind: 'kubernetes', mode: 'brokered' }).mode).toBe('agent');
    expect(fields({ kind: 'infisical', mode: 'agent' }).method).toBe('universal_auth');
    expect(fields({ kind: 'infisical', mode: 'federated', method: 'kubernetes' }).method).toBe(
      'oidc',
    );
    expect(fields({ kind: 'infisical', mode: 'brokered', method: 'oidc' }).method).toBe('');
    expect(fields({ kind: 'vault', mode: 'agent', method: 'oidc' }).method).toBe('');
  });

  it('sends only the fields the kind takes in its mode', () => {
    const typed = {
      roleArn: 'arn:aws:iam::1:role/ivaldi',
      role: 'ivaldi',
      tokenRole: 'payments',
      credential: 'doppler-production',
      identityId: 'idt_1',
    };
    expect(storeAccess(fields({ ...typed, kind: 'vault', mode: 'brokered' }))).toEqual({
      mode: 'brokered',
      token_role: 'payments',
      auth_mount: undefined,
    });
    expect(storeAccess(fields({ ...typed, kind: 'vault', mode: 'federated' }))).toEqual({
      mode: 'federated',
      role: 'ivaldi',
      auth_mount: undefined,
    });
    expect(storeAccess(fields({ ...typed, kind: 'doppler', mode: 'federated' }))).toEqual({
      mode: 'federated',
      identity_id: 'idt_1',
    });
    expect(storeAccess(fields({ ...typed, kind: 'doppler', mode: 'brokered' }))).toEqual({
      mode: 'brokered',
    });
    expect(storeAccess(fields({ ...typed, kind: 'infisical', mode: 'federated' }))).toEqual({
      mode: 'federated',
      method: 'oidc',
      identity_id: 'idt_1',
    });
    expect(storeAccess(fields({ ...typed, kind: 'infisical', mode: 'agent' }))).toEqual({
      mode: 'agent',
      method: 'universal_auth',
      credential: 'doppler-production',
    });
  });

  it('says what each mode still needs', () => {
    const named = { name: 'Store' };
    expect(
      storeProblem(
        fields({ ...named, kind: 'aws_secrets_manager', region: 'eu-west-1', mode: 'brokered' }),
        [],
      ),
    ).toContain('role the control plane assumes');
    expect(
      storeProblem(
        fields({ ...named, kind: 'vault', address: 'https://v', mount: 'kv', mode: 'brokered' }),
        [],
      ),
    ).toContain('token role');
    expect(
      storeProblem(fields({ ...named, kind: 'doppler', mode: 'brokered' }), ['env_1']),
    ).toContain('project and config');
    expect(
      storeProblem(
        fields({
          ...named,
          kind: 'infisical',
          projectId: 'p',
          environment: 'prod',
          mode: 'federated',
        }),
        [],
      ),
    ).toContain('machine identity');
    expect(
      storeProblem(
        fields({
          ...named,
          kind: 'infisical',
          projectId: 'p',
          environment: 'prod',
          mode: 'brokered',
        }),
        [],
      ),
    ).toBeNull();
  });
});

const stored = (changes: Partial<SecretStore>): SecretStore => ({
  id: 'sst_1',
  organization_id: 'org_1',
  workspace_id: 'wsp_1',
  slug: 'store',
  display_name: 'Store',
  kind: 'vault',
  settings: { vault: { address: 'https://vault.example.com', mount: 'kv' } },
  access: { mode: 'brokered', token_role: 'payments' },
  environment_ids: [],
  credential_set_at: '2026-11-26T10:00:00Z',
  credential_set_by: 'prn_ada',
  created_at: '2026-11-26T10:00:00Z',
  updated_at: '2026-11-26T10:00:00Z',
  archived_at: null,
  ...changes,
});

describe('changing a store', () => {
  it('fills the form from the store, and saves it as it was', () => {
    const stores = [
      stored({}),
      stored({
        kind: 'infisical',
        settings: {
          infisical: {
            site_url: 'https://app.infisical.com',
            project_id: 'p',
            environment: 'prod',
            secret_path: '/app',
          },
        },
        access: { mode: 'agent', method: 'kubernetes', identity_id: 'idt_1' },
      }),
      stored({
        kind: 'doppler',
        settings: { doppler: { project: 'payments', config: 'prd' } },
        access: { mode: 'federated', identity_id: 'said_1' },
      }),
    ];
    for (const store of stores) {
      const fields = fieldsOf(store);
      expect(storeSettings(fields)).toEqual(store.settings);
      expect(JSON.parse(JSON.stringify(storeAccess(fields)))).toEqual(store.access);
    }
  });

  it('knows which changes remove the minting credential', () => {
    const store = stored({});
    expect(
      credentialDropped(store, { ...fieldsOf(store), name: 'Renamed', tokenRole: 'other' }),
    ).toBe(false);
    expect(
      credentialDropped(store, { ...fieldsOf(store), address: 'https://vault.example.com/' }),
    ).toBe(false);
    expect(
      credentialDropped(store, { ...fieldsOf(store), address: 'https://elsewhere.example.com' }),
    ).toBe(true);
    expect(credentialDropped(store, { ...fieldsOf(store), mode: 'agent' })).toBe(true);
    expect(
      credentialDropped(stored({ credential_set_at: null }), { ...fieldsOf(store), mode: 'agent' }),
    ).toBe(false);
  });
});
