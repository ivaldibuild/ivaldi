# Deployment variables

- Status: Draft for review
- Date: 2026-11-08
- Related: [ADR 0003](../adr/0003-identity-access-and-policy.md), [ADR 0004](../adr/0004-credentials-and-keys.md), [ADR 0006](../adr/0006-release-and-delivery-tracking.md), [delivery data model](delivery-data-model.md), [Kubernetes agent](k8s-agent.md)

This document designs the configuration an Application is deployed with: plain values and secrets, which can differ by Environment, Target, and Channel. It follows Octopus Deploy's [project variables](https://octopus.com/docs/projects/variables), [variable sets](https://octopus.com/docs/projects/variables/library-variable-sets), [sensitive variables](https://octopus.com/docs/projects/variables/sensitive-variables), and [release snapshots](https://octopus.com/docs/releases/creating-a-release). Where it differs, the reason is noted.

The main difference is secrets. Octopus stores sensitive values itself, encrypted. Ivaldi stores only a **reference** to a secret in the customer's secret manager, and the agent resolves it inside the cluster, so the value never reaches Ivaldi ([ADR 0004](../adr/0004-credentials-and-keys.md)).

## Decisions this model is built on

- **A variable is a name with one or more scoped values.** As in Octopus, an Application has variables, and each value can be scoped to Environments, Targets, and Channels. At deploy time the most specific matching value wins.
- **Two kinds of value: `text` and `secret`.** A `text` value is stored in Ivaldi. A `secret` value is a reference (Secret Store, path, field, version) and is resolved by the agent. There is no third kind for secrets that Ivaldi stores encrypted. Customers without a secret manager use the `kubernetes` Secret Store, which reads a Secret in the Target's namespace. An Ivaldi-held store for managed-cloud tenants is ADR 0004's open question, and would be another Secret Store kind rather than a new kind of value.
- **Variable Sets share variables across Applications.** A Variable Set belongs to a Workspace, and Applications include it, like Octopus's library variable sets.
- **A Release takes a snapshot of its variables.** As in Octopus, creating a Release snapshots the variables it will deploy with, so redeploying it or rolling back to it uses the configuration it was released with, not whatever the Application holds that day. The snapshot can be refreshed later, which creates a new revision. A Release's Builds stay immutable once it is published.
- **Every Deployment records the snapshot revision it used and the secret versions it resolved.** Its history says exactly what configuration it shipped.
- **A rollback restores that configuration.** Rolling back replays the snapshot revision and the secret versions of the Deployment being restored, so it brings back the old secret values too, not just the old code.
- **A refresh that changes production needs a human's approval.** It changes what an already-approved Release would deploy, so policy treats it like a production Deployment.
- **Ivaldi resolves scopes; the agent resolves secrets and bindings.** The control plane picks the winning value of each variable for the Deployment's Target. The agent fetches secrets, fills in bindings, and hands the result to the chart.
- **Ambiguity is refused at save time.** Octopus resolves some ties non-deterministically. Ivaldi refuses to save two values that could tie.

## Variables

