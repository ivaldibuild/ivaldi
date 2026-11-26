import {
  MintingCredential,
  SecretStore,
  SecretStoreKind,
  SecretStoreMethod,
  SecretStoreMode,
} from '../../api/models';

/** A secret manager, the modes it can be opened in, and what the agent reads from it. */
export interface KindOption {
  value: SecretStoreKind;
  label: string;
  reads: string;
  modes: SecretStoreMode[];
}

export const kinds: KindOption[] = [
  {
    value: 'kubernetes',
    label: 'Kubernetes Secrets',
    reads: 'Secrets in the namespace it deploys to, with its own RBAC',
    modes: ['agent'],
  },
  {
    value: 'aws_secrets_manager',
    label: 'AWS Secrets Manager',
    reads: 'secrets in one region',
    modes: ['agent', 'federated', 'brokered'],
  },
  {
    value: 'vault',
    label: 'HashiCorp Vault',
    reads: 'a KV v2 mount',
    modes: ['agent', 'federated', 'brokered'],
  },
  {
    value: 'doppler',
    label: 'Doppler',
    reads: 'one config',
    modes: ['agent', 'federated', 'brokered'],
  },
  {
    value: 'infisical',
    label: 'Infisical',
    reads: 'one environment of a project, under one folder',
    modes: ['agent', 'federated', 'brokered'],
  },
];

export const modes: { value: SecretStoreMode; label: string; description: string }[] = [
  {
    value: 'agent',
    label: 'Agent',
    description:
      "The agent opens the store with its own identity, or a credential its operator configured on it. Ivaldi can't open the store.",
  },
  {
    value: 'federated',
    label: 'Federated',
    description:
      'Ivaldi signs a five-minute JWT for each Deployment, which your store trusts and exchanges for its own credential. Ivaldi holds no credential.',
  },
  {
    value: 'brokered',
    label: 'Brokered',
    description:
      'You give Ivaldi a credential that can create others, and it mints a read-only one for each Deployment that lasts minutes. Ivaldi must reach the store.',
  },
];

/** How an Infisical store is logged in to, in each mode. */
export const infisicalMethods: Record<
  SecretStoreMode,
  { value: SecretStoreMethod; label: string }[]
> = {
  agent: [
    { value: 'universal_auth', label: 'Universal Auth, with a credential on the agent' },
    { value: 'kubernetes', label: "Kubernetes auth, with the agent's service account" },
  ],
  federated: [
    { value: 'oidc', label: 'OIDC auth, which fetches the issuer’s keys' },
    { value: 'jwt', label: 'JWT auth, with the issuer’s keys copied to it' },
  ],
  brokered: [],
};

/** Everything the store form asks for, as typed. */
export interface StoreFields {
  name: string;
  kind: SecretStoreKind;
  mode: SecretStoreMode;
  method: SecretStoreMethod | '';
  region: string;
  roleArn: string;
  address: string;
  mount: string;
  role: string;
  authMount: string;
  tokenRole: string;
  credential: string;
  identityId: string;
  project: string;
  config: string;
  siteUrl: string;
  projectId: string;
  environment: string;
  secretPath: string;
}

export const emptyStore = (): StoreFields => ({
  name: '',
  kind: 'kubernetes',
  mode: 'agent',
  method: '',
  region: '',
  roleArn: '',
  address: '',
  mount: '',
  role: '',
  authMount: '',
  tokenRole: '',
  credential: '',
  identityId: '',
  project: '',
  config: '',
  siteUrl: '',
  projectId: '',
  environment: '',
  secretPath: '',
});

/** The fields after a change, keeping the mode and method to ones the kind can use. */
export function withChange(fields: StoreFields, change: Partial<StoreFields>): StoreFields {
  const next = { ...fields, ...change };
  const allowed = kinds.find((k) => k.value === next.kind)?.modes ?? ['agent'];
  if (!allowed.includes(next.mode)) {
    next.mode = 'agent';
  }
  const methods = next.kind === 'infisical' ? infisicalMethods[next.mode] : [];
  if (!methods.some((m) => m.value === next.method)) {
    next.method = methods[0]?.value ?? '';
  }
  return next;
}

