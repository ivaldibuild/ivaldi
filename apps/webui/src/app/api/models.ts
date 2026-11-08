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

export type Environment = Schemas['EnvironmentSnapshot'];
export type Tier = Environment['tier'];
export type Target = Schemas['TargetSnapshot'];
export type LifecyclePhase = Schemas['LifecyclePhaseSnapshot'];
export type Lifecycle = Schemas['LifecycleSnapshot'];

export type Application = Schemas['ApplicationSnapshot'];
export type Artifact = Schemas['ArtifactSnapshot'];
export type ArtifactKind = Artifact['artifact_kind'];
export type ChannelRule = Schemas['ChannelRuleSnapshot'];
export type Channel = Schemas['ChannelSnapshot'];
export type Placement = Schemas['PlacementSnapshot'];

/** A Release with each of its Builds in full, from `GET /v1/releases/{id}`. */
export type Release = Schemas['Release'];
export type ReleaseStatus = Release['status'];
export type ReleaseBuild = Schemas['ReleaseBuild'];
export type Build = Schemas['BuildSnapshot'];

/** How Releases are applied on a Target: the artifact holding the definition, and values. */
export type ApplyConfig = Schemas['ApplyConfig'];
export type Deployment = Schemas['DeploymentSnapshot'];
export type DeploymentStatus = Deployment['status'];
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
/** A provider whose accounts are connected by installing its app. */
export type InstallableProvider = CompleteInstallation['provider'];

/** A cursor-paged list's page, with its cursors. */
export interface CursorPage<T> {
  items: T[];
  total_count: number;
  has_next: boolean;
  next_cursor: string | null;
}
