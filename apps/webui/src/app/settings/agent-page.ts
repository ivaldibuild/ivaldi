import { Component, computed, effect, inject, input, signal, untracked } from '@angular/core';
import { form, FormField, required, submit } from '@angular/forms/signals';
import { RouterLink } from '@angular/router';
import { HlmAlertImports } from '@spartan-ng/helm/alert';
import { HlmBadgeImports } from '@spartan-ng/helm/badge';
import { HlmButtonImports } from '@spartan-ng/helm/button';
import { HlmFieldImports } from '@spartan-ng/helm/field';
import { HlmInputImports } from '@spartan-ng/helm/input';
import { HlmSpinnerImports } from '@spartan-ng/helm/spinner';
import { AgentsApi } from '../api/agents-api';
import { CurrentUser } from '../api/current-user';
import { AgentDetail, Team, TeamRole } from '../api/models';
import { teamRoleLabels, TeamsApi } from '../api/teams-api';
import { PageHeader } from '../delivery/page-header';
import { RequestStatus } from '../shared/request-status';

interface TeamRow {
  team_id: string;
  display_name: string;
  slug: string;
  role: TeamRole;
  archived_at: string | null;
}

const teamRoles: TeamRole[] = ['lead', 'member'];

/**
 * One Agent: its name, its system prompt, suspending and reactivating it, and the Teams it
 * belongs to. Owners and admins change it; everyone else reads it.
 */
@Component({
  selector: 'app-agent-page',
  imports: [
    FormField,
    PageHeader,
    RouterLink,
    HlmAlertImports,
    HlmBadgeImports,
    HlmButtonImports,
    HlmFieldImports,
    HlmInputImports,
    HlmSpinnerImports,
  ],
  templateUrl: './agent-page.html',
})
export class AgentPage {
  readonly id = input.required<string>();

  private readonly api = inject(AgentsApi);
  private readonly teamsApi = inject(TeamsApi);
  protected readonly user = inject(CurrentUser);

  protected readonly status = new RequestStatus();
  protected readonly agent = signal<AgentDetail | null>(null);
  private readonly teams = signal<Team[]>([]);

  protected readonly canManage = this.user.canManageOrganization;
  protected readonly teamRoles = teamRoles;
  protected readonly roleLabels = teamRoleLabels;

  protected readonly teamRows = computed<TeamRow[]>(() => this.agent()?.teams ?? []);

  /** Active Teams the Agent does not belong to yet. */
  protected readonly candidates = computed(() => {
    const memberOf = new Set((this.agent()?.teams ?? []).map((team) => team.team_id));
    return this.teams().filter((team) => !team.archived_at && !memberOf.has(team.id));
  });

  // The Agent's name and prompt.
  protected readonly detailsStatus = new RequestStatus();
  private readonly detailsModel = signal({ display_name: '', system_prompt: '' });
  protected readonly detailsForm = form(this.detailsModel, (path) => {
    required(path.display_name, { message: 'Name the Agent.' });
  });
  protected readonly detailsChanged = computed(() => {
    const agent = this.agent();
    const { display_name, system_prompt } = this.detailsModel();
    return (
      !!agent &&
      (display_name.trim() !== agent.display_name || system_prompt !== agent.system_prompt)
    );
  });

  // Suspending and reactivating.
  protected readonly statusChange = new RequestStatus();

  // Team memberships.
  protected readonly membershipStatus = new RequestStatus();
  protected readonly changing = signal<string | null>(null);
  protected readonly teamToAdd = signal('');
  protected readonly roleToAdd = signal<TeamRole>('member');

  constructor() {
    effect(() => {
      const id = this.id();
      if (this.user.organization()?.id) {
        untracked(() => void this.load(id));
      }
    });
  }

  protected async saveDetails(event: Event): Promise<void> {
    event.preventDefault();
    await submit(this.detailsForm, async () => {
      const agent = this.agent();
      if (!agent) {
        return undefined;
      }
      const { display_name, system_prompt } = this.detailsModel();
      const body: { display_name?: string; system_prompt?: string } = {};
      if (display_name.trim() !== agent.display_name) {
        body.display_name = display_name.trim();
      }
      if (system_prompt !== agent.system_prompt) {
        body.system_prompt = system_prompt;
      }
      if (Object.keys(body).length === 0) {
        return undefined;
      }
      await this.detailsStatus.run(async () => this.show(await this.api.update(this.id(), body)));
      return undefined;
    });
  }

  protected async suspend(): Promise<void> {
    await this.statusChange.run(async () => this.show(await this.api.suspend(this.id())));
  }

  protected async reactivate(): Promise<void> {
    await this.statusChange.run(async () => this.show(await this.api.reactivate(this.id())));
  }

  protected async changeRole(row: TeamRow, role: TeamRole): Promise<void> {
    if (role === row.role) {
      return;
    }
    this.changing.set(row.team_id);
    await this.membershipStatus.run(async () => {
      await this.teamsApi.setMember(row.team_id, this.id(), role);
      await this.reloadAgent();
    });
    this.changing.set(null);
  }

  protected async remove(row: TeamRow): Promise<void> {
    this.changing.set(row.team_id);
    await this.membershipStatus.run(async () => {
      await this.teamsApi.removeMember(row.team_id, this.id());
      await this.reloadAgent();
    });
    this.changing.set(null);
  }

  protected async addTeam(event: Event): Promise<void> {
    event.preventDefault();
    const teamId = this.teamToAdd();
    if (!teamId) {
      this.membershipStatus.error.set('Choose a Team to add the Agent to.');
      return;
    }
    const added = await this.membershipStatus.run(async () => {
      await this.teamsApi.setMember(teamId, this.id(), this.roleToAdd());
      await this.reloadAgent();
    });
    if (added) {
      this.teamToAdd.set('');
      this.roleToAdd.set('member');
    }
  }

  private show(agent: AgentDetail): void {
    this.agent.set(agent);
    this.detailsForm().reset({
      display_name: agent.display_name,
      system_prompt: agent.system_prompt,
    });
  }

  private async reloadAgent(): Promise<void> {
    this.show(await this.api.get(this.id()));
  }

  private async load(id: string): Promise<void> {
    await this.status.run(async () => {
      const [agent, teams] = await Promise.all([this.api.get(id), this.teamsApi.list()]);
      this.teams.set(teams);
      this.show(agent);
    }, 'loading');
  }
}
