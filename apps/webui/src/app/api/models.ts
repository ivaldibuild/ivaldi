/**
 * The control plane's types under the names the web app uses. They come from
 * `schema.gen.ts`, which is generated from the API's description
 * (`api/openapi.json`), so they cannot drift from what the control plane sends.
 * Regenerate both with `make openapi` from the repository root.
 */
import type { components } from './schema.gen';

type Schemas = components['schemas'];

export type Workspace = Schemas['WorkspaceSnapshot'];
export type Project = Schemas['ProjectSnapshot'];

/** A setting's value as stored at one level. */
export type Setting = Schemas['SettingSnapshot'];
/** A setting's effective value at a level, and where it comes from. */
export type ResolvedSetting = Schemas['Resolved'];
/** Where a setting's value is stored. */
export type SettingLevel = NonNullable<ResolvedSetting['level']>;

export type TeamMember = Schemas['TeamMemberSnapshot'];
export type TeamRole = TeamMember['role'];
export type Team = Schemas['TeamSnapshot'];
export type Agent = Schemas['AgentSnapshot'];
/** An Agent as a list shows it, with its Teams but not the full prompt. */
export type AgentSummary = Schemas['AgentSummary'];
/** One Agent with its system prompt and Teams. */
export type AgentDetail = Schemas['AgentDetail'];

export type Cluster = Schemas['ClusterSnapshot'];
/** A Cluster's enrolled agent and credential state, from `GET /v1/clusters/{id}/agent`. */
export type AgentStatus = Schemas['AgentStatus'];
/** A new enrolment token and how to install the agent that redeems it. */
export type IssuedEnrolment = Schemas['IssuedEnrolment'];
export type InstallDetails = Schemas['InstallDetails'];

export type Environment = Schemas['EnvironmentSnapshot'];
export type Tier = Environment['tier'];
export type Target = Schemas['TargetSnapshot'];
export type LifecyclePhase = Schemas['LifecyclePhaseSnapshot'];
export type Lifecycle = Schemas['LifecycleSnapshot'];

export type Application = Schemas['ApplicationSnapshot'];
/** Where a Workspace's Applications run, from `GET /v1/workspaces/{id}/runs-on`. */
export type WorkspaceRunsOn = Schemas['WorkspaceRunsOn'];
export type RunsOnApplication = Schemas['RunsOnApplication'];
export type RunsOnProject = Schemas['RunsOnProject'];
export type RunsOnTeam = Schemas['RunsOnTeam'];
export type RunsOnTarget = Schemas['RunsOnTarget'];
export type Artifact = Schemas['ArtifactSnapshot'];
export type ArtifactKind = Artifact['artifact_kind'];
export type ChannelRule = Schemas['ChannelRuleSnapshot'];
export type Channel = Schemas['ChannelSnapshot'];
export type Placement = Schemas['PlacementSnapshot'];

/** A secret manager a runtime agent reads secret variables from; it holds no credential. */
export type SecretStore = Schemas['SecretStoreSnapshot'];
export type SecretStoreKind = SecretStore['kind'];
export type SecretStoreMode = SecretStore['access']['mode'];
export type SecretStoreMethod = NonNullable<SecretStore['access']['method']>;
export type MintingCredential = Schemas['MintingCredentialBody'];
/** Where a secret variable's value is kept: never the secret itself. */
export type SecretReference = Schemas['SecretReference'];

/** A named piece of configuration a Project's Applications, or one Application, deploy with. */
/**
 * The generated types can't say an object is null, so these say it: a text variable's value has
 * no `secret`.
 */
export type VariableValue = Omit<Schemas['VariableValueSnapshot'], 'secret'> & {
  secret: SecretReference | null;
};
export type Variable = Omit<Schemas['VariableSnapshot'], 'values'> & { values: VariableValue[] };
/** The Deployments a value applies to; an empty list matches any. */
export type VariableScope = Schemas['VariableScope'];

/** A value that matches a Deployment: a text variable's text, or where a secret is kept. */
export type ResolvedValue = Omit<Schemas['ResolvedValue'], 'secret'> & {
  secret: SecretReference | null;
};
/**
 * A variable as one Deployment gets it: the value that wins and the ones it beat. `winner` is null
 * when no value applies.
 */
export type ResolvedVariable = Omit<Schemas['ResolvedVariable'], 'winner' | 'beaten'> & {
  winner: ResolvedValue | null;
  beaten: ResolvedValue[];
};
/**
 * A Release's current revision of variables, and what a Target would get of them. `revision` is
 * null for a Release created before variables were snapshotted.
 */
