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

/** A cursor-paged list's page, with its cursors. */
export interface CursorPage<T> {
  items: T[];
  total_count: number;
  has_next: boolean;
  next_cursor: string | null;
}
