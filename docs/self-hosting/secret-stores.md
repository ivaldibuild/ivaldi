# Secret Stores

A **Secret Store** tells a Cluster's agent where to read an Application's secret variables from. Ivaldi keeps only where a secret is: the store, a path, an optional field, and an optional version. It never holds a secret or a credential for the store. The agent reads the secret inside your cluster, with its own identity, when it deploys ([ADR 0004](../adr/0004-credentials-and-keys.md), [deployment variables](../design/deployment-variables.md)).

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

Settings: the AWS `region`, and optionally a `role_arn` the agent assumes on top of its own identity. The path is the secret's name or ARN, and the field is one key of a JSON secret.

Give the agent's ServiceAccount an AWS identity with EKS Pod Identity or IRSA:

```yaml
serviceAccount:
  annotations:
    eks.amazonaws.com/role-arn: arn:aws:iam::123456789012:role/ivaldi-agent
```

The role needs `secretsmanager:GetSecretValue` on the secrets it should read.

### `vault`

Settings: Vault's `address`, the KV v2 `mount`, and the Kubernetes `auth_role` the agent logs in with; optionally the `auth_mount` (default `kubernetes`) and an Enterprise `namespace`. The path is the secret's path within the mount, and the field is one of its keys.

Bind the role to the agent's ServiceAccount and namespace in Vault's Kubernetes auth method, with a policy that can read the paths it should.

### `doppler`

Settings: the name and key of a Kubernetes Secret, in the agent's namespace, that holds a Doppler service token. The path is the secret's name in Doppler.

A service token serves one Doppler config, so a Doppler store serves one config and must list the Environments that config is for. List the token Secrets the agent may read in the chart:

```yaml
secrets:
  dopplerTokenSecrets: [doppler-production]
```

## Versions and rollbacks

A secret value may pin a version. Without one, each Deployment reads the latest, which is how a redeploy picks up a rotated secret.

The agent reports the version it read and a keyed fingerprint of the value, and the Deployment records both; never the value. The fingerprint's key stays in the cluster, in the Secret `fingerprint.secretName` names. Back that Secret up with the cluster's others: a rollback compares against fingerprints made with it.

A rollback reads the versions the Deployment it restores recorded, so keep enough history in the store. Vault's KV v2 keeps ten versions by default, and AWS Secrets Manager removes versions that have no staging label.

## How the chart receives a secret

By default the agent writes one immutable Kubernetes Secret per Deployment in the Target's namespace, with one key per secret variable, and passes its name to the chart as `global.ivaldi.secrets.name`.

A secret variable can instead be delivered as a value, written into the chart's values at its values path, for charts that cannot read a Secret. Helm keeps values in its release history, so the secret then sits in the namespace's Helm release Secrets and shows in `helm get values`.
