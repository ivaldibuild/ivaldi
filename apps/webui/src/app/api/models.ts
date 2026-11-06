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
