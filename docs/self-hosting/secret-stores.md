# Secret Stores

A **Secret Store** tells a Cluster's agent where to read an Application's secret variables from. Ivaldi keeps only where a secret is: the store, a path, an optional field, and an optional version. It never holds a secret or a credential for the store. The agent reads the secret inside your cluster, with its own identity, when it deploys ([ADR 0004](../adr/0004-credentials-and-keys.md), [deployment variables](../design/deployment-variables.md)).

A store has **settings**, which say where it is, and an **access**, which says how it is opened. Every store is in `agent` mode so far: the agent opens it with its own identity, or with a credential its operator configured on it under a name the store gives. [ADR 0017](../adr/0017-control-plane-grants-secret-store-access.md) adds modes in which the control plane grants access instead ([secret-store access](../design/secret-store-access.md)).

Add stores under **Secret Stores** in the web app, or with `POST /v1/workspaces/{workspace_id}/secret-stores`. A store belongs to a Workspace. Reading stores needs `secret_store.read` on the Workspace and changing them needs `secret_store.manage`.

## Which Environments may read a store

A store lists the Environments whose Deployments may read it. A store that lists none serves every Environment in its Workspace.

One agent serves a whole Cluster with one identity, so it can read every secret that identity is allowed to. Two things keep a staging Deployment from reading production's secrets:

- **In Ivaldi:** give production its own store and list only production on it. Ivaldi refuses a secret value scoped to an Environment the store does not list, and refuses a Deployment whose secrets are in a store its Environment may not read.
- **In your secret manager:** scope the policy or role the agent's identity holds as narrowly as you need.

## Kinds

### `kubernetes`

Reads Secrets in the namespace the Target deploys to. The path is the Secret's name, and the field is one of its keys. It has no settings.

The agent may read Secrets only in the namespaces you list in the agent chart:

```yaml
secrets:
  kubernetesNamespaces: [payments, checkout]
```

Use this store when another tool, such as External Secrets or the Doppler operator, already writes the Secret.

### `aws_secrets_manager`

Settings: the AWS `region`. Access: optionally a `role_arn` the agent assumes on top of its own identity. The path is the secret's name or ARN, and the field is one key of a JSON secret.

Give the agent's ServiceAccount an AWS identity with EKS Pod Identity or IRSA:

```yaml
serviceAccount:
  annotations:
    eks.amazonaws.com/role-arn: arn:aws:iam::123456789012:role/ivaldi-agent
```

The role needs `secretsmanager:GetSecretValue` on the secrets it should read.

### `vault`

Settings: Vault's `address`, the KV v2 `mount`, and optionally an Enterprise `namespace`. Access: the Kubernetes auth `role` the agent logs in as, and optionally the `auth_mount` (default `kubernetes`). The path is the secret's path within the mount, and the field is one of its keys.

Bind the role to the agent's ServiceAccount and namespace in Vault's Kubernetes auth method, with a policy that can read the paths it should.

The agent logs in by sending Vault a service-account token, and a store's address is set in Ivaldi by whoever manages the Workspace's stores. So the agent's operator decides which Vault servers it may use, in the agent chart's values:

```yaml
secrets:
  vault:
    addresses:
      - https://vault.example.com
    audience: vault
```

- A `vault` store at any other address fails the Deployment before anything is sent, and an agent with no addresses does not offer to read `vault` stores at all, so the deploy plan refuses them.
- The token is projected for `audience` (default `vault`), never the Pod's Kubernetes API token, so a Vault that receives it cannot use it on the cluster's API. Set the same `audience` on the Kubernetes auth role, or Vault rejects the login.
- The agent follows no redirect from Vault. Give it the address that answers, such as the active node's or a load balancer's.

### `doppler`

Settings: optionally the Doppler `project` and `config`, both or neither. Access: the name of the `credential` that holds the Doppler token. The path is the secret's name in Doppler.

A Doppler store serves one config, so it must list the Environments that config is for. A service token implies its config; another token, such as a service account's, needs the store's `project` and `config`.

A store names a credential, never where it is kept. The agent's operator maps each name to a Secret in the agent's namespace, and its key, in the chart; the agent may read only those Secrets, and a Deployment whose store names a credential the Cluster's agent does not have is refused before it starts:

```yaml
secrets:
  credentials:
    doppler-production: { secretName: doppler-production, key: token }
```

## Versions and rollbacks

A secret value may pin a version. Without one, each Deployment reads the latest, which is how a redeploy picks up a rotated secret.

The agent reports the version it read and a keyed fingerprint of the value, and the Deployment records both; never the value. The fingerprint's key stays in the cluster, in the Secret `fingerprint.secretName` names. Back that Secret up with the cluster's others: a rollback compares against fingerprints made with it.

A rollback reads the versions the Deployment it restores recorded, so keep enough history in the store. Vault's KV v2 keeps ten versions by default, and AWS Secrets Manager removes versions that have no staging label.

## How the chart receives a secret

By default the agent writes one immutable Kubernetes Secret per Deployment in the Target's namespace, with one key per secret variable, and passes its name to the chart as `global.ivaldi.secrets.name`.

A secret variable can instead be delivered as a value, written into the chart's values at its values path, for charts that cannot read a Secret. Helm keeps values in its release history, so the secret then sits in the namespace's Helm release Secrets and shows in `helm get values`.