/** What stops the store from being created, in words, or null. */
export function storeProblem(f: StoreFields, environmentIds: string[]): string | null {
  const has = (value: string) => value.trim() !== '';
  if (!has(f.name)) {
    return 'Name the store.';
  }
  switch (f.kind) {
    case 'aws_secrets_manager':
      if (!has(f.region)) {
        return 'Give the AWS region the secrets are in.';
      }
      if (f.mode !== 'agent' && !has(f.roleArn)) {
        return f.mode === 'federated'
          ? 'Give the role that trusts the federation issuer.'
          : 'Give the role the control plane assumes.';
      }
      return null;
    case 'vault':
      if (!has(f.address) || !has(f.mount)) {
        return "Give Vault's address and the KV v2 mount.";
      }
      if (f.mode === 'brokered' && !has(f.tokenRole)) {
        return 'Give the token role each Deployment’s token is created from.';
      }
      if (f.mode !== 'brokered' && !has(f.role)) {
        return 'Give the auth role the agent logs in as.';
      }
      return null;
    case 'doppler':
      if (f.mode === 'agent' && !has(f.credential)) {
        return 'Name the credential, configured on the agent, that holds the Doppler token.';
      }
      if (f.mode === 'federated' && !has(f.identityId)) {
        return 'Give the service account identity that trusts the federation issuer.';
      }
      if (f.mode === 'agent' && has(f.project) !== has(f.config)) {
        return 'Give both the Doppler project and config, or neither for a service token.';
      }
      if (f.mode !== 'agent' && !(has(f.project) && has(f.config))) {
        return 'Give the Doppler project and config the store reads.';
      }
      if (environmentIds.length === 0) {
        return 'A Doppler store serves one config: tick the Environments that config is for.';
      }
      return null;
    case 'infisical':
      if (!has(f.projectId) || !has(f.environment)) {
        return 'Give the Infisical project ID and the environment’s slug.';
      }
      if (f.secretPath.trim() && !f.secretPath.trim().startsWith('/')) {
        return 'The folder starts with /.';
      }
      if (f.method === 'universal_auth' && !has(f.credential)) {
        return 'Name the credential, configured on the agent, that holds the Universal Auth client ID and secret.';
      }
      if (f.mode !== 'brokered' && f.method !== 'universal_auth' && !has(f.identityId)) {
        return 'Give the machine identity the agent logs in as.';
      }
      return null;
    default:
      return null;
  }
}

/** Where the store is, as the control plane takes it. */
export function storeSettings(f: StoreFields): SecretStore['settings'] {
  switch (f.kind) {
    case 'aws_secrets_manager':
      return { aws: { region: f.region.trim() } };
    case 'vault':
      return { vault: { address: f.address.trim(), mount: f.mount.trim() } };
    case 'doppler':
      return f.project.trim()
        ? { doppler: { project: f.project.trim(), config: f.config.trim() } }
        : {};
    case 'infisical':
      return {
        infisical: {
          site_url: f.siteUrl.trim(),
          project_id: f.projectId.trim(),
          environment: f.environment.trim(),
          secret_path: f.secretPath.trim(),
        },
      };
    default:
      return {};
  }
}

/** How the store is opened, with only the fields its kind takes in its mode. */
export function storeAccess(f: StoreFields): SecretStore['access'] {
  const set = (value: string) => value.trim() || undefined;
  const mode = f.mode;
  switch (f.kind) {
    case 'aws_secrets_manager':
      return { mode, role_arn: set(f.roleArn) };
    case 'vault':
      return mode === 'brokered'
        ? { mode, token_role: set(f.tokenRole), auth_mount: set(f.authMount) }
        : { mode, role: set(f.role), auth_mount: set(f.authMount) };
    case 'doppler':
      if (mode === 'agent') {
        return { mode, credential: set(f.credential) };
      }
      return mode === 'federated' ? { mode, identity_id: set(f.identityId) } : { mode };
    case 'infisical':
      if (mode === 'brokered') {
        return { mode };
      }
      return f.method === 'universal_auth'
        ? { mode, method: f.method, credential: set(f.credential) }
        : { mode, method: f.method || undefined, identity_id: set(f.identityId) };
    default:
      return { mode: 'agent' };
  }
}

/** Where a store's secrets are. */
export function storeLocation(store: SecretStore): string {
  const { aws, vault, doppler, infisical } = store.settings;
  if (aws) {
    return aws.region;
  }
  if (vault) {
    return `${vault.address}, mount ${vault.mount}`;
  }
  if (store.kind === 'doppler') {
    return doppler
      ? `${doppler.project}/${doppler.config}`
      : 'The config its service token implies';
  }
  if (infisical) {
    return `${infisical.site_url}, project ${infisical.project_id}, ${infisical.environment}:${infisical.secret_path}`;
  }
  return "Secrets in each Target's namespace";
}

