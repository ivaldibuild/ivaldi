/** The control plane's snapshots, as the web app reads them. Fields it doesn't use are left out. */

export interface Workspace {
  id: string;
  slug: string;
  display_name: string;
  is_default: boolean;
}

export interface Project {
  id: string;
  workspace_id: string;
  owner_team_id: string;
  slug: string;
  display_name: string;
  description: string;
  created_at: string;
  /** When it was archived, or null while it's active. */
  archived_at: string | null;
}

/** Where a setting's value is stored. */
export type SettingLevel = 'organization' | 'workspace' | 'project';

/** A setting's value as stored at one level. */
export interface Setting {
  key: string;
  value: unknown;
  /** Increases with every change; a change or removal sends the version it read. */
  version: number;
}

/** A setting's effective value at a level, and where it comes from. */
export interface ResolvedSetting {
  key: string;
  value: unknown;
  /** The level the value is stored at; absent for the default or no value. */
  level?: SettingLevel;
  from_default: boolean;
  /** Whether a lock at a higher level fixed the value. */
  locked: boolean;
}

export type TeamRole = 'lead' | 'member';

export interface TeamMember {
  principal_id: string;
  principal_kind: string;
  role: TeamRole;
}

export interface Team {
  id: string;
  slug: string;
  display_name: string;
  members: TeamMember[];
}

export interface Agent {
  id: string;
  display_name: string;
}

export type Tier = 'development' | 'staging' | 'production' | 'other';

export interface Environment {
  id: string;
  slug: string;
  display_name: string;
  tier: Tier;
  execution_enabled: boolean;
}

export interface Target {
  id: string;
  environment_id: string;
  slug: string;
  display_name: string;
  locator: Record<string, string>;
  agent_principal_id?: string | null;
  last_reported_at?: string | null;
}

export interface LifecyclePhase {
  id: string;
  name: string;
  position: number;
  optional?: boolean;
  environment_ids: string[];
}

export interface Lifecycle {
  id: string;
  slug: string;
  display_name: string;
  description?: string | null;
  phases: LifecyclePhase[];
}

export interface Application {
  id: string;
  workspace_id: string;
  project_id: string;
  owner_team_id: string;
  slug: string;
  display_name: string;
  source_id: string | null;
}

export type ArtifactKind = 'container_image' | 'helm_chart' | 'archive';

export interface Artifact {
  id: string;
  name: string;
  artifact_kind: ArtifactKind;
  artifact_name: string;
  registry_id: string | null;
  required: boolean;
  source: 'declared' | 'observed';
}

export interface ChannelRule {
  id: string;
  kind: 'version' | 'git_ref';
}

export interface Channel {
  id: string;
  lifecycle_id: string;
  slug: string;
  display_name: string;
  description: string | null;
  is_default: boolean;
  rules: ChannelRule[];
}

export interface Placement {
  application_id: string;
  target_id: string;
  source: 'configured' | 'observed';
  current_release_id: string | null;
}

/** The caller's Organisation as `GET /v1/organization` returns it. */
export interface Organization {
  id: string;
  slug: string;
  name: string;
  display_name: string;
  logo_url: string | null;
}

export interface MeRole {
  /** The built-in role's key, such as `owner`, or empty for a custom role. */
  role: string;
  role_id: string;
  resource_id: string;
}

/** The signed-in caller, from `GET /v1/me`. */
export interface Me {
  principal: { id: string; kind: string; display_name: string; email: string | null };
  organization: { id: string; slug: string; display_name: string; logo_url: string | null };
  roles: MeRole[];
}