export interface ReleaseVariables {
  revision: Schemas['ReleaseVariablesSnapshot'] | null;
  resolved: ResolvedVariable[];
}

/**
 * What refreshing a Release's variables changes, or would change. `revision` is the Release's
 * current revision after the call, null when it has none.
 */
export type ReleaseVariablesRefresh = Omit<Schemas['ReleaseVariablesRefresh'], 'revision'> & {
  revision: Schemas['ReleaseVariablesSnapshot'] | null;
};

/** A Release with each of its Builds in full, from `GET /v1/releases/{id}`. */
export type Release = Schemas['Release'];
export type ReleaseStatus = Release['status'];
export type ReleaseBuild = Schemas['ReleaseBuild'];
export type Build = Schemas['BuildSnapshot'];

/** How Releases are applied on a Target: the artifact holding the definition, and values. */
export type ApplyConfig = Schemas['ApplyConfig'];
export type Deployment = Schemas['DeploymentSnapshot'];
export type DeploymentStatus = Deployment['status'];
/** A rule an observed Deployment broke: another tool deployed what Ivaldi would have refused. */
export type DeploymentFinding = Deployment['findings'][number];
export type DeploymentEvent = Schemas['DeploymentEventSnapshot'];
/** A Deployment with its timeline, from `GET /v1/deployments/{id}`. */
export type DeploymentDetail = Schemas['DeploymentDetail'];
/** What a Release would change on a Target, and the checks before it can deploy there. */
export type ReleaseChanges = Schemas['ReleaseChanges'];
export type DeploymentCheck = Schemas['DeploymentCheck'];

/** The caller's Organisation as `GET /v1/organization` returns it. */
export type Organization = Schemas['Organization'];

export type MeRole = Schemas['MeRole'];
/** The signed-in caller, from `GET /v1/me`. */
export type Me = Schemas['Me'];

/** One of the Organisation's users, from `GET /v1/people`. */
export type Person = Schemas['Person'];
/** A role granted on a resource, from `GET /v1/grants`. */
export type Grant = Schemas['Grant'];
export type BuiltInRole = Schemas['CreateGrantRequestBody']['role'];

/** A non-person principal, such as CI, from `GET /v1/service-accounts`. */
export type ServiceAccount = Schemas['ServiceAccount'];
/** A service account's API token, without its secret. */
export type ApiToken = Schemas['APITokenSnapshot'];
/** A token as it is issued: the only time its secret is shown. */
export type IssuedApiToken = Schemas['IssuedToken'];

/** How the Organisation reaches one version-control account, such as a GitHub App installation. */
export type Connection = Schemas['ConnectionSnapshot'];
export type ConnectionProvider = Connection['provider'];
/** Where to send someone to install a provider's app, from `POST /v1/connections/installations`. */
export type InstallationStart = Schemas['InstallationStartSnapshot'];
/** What the provider sent back after an install, for `POST /v1/connections/installations/complete`. */
export type CompleteInstallation = Omit<Schemas['CompleteInstallationRequestBody'], '$schema'>;
/** A repository a Connection's account lets Ivaldi read, which can become a Source. */
export type Repository = Schemas['RepositorySnapshot'];
/** A repository connected to a Project through a Connection. */
export type Source = Schemas['SourceSnapshot'];
/** A provider whose accounts are connected by installing its app. */
export type InstallableProvider = CompleteInstallation['provider'];

/** Which of the asked permissions the signed-in person holds on a resource. */
export type MyPermissions = Schemas['MyPermissions'];

/** A cursor-paged list's page, with its cursors. */
export interface CursorPage<T> {
  items: T[];
  total_count: number;
  has_next: boolean;
  next_cursor: string | null;
}

/** One message to the signed-in person, from `GET /v1/notifications`. */
export type Notification = Schemas['NotificationSnapshot'];

/** A request for a person's approval, with whether the caller could decide it. */
export type ApprovalRequest = Schemas['ApprovalRequestView'];
export type ApprovalState = ApprovalRequest['state'];
/** What a Deployment's approval request shows its approvers. */
export type DeploymentApprovalSummary = Schemas['DeploymentApprovalSummary'];

/** Any of the Organisation's principals by name, from `GET /v1/principals`. */
export type NamedPrincipal = Schemas['NamedPrincipal'];