/** How a store is opened, in words. */
export function storeOpening(store: SecretStore): string {
  const a = store.access;
  switch (a.mode) {
    case 'federated':
      return `Federated: ${a.role_arn ?? a.role ?? `identity ${a.identity_id}`}`;
    case 'brokered':
      if (store.kind === 'aws_secrets_manager') {
        return `Brokered: the control plane assumes ${a.role_arn}`;
      }
      if (store.kind === 'vault') {
        return `Brokered: tokens from the token role ${a.token_role}`;
      }
      return 'Brokered';
  }
  switch (store.kind) {
    case 'kubernetes':
      return "Agent: the agent's own RBAC";
    case 'aws_secrets_manager':
      return a.role_arn
        ? `Agent: its own AWS identity, as ${a.role_arn}`
        : 'Agent: its own AWS identity';
    case 'vault':
      return `Agent: Kubernetes auth as role ${a.role}`;
    case 'infisical':
      return a.method === 'kubernetes'
        ? `Agent: Kubernetes auth as identity ${a.identity_id}`
        : `Agent: Universal Auth, credential ${a.credential}`;
    default:
      return `Agent: credential ${a.credential}`;
  }
}

/** The fields a minting credential can have. */
export type CredentialKey = Exclude<keyof MintingCredential, '$schema'>;

/** A field of a brokered store's minting credential. */
export interface CredentialField {
  key: CredentialKey;
  label: string;
  secret: boolean;
}

/** The minting credential a brokered store needs, or none: AWS is brokered without one. */
export function credentialFields(kind: SecretStoreKind): CredentialField[] {
  switch (kind) {
    case 'vault':
      return [
        { key: 'role_id', label: 'AppRole role ID', secret: false },
        { key: 'secret_id', label: 'AppRole secret ID', secret: true },
      ];
    case 'doppler':
      return [{ key: 'token', label: 'Service account token', secret: true }];
    case 'infisical':
      return [
        { key: 'client_id', label: 'Universal Auth client ID', secret: false },
        { key: 'client_secret', label: 'Universal Auth client secret', secret: true },
      ];
    default:
      return [];
  }
}

/** A federated store's audience: fixed, and unique to it. */
export function federationAudience(store: SecretStore): string {
  return `ivaldi:${store.id}`;
}

/** The subject a federated store's JWTs carry for each Environment that may read it. */
export function federationSubject(store: SecretStore, environmentId: string): string {
  return `org:${store.organization_id}:store:${store.id}:env:${environmentId}`;
}

/** The form's fields for an existing store, to change it. */
export function fieldsOf(store: SecretStore): StoreFields {
  const { aws, vault, doppler, infisical } = store.settings;
  const a = store.access;
  return withChange(emptyStore(), {
    name: store.display_name,
    kind: store.kind,
    mode: a.mode,
    method: a.method ?? '',
    region: aws?.region ?? '',
    roleArn: a.role_arn ?? '',
    address: vault?.address ?? '',
    mount: vault?.mount ?? '',
    role: a.role ?? '',
    authMount: a.auth_mount ?? '',
    tokenRole: a.token_role ?? '',
    credential: a.credential ?? '',
    identityId: a.identity_id ?? '',
    project: doppler?.project ?? '',
    config: doppler?.config ?? '',
    siteUrl: infisical?.site_url ?? '',
    projectId: infisical?.project_id ?? '',
    environment: infisical?.environment ?? '',
    secretPath: infisical?.secret_path ?? '',
  });
}

/**
 * Where a store's minting credential is sent, or what it is for, as the control plane decides it:
 * changing it removes the credential.
 */
function destination(store: SecretStore['settings'], kind: SecretStore['kind']): string {
  const trim = (url = '') => url.trim().replace(/\/+$/, '');
  switch (kind) {
    case 'vault':
      return trim(store.vault?.address);
    case 'doppler':
      return `${store.doppler?.project ?? ''}/${store.doppler?.config ?? ''}`;
    case 'infisical':
      return `${trim(store.infisical?.site_url) || 'https://app.infisical.com'}/${store.infisical?.project_id ?? ''}`;
    case 'aws_secrets_manager':
      return store.aws?.region ?? '';
    default:
      return '';
  }
}

/** Whether saving the fields removes the store's minting credential: a new mode, or destination. */
export function credentialDropped(store: SecretStore, f: StoreFields): boolean {
  return (
    store.credential_set_at !== null &&
    (f.mode !== store.access.mode ||
      destination(storeSettings(f), store.kind) !== destination(store.settings, store.kind))
  );
}