| Field | Meaning |
|---|---|
| `name` | Unique in its Application or Variable Set, case-insensitive. Letters, digits, `.`, `-`, and `_`, up to 253 characters, which is what a Kubernetes Secret key allows, because secrets become Secret keys. Octopus also allows spaces; Ivaldi does not, for the same reason. Dots namespace names, as Octopus recommends: `Database.Password`. |
| `description` | Optional. |
| `kind` | `text` or `secret`. All values of a variable have its kind. |
| `values_path` | Optional: where the chart receives the variable, such as `database.host`. A variable without one is only used through bindings. |
| `deliver_as` | For `secret` variables: `secret` (the default) or `value` ([Kubernetes agent](k8s-agent.md#variables-and-secrets)). |

Each **value** has its content and its scope:

- A `text` value is a string, which may contain bindings.
- A `secret` value is `store_id`, `path`, an optional `field` (one key of a JSON or key-value secret), and an optional `version` (empty means the latest at deploy time). `path` may contain bindings to `text` variables and system variables, so one value can serve every Environment: `payments/#{Ivaldi.Environment.Slug}/database`.

### Scopes and precedence

A value's scope names Environments, Targets, and Channels, any of which may be empty. As in Octopus, the names within one dimension are alternatives and the dimensions must all match: a value scoped to Environments `staging, production` and Channel `hotfix` applies to a Deployment to either Environment, from a Release in `hotfix`.

When several values of a variable match a Deployment, the most specific one wins, comparing the dimensions in this order:

1. Target
2. Environment
3. Channel

A value scoped to a Target beats one that is not. Between two that tie on Target, one scoped to an Environment wins, and so on. This one rule gives both of Octopus's rules: its precedence list (a machine before an environment before a channel), and "a value scoped twice is more specific than a value scoped once", since a value scoped to an Environment and a Channel beats one scoped only to the Environment. Values with no scope match everything and lose to any scoped value.

Octopus's other dimensions are left out: there are no tenants, no target tags, and no steps, because a Helm Deployment is a single step. A dimension can be added to the order when the concept exists.

**Ties are refused.** Two values of one variable with the same dimensions in scope, and overlapping names in each, would tie for some Deployment: for example, one scoped to `staging, production` and another to `production`. Saving the second is refused, naming the overlap. Octopus picks among those non-deterministically; deploying an unpredictable value is worse than being told when saving.

### Variable Sets

A **Variable Set** belongs to a Workspace and holds variables like an Application does. An Application includes any number of the Workspace's Variable Sets.

- A Variable Set's values can be scoped to Environments and Targets but not Channels, because Channels belong to one Application. Octopus has the same restriction.
- A Variable Set and an Application, or two Variable Sets, may define the same name. The most specific value wins across all of them. When the Application's value and a Variable Set's value are equally specific, the Application's wins, as in Octopus. When two included Variable Sets would tie with each other, including the second one is refused, naming the variable.

### Bindings and system variables

A `text` value or a secret's `path` can refer to other variables with `#{Name}`, as in Octopus: `postgres://#{Database.Host}:5432/#{Database.Name}`. `##{` writes a literal `#{`. Unlike Octopus's Octostache, there are no conditionals, loops, or filters: logic belongs in the chart's templates, which Helm already has.

- A binding to an undefined name fails the Deployment, naming it. Octopus substitutes an empty string, which turns a typo into a misconfigured production service.
- A cycle fails the Deployment.
- **A value that binds a secret is a secret.** Octopus says sensitivity is "not transitive"; in Ivaldi it is, so a connection string that binds a password is delivered and masked as a secret, because it contains one.

System variables are always defined, and can be bound but not overridden:

| Name | Value |
|---|---|
| `Ivaldi.Organization.Slug`, `Ivaldi.Workspace.Slug`, `Ivaldi.Project.Slug` | The Application's |
| `Ivaldi.Application.Slug`, `Ivaldi.Application.Id` | |
| `Ivaldi.Environment.Slug`, `Ivaldi.Environment.Tier` | The Target's Environment |
| `Ivaldi.Target.Slug`, `Ivaldi.Target.Namespace`, `Ivaldi.Cluster.Slug` | |
| `Ivaldi.Channel.Slug`, `Ivaldi.Release.Version`, `Ivaldi.Release.Id` | |
| `Ivaldi.Deployment.Id` | |

## Snapshots

**Creating a Release takes a snapshot** of every variable it will deploy with: the Application's variables and those of the Variable Sets it includes, with all their scoped values and each value's source. Secret values are snapshotted as references; the secret itself is read when the Release is deployed. A snapshot is revision 1 of the Release's variables.

**Refreshing creates a new revision.** "Update variables" on a Release takes a fresh snapshot from the Application's current variables and makes it the Release's current revision. Earlier revisions are kept. Octopus can also update chosen variables only; Ivaldi refreshes the whole snapshot, and the API's preview shows what would change before it does. Refreshing needs `release.manage`, and a human's approval when it changes what production would get ([below](#refreshes-that-change-production-need-approval)). It is allowed on a draft and on a published Release, because a snapshot is configuration and not one of the Release's Builds. It is not allowed on a withdrawn Release.

**Deployments use the current revision** of their Release's snapshot when they are requested, except rollbacks (below), and record which revision that was. Redeploying a Release uses its current revision, so a redeploy after a variable change needs a refresh first, as in Octopus. A Deployment's record keeps the revision it used even if the Release is refreshed later.

**A snapshot names secrets; a Deployment pins their versions.** A snapshot freezes which secret is used, not its value. A rollout or a redeploy resolves each secret to the version its value pins, or to the latest when it pins none, which is how a redeploy picks up a rotated secret. The agent reports the version it resolved for every secret before it applies anything, and the control plane stores them on the Deployment ([Kubernetes agent](k8s-agent.md#variables-and-secrets)).

### Rollbacks restore the configuration they replace

The control plane decides a requested Deployment's `reason` (today every executed Deployment is a `rollout`):

- `redeploy` when the Release is the placement's current Release;
- `rollback` when the Release succeeded on this Target before (one of its Deployments there reached `healthy`) and is not current;
- `rollout` otherwise.

A **rollback replays the last Deployment of that Release on that Target that reached `healthy`**: it uses that Deployment's snapshot revision, not the Release's current one, and pins every secret to the version that Deployment resolved. The assignment carries the pins, and the agent fetches exactly those versions. Restoring a Release therefore restores the code, the configuration, and the secret values it last ran with there.

- **A pinned version may be gone.** Secret managers keep a limited history: Vault's KV engine keeps ten versions by default, and AWS Secrets Manager eventually removes versions that have no staging label. A rollback whose pinned version no longer exists fails before anything is applied, naming the secret. The self-hosting docs will say how many versions to keep.
- **Rolling back with current secrets is a deliberate choice.** The person can then request the rollback again with `secrets: latest`, which keeps the restored snapshot revision but resolves secrets to their latest versions. It is recorded on the Deployment and audited, and policy sees it like any other Deployment request.
- **A Release last seen through observation has nothing to replay.** If the Release's last healthy Deployment on the Target was observed rather than executed, there are no recorded versions. The rollback uses the Release's current revision and latest secrets, and the deploy sheet says so before it is requested.

### Refreshes that change production need approval

When someone refreshes a Release's snapshot, the control plane resolves the old and the new revision for every active Target whose Environment has the `production` tier and is in the Release's Channel's Lifecycle. If any variable's winning value differs for any of them (a text value, a secret reference, or a pinned version), the refresh changes production.

- **Policy decides, delivery reports.** Delivery asks the single authorizer for `release.refresh_variables`, naming the production Environments the change touches ([ADR 0003](../adr/0003-identity-access-and-policy.md)). Requiring a human's approval for that is a built-in policy rule, like ADR 0007's rule for autonomous agents, not code in delivery. A refresh that changes no production value needs no approval.
- **The new revision waits.** It is stored as `pending_approval` and does not become current until it is approved, so Deployments meanwhile keep using the current revision. A refused approval marks it `rejected`. A Release has at most one pending revision; refreshing again replaces it, marking the earlier one `superseded`.
- **Approvers see the change.** The approval request shows which variables change, for which production Targets, from what to what, with secrets shown as references. It goes to the Application's owning Team by default.
- **Who can approve.** A human user, never an agent Session, who holds `variable.manage` on every production Environment the change touches.
- **Why only refreshes.** A new Release's snapshot needs no approval of its own: its first Deployment to production is already the gate, and the deploy sheet shows its variables. A refresh needs one because it changes what an already-approved Release would deploy, including on its next rollback.

## Resolving a Deployment's variables

When the control plane builds an assignment, it takes the Deployment's snapshot revision and, for each variable, picks the winning value for the Deployment's Target, Environment, and Channel. The assignment carries, for each variable with a winning value:

- `name`, `kind`, `values_path`, `deliver_as`, and its source (Application or Variable Set);
- the `text` value, or the secret reference with the version to fetch: the one the value pins, the one a rollback replays, or none for the latest;

plus every system variable.

The agent then fills in bindings, fetches secrets, and builds the chart's values ([Kubernetes agent](k8s-agent.md#applying-with-helm)).

**A preview shows the result before deploying.** `GET /v1/releases/{release_id}/variables?target_id=…` returns, for each variable, the winning value and the values it beat, with secrets shown as references. The deploy sheet shows it, and it answers "why did production get this value" without reading the precedence rules.

**Checks before a Deployment is accepted.** A Deployment request is refused when a winning `secret` value's Secret Store is not allowed for the Target's Environment, or a binding names a variable that is not defined, in the snapshot revision it would use. Both can be known before the agent runs, so the person deploying hears it straight away.

## Permissions and audit

- **Reading.** `variable.read` on the Project reads an Application's variables and its Releases' snapshots; on the Workspace it reads Variable Sets. Secret values are references, so reading them reveals where a secret is, not what it is.
- **Changing.** `variable.manage` on the Application's Project changes its unscoped values. A value scoped to Environments also needs `variable.manage` on each of those Environments, so a Project's members cannot change production configuration without being allowed to on production. Octopus checks Environment and Target scopes against permissions the same way. Variable Sets need `variable.manage` on the Workspace, and the same per-Environment check.
- **Including** a Variable Set in an Application needs `variable.manage` on the Project and `variable.read` on the Workspace.
- **Every change is audited** through `internal/change`, with the variable's snapshot before and after. A secret value's audit record holds its reference, which is not secret.

The built-in roles pick up `variable.read` and `variable.manage` by rule, so IAM does not change.

## Entities

All in the `delivery` schema, following its conventions. Secret Stores belong to the credentials capability ([Kubernetes agent](k8s-agent.md#secret-stores)) and are referred to by plain ID.

| Table | Prefix | Holds |
|---|---|---|
| `variable_sets` | `vst` | Workspace, slug, display name, description, archived time |
| `application_variable_sets` | | Which Variable Sets an Application includes |
| `variables` | `var` | Exactly one of `application_id` or `variable_set_id`; `name` (unique case-insensitively within its owner), `description`, `kind`, `values_path`, `deliver_as` |
| `variable_values` | `vvl` | Child of a variable: `text_value`, or `store_id`, `path`, `field`, `version`; `environment_ids`, `target_ids`, `channel_ids` |
| `variable_snapshots` | `vsn` | One per Release revision: `release_id`, `revision`, the snapshot as JSON, `status` (`current`, `pending_approval`, `rejected`, `superseded`), `approval_decision_id`, who took it and when |

`releases` gains `variable_snapshot_id` (the current revision). `deployments` gains `variable_snapshot_id` (the revision it used), `secret_versions` (for each secret variable: its store, path, field, and the version ID the agent resolved), `secrets_latest` (a rollback that asked for current secrets), and `restores_deployment_id` (for a rollback, the Deployment it replays). A variable and its values form one aggregate, changed through the variable's methods. A Variable Set cannot be archived while an active Application includes it. Environments, Targets, and Channels named in a scope cannot be archived while an active value names them, matching delivery's rule that configuration is not archived out from under what uses it.

`apply_config.values_refs` stays for whole values documents, such as a values file a platform team keeps in a ConfigMap. Variables are for individual values.

## Invariants

- **No secret value is stored by Ivaldi**, in variables, snapshots, audit records, events, or Deployment history. Only references and resolved version IDs are.
- **Every executed Deployment records its snapshot revision and the secret versions it resolved**, and a snapshot revision never changes once taken.
- **A rollback deploys the snapshot revision and secret versions of the Deployment it restores**, or fails, unless the person asked for current secrets.
- **A revision that changes production values becomes current only with a human's approval.**
- **No two values of a variable can tie** for any Deployment, within one owner or across an Application's included Variable Sets.
- **A Deployment uses only Secret Stores allowed for its Environment.**
- **Changing a value scoped to an Environment needs permission on that Environment.**

## Open questions

- **Prompted variables.** Octopus can ask for a value when a Deployment is requested. Useful for one-off operations, but it puts unreviewed input into a Deployment; it waits for a need.
- **Partial refresh.** Whether "Update variables" should refresh chosen variables only, as Octopus allows, or the whole snapshot as proposed.
- **Four eyes.** Whether the person who refreshed a snapshot may approve it themselves when they hold the permission. Octopus has no such rule; policy could add one for production.
- **Approvals for other tiers.** Only `production` needs approval for now. Whether `staging`, or Environments named by policy, should too.
- **Secret rotation without a Release.** A rotated secret reaches workloads at the next Deployment. Redeploying automatically when an unpinned secret's version changes would be a new Deployment `reason` and needs its own design.
- **Variables as code.** Octopus can keep variables in Git with the deployment process. Ivaldi could read them from the Source a Release was built from, which would version them with the code; sensitive values would stay references either way.
